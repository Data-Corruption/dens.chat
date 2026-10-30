package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/release"

	"golang.org/x/mod/semver"
)

// The Android app's updater (M12). The update check finds a newer release
// as an installed instance's does. The service then downloads the
// release's checksums and their cosign bundle, checks the bundle as cosign
// would, against the release identity this build carries, then this
// phone's APK against the checksums, and keeps them in the layout's Update
// directory. The shell posts that the update is ready, and its tap, or the
// page's Install, hands the APK to Android's installer, which asks the
// member to confirm, and takes it only signed with the key the app was
// installed with, at a higher version code. Each start checks what's kept
// again, and removes what's no newer than itself.

// The most the updater downloads of each file of a release.
const (
	maxChecksumsBytes = 64 << 10
	maxBundleBytes    = 1 << 20
	maxVersionBytes   = 64
	maxAPKBytes       = 512 << 20
)

// apkDownloadTimeout bounds one download of a release's files, an APK of
// tens of megabytes on a slow phone network among them.
const apkDownloadTimeout = 15 * time.Minute

// apkName is the release's APK for the phone this runs on.
func apkName() string { return "android-" + runtime.GOARCH + ".apk" }

// UpdateReady is a newer release's APK, downloaded and checked, which the
// shell hands to Android's installer.
type UpdateReady struct {
	Version string `json:"version"`
	APK     string `json:"apk"`
}

// UpdateShell is the Android app's shell, as the updater tells it of an
// update: that one is ready, which it posts, and that the member asked the
// page to install it.
type UpdateShell interface {
	UpdateReady(u UpdateReady)
	InstallUpdate(u UpdateReady)
}

// apkUpdate is what the updater has ready, and the shell it tells; and
// the version it last refused, and when, which it tries again only after
// a day rather than download its APK each hour.
type apkUpdate struct {
	mu        sync.Mutex
	ready     *UpdateReady
	shell     UpdateShell
	refused   string
	refusedAt time.Time
}

// errRefused is a release that doesn't check out, as against one the
// updater couldn't download.
var errRefused = errors.New("the release doesn't check out")

// ReadyUpdate is the update the Android app's updater has ready, if any.
func (a *App) ReadyUpdate() (UpdateReady, bool) {
	a.apk.mu.Lock()
	defer a.apk.mu.Unlock()
	if a.apk.ready == nil {
		return UpdateReady{}, false
	}
	return *a.apk.ready, true
}

// SetUpdateShell is the shell connecting, which hears of a ready update
// at once, where the member wants notices of them.
func (a *App) SetUpdateShell(s UpdateShell) {
	a.apk.mu.Lock()
	a.apk.shell = s
	ready := a.apk.ready
	a.apk.mu.Unlock()
	if ready != nil && a.updateNotices() {
		s.UpdateReady(*ready)
	}
}

// DropUpdateShell is the shell s disconnecting.
func (a *App) DropUpdateShell(s UpdateShell) {
	a.apk.mu.Lock()
	defer a.apk.mu.Unlock()
	if a.apk.shell == s {
		a.apk.shell = nil
	}
}

// ErrNoUpdate is an install asked for with no update ready, or no shell to
// hand it to.
var ErrNoUpdate = errors.New("no update is ready to install")

// InstallUpdate asks the shell to hand the ready update to Android's
// installer, as the member asked from the page.
func (a *App) InstallUpdate() error {
	a.apk.mu.Lock()
	ready, shell := a.apk.ready, a.apk.shell
	a.apk.mu.Unlock()
	if ready == nil || shell == nil {
		return ErrNoUpdate
	}
	a.Log.Infof("The member asked to install %s", ready.Version)
	shell.InstallUpdate(*ready)
	return nil
}

func (a *App) updateNotices() bool {
	cfg, err := config.View(a.DB)
	return err == nil && cfg.UpdateNotifications
}

// releaseCheck is what a release's signature is checked against: the
// trusted root, Sigstore's, or a test release's own, and the identity
// this build was signed as. A build without one can't check a release,
// so it updates from none.
func (a *App) releaseCheck() (*release.TrustedRoot, release.Signer, error) {
	signer := release.Signer{Identity: a.buildInfo.CertIdentity, Issuer: a.buildInfo.OidcIssuer}
	if signer.Identity == "" || signer.Issuer == "" {
		return nil, signer, errors.New("this build carries no release identity to check an update against")
	}
	if a.buildInfo.SigstoreRoot == "" {
		root, err := release.PublicRoot()
		return root, signer, err
	}
	data, err := base64.StdEncoding.DecodeString(a.buildInfo.SigstoreRoot)
	if err != nil {
		return nil, signer, fmt.Errorf("this build's trusted root: %w", err)
	}
	root, err := release.ParseTrustedRoot(data)
	return root, signer, err
}

