package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/release"
	"github.com/Data-Corruption/dens.chat/internal/platform/release/sigstoretest"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// The identity a test release is signed as.
var testSigner = release.Signer{Identity: "https://example.test/release.yml@refs/heads/main", Issuer: "https://issuer.example.test"}

// releaseHost is a release host of test releases, signed by a Sigstore of
// the test's own: each version's files by name, which a test may change.
type releaseHost struct {
	t        *testing.T
	sigstore *sigstoretest.Sigstore
	mu       sync.Mutex
	latest   string
	files    map[string][]byte // releases/<version>/<name>
	asked    []string
}

func newReleaseHost(t *testing.T) (*releaseHost, *httptest.Server) {
	s, err := sigstoretest.New()
	if err != nil {
		t.Fatal(err)
	}
	h := &releaseHost{t: t, sigstore: s, files: map[string][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.asked = append(h.asked, r.URL.Path)
		if r.URL.Path == "/version" {
			fmt.Fprintln(w, h.latest)
			return
		}
		data, ok := h.files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return h, server
}

// publish puts version on the host with an APK, its checksums signed as
// signer, and makes it the latest.
func (h *releaseHost) publish(version string, apk []byte, signer release.Signer) {
	h.t.Helper()
	files := map[string][]byte{"version": []byte(version + "\n"), apkName(): apk}
	var sums strings.Builder
	for _, name := range []string{apkName(), "version"} {
		sum := sha256.Sum256(files[name])
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	files["checksums.txt"] = []byte(sums.String())
	bundle, err := h.sigstore.Sign(files["checksums.txt"], signer.Identity, signer.Issuer)
	if err != nil {
		h.t.Fatal(err)
	}
	files["checksums.txt.cosign.bundle"] = bundle
	h.mu.Lock()
	defer h.mu.Unlock()
	for name, data := range files {
		h.files["releases/"+version+"/"+name] = data
	}
	h.latest = version
}

func (h *releaseHost) set(version, name string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.files["releases/"+version+"/"+name] = data
}

func (h *releaseHost) fetched(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.asked {
		if strings.HasSuffix(p, "/"+name) {
			return true
		}
	}
	return false
}

// shellRecorder stands in for the Android app's shell.
type shellRecorder struct {
	mu        sync.Mutex
	ready     []UpdateReady
	installed []UpdateReady
}

func (s *shellRecorder) UpdateReady(u UpdateReady) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = append(s.ready, u)
}

func (s *shellRecorder) InstallUpdate(u UpdateReady) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installed = append(s.installed, u)
}

func (s *shellRecorder) told() ([]UpdateReady, []UpdateReady) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]UpdateReady(nil), s.ready...), append([]UpdateReady(nil), s.installed...)
}

// androidApp is the Android app's service, at version, in the test's own
// files, whose release host is host and whose test release checks its
// signatures against root.
func androidApp(t *testing.T, files, version, host string, root []byte) *App {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", files)
	bi := build.BuildInfo{
		Name: "dens", Version: version, DefaultLogLevel: "error",
		CertIdentity: testSigner.Identity, OidcIssuer: testSigner.Issuer,
		SigstoreRoot: base64.StdEncoding.EncodeToString(root),
	}
	a := New(bi)
	l, err := layout.New("dens", "main", layout.Android)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.EnsureSession(); err != nil {
		t.Fatal(err)
	}
	a.Layout = l
	a.Instance = instance.Config{ReleaseURL: host + "/"}
	if a.Log, err = xlog.New(l.Logs, "error"); err != nil {
		t.Fatal(err)
	}
	if a.DB, err = database.New(l.DB, a.Log, bi, database.ApplyPendingMigrations); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = a.DB.Close()
		_ = a.Log.Close()
	})
	a.ReleaseSource = &release.GenericReleaseSource{}
	return a
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// checkNow checks for an update and readies what it finds, as the
// checker's loop does.
func checkNow(t *testing.T, a *App) {
	t.Helper()
	if err := a.CheckForUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.readyAndroidUpdate(context.Background())
}

