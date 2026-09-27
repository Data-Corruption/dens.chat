// Package release resolves the newest published version from a release host.
//
// A release host publishes one root `version` object containing a single
// SemVer string. Installers read that pointer and pin it for the rest of their
// run; this package reads the same object so the application and the
// installer can never disagree about what "latest" means. Everything else
// about a release (checksums, signatures, binaries) lives under the immutable
// `releases/<version>/` prefix and is the installer's concern, not ours.
package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

// maxVersionResponseBytes bounds the pointer read. A valid SemVer string is a
// few bytes; anything approaching this limit is a misconfigured or hostile host.
const maxVersionResponseBytes = 64

// ErrInvalidReleaseURL reports a release URL the source refuses to contact.
var ErrInvalidReleaseURL = errors.New("invalid release URL")

// ReleaseSource answers "what is the newest published version?" for a release
// URL. The App holds one so tests can substitute a fake without a network.
type ReleaseSource interface {
	GetLatestVersion(ctx context.Context, releaseURL string) (string, error)
}

// GenericReleaseSource reads the root version pointer over HTTP or HTTPS,
// or from a local release at a file:// URL, which tests install from.
//
// It has no timeout of its own. Callers bound each check with their context,
// and a second, hidden deadline here would only make the effective limit
// whichever one happens to be shorter.
type GenericReleaseSource struct {
	// UserAgent identifies this application to the release host. The App
	// builds one from its name, version, and contact URL so a host operator
	// can tell installations apart from browsers and bots. Empty falls back to
	// Go's default.
	UserAgent string

	// Client is optional. The zero value uses http.DefaultTransport with no
	// client-level timeout, which is what every caller in this project wants.
	Client *http.Client
}

// GetLatestVersion reads `<releaseURL>/version` and returns its trimmed,
// validated SemVer content.
func (s *GenericReleaseSource) GetLatestVersion(ctx context.Context, releaseURL string) (string, error) {
	target, err := versionURL(releaseURL)
	if err != nil {
		return "", err
	}
	var body []byte
	if target.Scheme == "file" {
		body, err = readVersionFile(LocalPath(target))
	} else {
		body, err = s.fetch(ctx, target.String())
	}
	if err != nil {
		return "", err
	}

	version := strings.TrimSpace(string(body))
	switch {
	case version == "":
		return "", fmt.Errorf("version pointer is empty")
	case !semver.IsValid(version):
		return "", fmt.Errorf("version pointer %q is not a valid semantic version", version)
	}
	return version, nil
}

func (s *GenericReleaseSource) fetch(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build version request: %w", err)
	}
	if s.UserAgent != "" {
		req.Header.Set("User-Agent", s.UserAgent)
	}
	req.Header.Set("Accept", "text/plain")
	// The pointer is the one object on the host that moves. Ask intermediaries
	// not to hand back a stale copy that predates a promotion.
	req.Header.Set("Cache-Control", "no-cache")

	client := s.Client
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: unexpected status %s", target, resp.Status)
	}
	return readPointer(resp.Body)
}

func readVersionFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read version pointer: %w", err)
	}
	defer file.Close()
	return readPointer(file)
}

func readPointer(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxVersionResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read version pointer: %w", err)
	}
	if len(body) > maxVersionResponseBytes {
		return nil, fmt.Errorf("version pointer exceeds %d bytes", maxVersionResponseBytes)
	}
	return body, nil
}

// LocalPath returns the local file path a file:// URL names. On Windows,
// file:///C:/dir parses to the path /C:/dir; the slash before the drive
// letter goes.
func LocalPath(u *url.URL) string {
	path := u.Path
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

// versionURL joins the root pointer name onto a validated release URL: an
// http or https host, or a local file:// release. Any other scheme is a
// configuration error worth surfacing rather than something to try anyway.
func versionURL(releaseURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(releaseURL))
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", ErrInvalidReleaseURL, releaseURL, err)
	}
	switch parsed.Scheme {
	case "https", "http":
		if parsed.Host == "" {
			return nil, fmt.Errorf("%w %q: missing host", ErrInvalidReleaseURL, releaseURL)
		}
	case "file":
		if parsed.Host != "" || parsed.Path == "" {
			return nil, fmt.Errorf("%w %q: want file:///path/", ErrInvalidReleaseURL, releaseURL)
		}
	default:
		return nil, fmt.Errorf("%w %q: scheme must be https, http or file", ErrInvalidReleaseURL, releaseURL)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/version"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}