// checkStagedUpdate checks what an earlier start kept as a start begins:
// a newer release, still checking out, is ready again, and anything else
// goes, as the update just installed does, and what a download cut off
// left.
func (a *App) checkStagedUpdate() {
	dir := a.Layout.Update
	leftovers, _ := filepath.Glob(dir + "-*")
	for _, d := range leftovers {
		_ = os.RemoveAll(d)
	}
	if _, err := os.Stat(filepath.Join(dir, "version")); errors.Is(err, os.ErrNotExist) {
		return
	}
	version, err := a.checkRelease(dir)
	switch {
	case err != nil:
		a.Log.Warnf("The update kept from before doesn't check out, so it goes: %v", err)
	case semver.Compare(version, a.buildInfo.Version) <= 0:
		a.Log.Infof("Removing the update to %s, which this %s replaces", version, a.buildInfo.Version)
	default:
		a.setReady(UpdateReady{Version: version, APK: filepath.Join(dir, apkName())})
		return
	}
	a.removeStaged()
}

// removeStaged empties the Update directory, which the layout keeps.
func (a *App) removeStaged() {
	if err := emptyDir(a.Layout.Update); err != nil && !errors.Is(err, os.ErrNotExist) {
		a.Log.Warnf("Couldn't remove the kept update: %v", err)
	}
}

func (a *App) setReady(u UpdateReady) {
	a.apk.mu.Lock()
	a.apk.ready = &u
	shell := a.apk.shell
	a.apk.mu.Unlock()
	if shell != nil && a.updateNotices() {
		shell.UpdateReady(u)
	}
}

// readyAndroidUpdate downloads and checks the newer release the last check
// found, unless it's ready already.
func (a *App) readyAndroidUpdate(ctx context.Context) {
	cfg, err := config.View(a.DB)
	if err != nil {
		return
	}
	latest, ok := a.LatestUpdate(cfg)
	if !ok {
		return
	}
	if ready, ok := a.ReadyUpdate(); ok && semver.Compare(ready.Version, latest) >= 0 {
		return
	}
	a.apk.mu.Lock()
	wait := a.apk.refused == latest && time.Since(a.apk.refusedAt) < UpdateCheckInterval
	a.apk.mu.Unlock()
	if wait {
		return
	}
	if err := a.stageUpdate(ctx, latest); err != nil {
		if errors.Is(err, errRefused) {
			a.apk.mu.Lock()
			a.apk.refused, a.apk.refusedAt = latest, time.Now()
			a.apk.mu.Unlock()
		}
		if ctx.Err() == nil {
			a.Log.Warnf("Couldn't ready the update to %s: %v", latest, quietNetError(err))
		}
		return
	}
	a.Log.Infof("Downloaded %s's APK, which checks out against the release's signed checksums", latest)
}

// quietNetError is err without the address Go's network errors name,
// which logs don't keep (M8).
func quietNetError(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return errors.New("the release host can't be reached")
	}
	return err
}

// stageUpdate downloads what the updater needs of version's release to a
// directory of its own, checks it, and only then puts it in the Update
// directory's place, so what's there has always checked out.
func (a *App) stageUpdate(ctx context.Context, version string) error {
	root, signer, err := a.releaseCheck()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, apkDownloadTimeout)
	defer cancel()
	temp, err := os.MkdirTemp(filepath.Dir(a.Layout.Update), filepath.Base(a.Layout.Update)+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	base := a.Instance.ReleaseURL + "releases/" + version + "/"
	fetch := func(name string, limit int64) error {
		return a.fetchReleaseFile(ctx, base+name, filepath.Join(temp, name), limit)
	}
	// The checksums first, and their signature, so an APK the release
	// doesn't vouch for is never downloaded.
	if err := fetch("checksums.txt", maxChecksumsBytes); err != nil {
		return err
	}
	if err := fetch("checksums.txt.cosign.bundle", maxBundleBytes); err != nil {
		return err
	}
	sums, err := checkChecksums(temp, root, signer)
	if err != nil {
		return fmt.Errorf("%w: %w", errRefused, err)
	}
	if _, ok := sums[apkName()]; !ok {
		return fmt.Errorf("%w: the release has no %s", errRefused, apkName())
	}
	if err := fetch("version", maxVersionBytes); err != nil {
		return err
	}
	if err := fetch(apkName(), maxAPKBytes); err != nil {
		return err
	}
	got, err := checkRelease(temp, root, signer)
	if err != nil {
		return fmt.Errorf("%w: %w", errRefused, err)
	}
	if got != version {
		return fmt.Errorf("%w: the release at %s says it's %s", errRefused, version, got)
	}
	if err := os.RemoveAll(a.Layout.Update); err != nil {
		return err
	}
	if err := os.Rename(temp, a.Layout.Update); err != nil {
		return err
	}
	a.setReady(UpdateReady{Version: version, APK: filepath.Join(a.Layout.Update, apkName())})
	return nil
}