// The Android app's updater downloads a newer release's APK, checked
// against its checksums and their signature, tells the shell it's ready,
// and asks the shell to install it when the member does; a start finds it
// ready again, and the next version's start removes it (M12).
func TestAndroidUpdaterReadiesACheckedAPK(t *testing.T) {
	host, server := newReleaseHost(t)
	files := filepath.Join(t.TempDir(), "files")
	apk := []byte("the APK of v99.0.2")
	host.publish("v99.0.2-e2e", apk, testSigner)

	a := androidApp(t, files, "v99.0.1-e2e", server.URL, host.sigstore.Root())
	if !a.ChecksForUpdates() {
		t.Fatal("the Android app with a release host doesn't check for updates")
	}
	shell := &shellRecorder{}
	a.SetUpdateShell(shell)
	if err := a.InstallUpdate(); err == nil {
		t.Fatal("an install was asked for with no update ready")
	}
	checkNow(t, a)
	ready, ok := a.ReadyUpdate()
	if !ok || ready.Version != "v99.0.2-e2e" || ready.APK != filepath.Join(a.Layout.Update, apkName()) {
		t.Fatalf("ready = %+v, %v", ready, ok)
	}
	if got, err := os.ReadFile(ready.APK); err != nil || string(got) != string(apk) {
		t.Fatalf("the kept APK: %q, %v", got, err)
	}
	if info, err := os.Stat(ready.APK); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the kept APK's mode: %v, %v", info.Mode(), err)
	}
	told, _ := shell.told()
	if len(told) != 1 || told[0] != ready {
		t.Fatalf("the shell heard %+v", told)
	}
	if err := a.InstallUpdate(); err != nil {
		t.Fatal(err)
	}
	if _, installed := shell.told(); len(installed) != 1 || installed[0] != ready {
		t.Fatalf("the shell was asked to install %+v", installed)
	}
	// Once it's ready, the next check downloads nothing again.
	host.set("v99.0.2-e2e", apkName(), []byte("changed"))
	checkNow(t, a)
	if got, _ := os.ReadFile(ready.APK); string(got) != string(apk) {
		t.Fatal("a second check downloaded the APK again")
	}

	// A start finds it ready again, and a member who turned notices off
	// isn't told of it.
	if _, err := config.Update(a.DB, func(c *types.Configuration) error { c.UpdateNotifications = false; return nil }); err != nil {
		t.Fatal(err)
	}
	again := androidApp(t, files, "v99.0.1-e2e", server.URL, host.sigstore.Root())
	again.checkStagedUpdate()
	if r, ok := again.ReadyUpdate(); !ok || r != ready {
		t.Fatalf("a start found %+v, %v", r, ok)
	}
	quiet := &shellRecorder{}
	again.SetUpdateShell(quiet)
	if told, _ := quiet.told(); len(told) != 0 {
		t.Fatalf("a member who turned notices off heard of the update: %+v", told)
	}

	// The version it installed removes it as it starts.
	updated := androidApp(t, files, "v99.0.2-e2e", server.URL, host.sigstore.Root())
	updated.checkStagedUpdate()
	if _, ok := updated.ReadyUpdate(); ok {
		t.Fatal("the installed version is still offered")
	}
	if entries, _ := os.ReadDir(updated.Layout.Update); len(entries) != 0 {
		t.Fatalf("the installed version's start left %d files", len(entries))
	}
}

