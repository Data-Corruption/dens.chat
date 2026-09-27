package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/instance"
)

// SkipVerifyEnv skips cosign verification of the installer, for testing
// against unsigned local releases. The installers honor the same variable.
const SkipVerifyEnv = "APP_SKIP_VERIFY"

const maxInstallerSize = 16 << 20

// Update fetches the release's installer and its signature bundle, verifies
// the pair against the signing identity built into this binary, and runs the
// installer in the foreground. The installer downloads and verifies the
// release binary and runs its install command, which updates every instance.
func Update(ctx context.Context, sys System, bi build.BuildInfo, opts Options) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	if bi.DevMode {
		return errors.New("development builds don't update; build a new one")
	}
	if err := sys.CheckAdmin(); err != nil {
		return err
	}
	l, err := sys.Layout(opts.Instance)
	if err != nil {
		return err
	}
	cfg, err := instance.Read(l.InstanceConfig)
	if err != nil {
		return fmt.Errorf("instance %s isn't installed: %w", opts.Instance, err)
	}
	source := cfg.ReleaseURL
	if opts.ReleaseURL != "" {
		source = opts.ReleaseURL
	}
	if source == "" {
		return errors.New("this instance has no release source; install a new release with the installer")
	}

	temp, err := os.MkdirTemp("", "dens-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	name := sys.InstallerName()
	script := filepath.Join(temp, name)
	bundle := script + ".cosign.bundle"
	fmt.Fprintf(out, "Downloading %s%s ...\n", source, name)
	if err := download(ctx, source+name, script); err != nil {
		return err
	}
	if err := download(ctx, source+name+".cosign.bundle", bundle); err != nil {
		return err
	}

	env := []string{"APP_RELEASE_URL=" + strings.TrimRight(source, "/")}
	if skipVerify() {
		fmt.Fprintf(out, "%s is set: NOT verifying the installer's signature. Only for testing.\n", SkipVerifyEnv)
		env = append(env, SkipVerifyEnv+"=true")
	} else {
		fmt.Fprintln(out, "Verifying the installer's signature ...")
		if err := verifyBlob(bi, l.Cosign, script, bundle); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "Running the installer ...")
	return sys.RunInstaller(ctx, script, opts.Instance, env)
}

func skipVerify() bool {
	value := os.Getenv(SkipVerifyEnv)
	return value == "true" || value == "1"
}

// verifyBlob checks a file's cosign bundle against the workflow identity
// the release pipeline signs with.
func verifyBlob(bi build.BuildInfo, managedCosign, file, bundle string) error {
	if bi.CertIdentity == "" || bi.OidcIssuer == "" {
		return errors.New("this binary wasn't built by the release pipeline, so it can't verify a release; install with the installer instead")
	}
	cosign := managedCosign
	if _, err := os.Stat(cosign); err != nil {
		if cosign, err = exec.LookPath("cosign"); err != nil {
			return errors.New("cosign is missing; run the installer again to restore it")
		}
	}
	cmd := exec.Command(cosign, "verify-blob",
		"--bundle", bundle,
		"--certificate-identity", bi.CertIdentity,
		"--certificate-oidc-issuer", bi.OidcIssuer,
		file)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the installer's signature doesn't verify: %w\n%s", err, strings.TrimSpace(output.String()))
	}
	return nil
}

// download fetches rawURL to dest. file:// URLs serve local test releases.
func download(ctx context.Context, rawURL, dest string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	var body io.ReadCloser
	switch u.Scheme {
	case "file":
		if body, err = os.Open(filepath.FromSlash(u.Path)); err != nil {
			return err
		}
	case "https":
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("download %s: %s", rawURL, resp.Status)
		}
		body = resp.Body
	default:
		return fmt.Errorf("release URL %s must use https", rawURL)
	}
	defer body.Close()
	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(file, io.LimitReader(body, maxInstallerSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("download %s: %w", rawURL, copyErr)
	}
	if n > maxInstallerSize {
		return fmt.Errorf("download %s: larger than %d bytes", rawURL, maxInstallerSize)
	}
	return closeErr
}