// fetchReleaseFile downloads rawURL to dest, a new file only this account
// reads, refusing more than limit bytes. The release host is https, or
// plain http on loopback, for a test release's.
func (a *App) fetchReleaseFile(ctx context.Context, rawURL, dest string, limit int64) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	allowed := func(u *url.URL) bool { return u.Scheme == "https" || instance.LoopbackHTTP(u) }
	if !allowed(u) {
		return fmt.Errorf("the release host isn't https: %s", u.Redacted())
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !allowed(req.URL) || len(via) >= 5 {
			return errors.New("the release host redirected elsewhere than https")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if a.UserAgent != "" {
		req.Header.Set("User-Agent", a.UserAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the release host answered %s for %s", resp.Status, fileName(u))
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	if copyErr == nil {
		copyErr = f.Sync()
	}
	if err := errors.Join(copyErr, f.Close()); err != nil {
		return fmt.Errorf("download %s: %w", fileName(u), err)
	}
	if n > limit {
		return fmt.Errorf("%s is larger than %d bytes", fileName(u), limit)
	}
	return nil
}

// fileName is the last part of a release file's address, for messages.
func fileName(u *url.URL) string { return u.Path[strings.LastIndex(u.Path, "/")+1:] }

// checkChecksums checks dir's checksums against their bundle, signed as
// signer, and returns them by name.
func checkChecksums(dir string, root *release.TrustedRoot, signer release.Signer) (map[string]string, error) {
	checksums, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		return nil, err
	}
	bundle, err := os.ReadFile(filepath.Join(dir, "checksums.txt.cosign.bundle"))
	if err != nil {
		return nil, err
	}
	if err := root.Verify(checksums, bundle, signer); err != nil {
		return nil, fmt.Errorf("the release's checksums don't check out: %w", err)
	}
	return parseChecksums(checksums)
}

// checkRelease checks what dir holds of a release, its checksums against
// their bundle, and its version and this phone's APK against the
// checksums, and returns the version it holds.
func checkRelease(dir string, root *release.TrustedRoot, signer release.Signer) (string, error) {
	sums, err := checkChecksums(dir, root, signer)
	if err != nil {
		return "", err
	}
	for _, name := range []string{"version", apkName()} {
		want, ok := sums[name]
		if !ok {
			return "", fmt.Errorf("the release's checksums list no %s", name)
		}
		got, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		if got != want {
			return "", fmt.Errorf("%s doesn't match the release's checksums", name)
		}
	}
	v, err := os.ReadFile(filepath.Join(dir, "version"))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(v))
	if !semver.IsValid(version) {
		return "", errors.New("the release's version isn't one")
	}
	return version, nil
}

// checkRelease checks the Update directory's release against this build's
// trusted root and identity.
func (a *App) checkRelease(dir string) (string, error) {
	root, signer, err := a.releaseCheck()
	if err != nil {
		return "", err
	}
	return checkRelease(dir, root, signer)
}

// parseChecksums reads sha256sum's lines, each a digest and a name, which
// may be listed once.
func parseChecksums(data []byte) (map[string]string, error) {
	sums := map[string]string{}
	for line := range strings.Lines(string(data)) {
		sum, name, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "  ")
		if _, err := hex.DecodeString(sum); !ok || err != nil || len(sum) != 2*sha256.Size || name == "" {
			return nil, errors.New("the release's checksums have a line that isn't a checksum and a name")
		}
		if _, dup := sums[name]; dup {
			return nil, fmt.Errorf("the release's checksums list %s twice", name)
		}
		sums[name] = sum
	}
	return sums, nil
}

func fileSHA256(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