// A release the updater can't vouch for is never offered: signed by
// another identity or another Sigstore, its APK or version unlike its
// checksums, or no APK for this phone. A kept update that no longer checks
// out goes as a start finds it.
func TestAndroidUpdaterRefuses(t *testing.T) {
	apk := []byte("an APK")
	other, err := sigstoretest.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		why     string
		publish func(h *releaseHost)
		fetches bool   // whether the APK is downloaded before the refusal
		says    string // what the refusal says
	}{
		{"another workflow", func(h *releaseHost) {
			h.publish("v99.0.2-e2e", apk, release.Signer{Identity: "https://example.test/other.yml@refs/heads/main", Issuer: testSigner.Issuer})
		}, false, "doesn't name"},
		{"another Sigstore", func(h *releaseHost) {
			h.publish("v99.0.2-e2e", apk, testSigner)
			bundle, err := other.Sign(h.files["releases/v99.0.2-e2e/checksums.txt"], testSigner.Identity, testSigner.Issuer)
			if err != nil {
				t.Fatal(err)
			}
			h.set("v99.0.2-e2e", "checksums.txt.cosign.bundle", bundle)
		}, false, "no log the root trusts"},
		{"an APK unlike its checksum", func(h *releaseHost) {
			h.publish("v99.0.2-e2e", apk, testSigner)
			h.set("v99.0.2-e2e", apkName(), []byte("another APK"))
		}, true, "doesn't match the release's checksums"},
		{"a version file naming another version", func(h *releaseHost) {
			h.publish("v99.0.2-e2e", apk, testSigner)
			h.mu.Lock()
			h.latest = "v99.0.3-e2e"
			for name, data := range h.files {
				h.files[strings.Replace(name, "v99.0.2-e2e", "v99.0.3-e2e", 1)] = data
			}
			h.mu.Unlock()
		}, true, "says it's v99.0.2-e2e"},
		{"no APK for this phone", func(h *releaseHost) {
			h.publish("v99.0.2-e2e", apk, testSigner)
			sums := []byte(strings.ReplaceAll(string(h.files["releases/v99.0.2-e2e/checksums.txt"]), apkName(), "android-other.apk"))
			bundle, err := h.sigstore.Sign(sums, testSigner.Identity, testSigner.Issuer)
			if err != nil {
				t.Fatal(err)
			}
			h.set("v99.0.2-e2e", "checksums.txt", sums)
			h.set("v99.0.2-e2e", "checksums.txt.cosign.bundle", bundle)
		}, false, "has no " + apkName()},
	} {
		t.Run(c.why, func(t *testing.T) {
			host, server := newReleaseHost(t)
			c.publish(host)
			a := androidApp(t, filepath.Join(t.TempDir(), "files"), "v99.0.1-e2e", server.URL, host.sigstore.Root())
			if err := a.CheckForUpdate(context.Background()); err != nil {
				t.Fatal(err)
			}
			latest, _ := a.LatestUpdate(must(config.View(a.DB)))
			if err := a.stageUpdate(context.Background(), latest); err == nil || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("readying it: %v, want it to say %q", err, c.says)
			}
			if r, ok := a.ReadyUpdate(); ok {
				t.Fatalf("offered %+v", r)
			}
			if host.fetched(apkName()) != c.fetches {
				t.Errorf("the APK fetched: %v, want %v", host.fetched(apkName()), c.fetches)
			}
			if entries, _ := os.ReadDir(a.Layout.Update); len(entries) != 0 {
				t.Fatalf("the refused release left %d files", len(entries))
			}
			if left, _ := filepath.Glob(a.Layout.Update + "-*"); len(left) != 0 {
				t.Fatalf("the refused release left %v", left)
			}
		})
	}

	// A kept update changed since it checked out goes.
	host, server := newReleaseHost(t)
	host.publish("v99.0.2-e2e", apk, testSigner)
	files := filepath.Join(t.TempDir(), "files")
	a := androidApp(t, files, "v99.0.1-e2e", server.URL, host.sigstore.Root())
	checkNow(t, a)
	ready, ok := a.ReadyUpdate()
	if !ok {
		t.Fatal("no update ready")
	}
	if err := os.WriteFile(ready.APK, []byte("changed since"), 0o600); err != nil {
		t.Fatal(err)
	}
	again := androidApp(t, files, "v99.0.1-e2e", server.URL, host.sigstore.Root())
	again.checkStagedUpdate()
	if r, ok := again.ReadyUpdate(); ok {
		t.Fatalf("a changed APK was offered: %+v", r)
	}
	if _, err := os.Stat(ready.APK); !os.IsNotExist(err) {
		t.Fatalf("the changed APK stayed: %v", err)
	}

	// A release that doesn't check out is tried again a day later, not at
	// each hour's look: its APK isn't downloaded each hour.
	host.publish("v99.0.3-e2e", apk, testSigner)
	host.set("v99.0.3-e2e", apkName(), []byte("not the APK the checksums name"))
	refusing := androidApp(t, filepath.Join(t.TempDir(), "files"), "v99.0.1-e2e", server.URL, host.sigstore.Root())
	checkNow(t, refusing)
	refusing.readyAndroidUpdate(context.Background())
	host.mu.Lock()
	apkFetches := 0
	for _, p := range host.asked {
		if p == "/releases/v99.0.3-e2e/"+apkName() {
			apkFetches++
		}
	}
	host.mu.Unlock()
	if apkFetches != 1 {
		t.Fatalf("a refused release's APK was fetched %d times, not once", apkFetches)
	}
	refusing.apk.refusedAt = time.Now().Add(-UpdateCheckInterval)
	host.set("v99.0.3-e2e", apkName(), apk)
	refusing.readyAndroidUpdate(context.Background())
	if r, ok := refusing.ReadyUpdate(); !ok || r.Version != "v99.0.3-e2e" {
		t.Fatalf("a day later, the fixed release isn't ready: %+v, %v", r, ok)
	}

	// An older release, or none newer, is never downloaded.
	host.publish("v99.0.0-e2e", apk, testSigner)
	newer := androidApp(t, files, "v99.0.1-e2e", server.URL, host.sigstore.Root())
	checkNow(t, newer)
	if r, ok := newer.ReadyUpdate(); ok {
		t.Fatalf("an older release was offered: %+v", r)
	}
}

// A build without the release identity checks no release, so it updates
// from none, and only https reaches a release host, but for a test
// release's on loopback.
func TestAndroidUpdaterNeedsAnIdentityAndHTTPS(t *testing.T) {
	a := New(build.BuildInfo{Name: "dens", Version: "v1.5.0"})
	if _, _, err := a.releaseCheck(); err == nil {
		t.Error("a build without an identity checks releases")
	}
	a = New(build.BuildInfo{Name: "dens", Version: "v1.5.0", CertIdentity: testSigner.Identity, OidcIssuer: testSigner.Issuer})
	if root, _, err := a.releaseCheck(); err != nil || root == nil {
		t.Errorf("a release's root: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "file")
	for _, u := range []string{"http://releases.example.test/version", "ftp://releases.example.test/version"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := a.fetchReleaseFile(ctx, u, dest, 64); err == nil || !strings.Contains(err.Error(), "https") {
			t.Errorf("%s: %v", u, err)
		}
		cancel()
	}
}
