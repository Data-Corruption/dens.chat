// Package build provides build-time information about the application.
package build

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// notices holds the licenses of the third-party software the binary
// includes, which scripts/notices.sh writes.
//
//go:embed notices.txt
var notices string

// Notices returns the licenses of the third-party software the binary
// includes.
func Notices() string { return notices }

// set by build.sh
var (
	name            string
	version         string
	contactURL      string
	releaseURL      string
	defaultLogLevel string
	// The cosign identity pins which workflow may sign installers that
	// dens update runs.
	certIdentity string
	oidcIssuer   string
	// A test release's own Sigstore trusted root, in base64, which the
	// Android app's updater checks its releases against in place of
	// Sigstore's public one (M12). Releases never set it.
	sigstoreRoot string
	devMode      string
)

type BuildInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ContactURL      string `json:"contactURL"`
	ReleaseURL      string `json:"releaseURL"`
	DefaultLogLevel string `json:"defaultLogLevel"`
	// CertIdentity and OidcIssuer are the cosign keyless identity an
	// installer must be signed with before dens update runs it. The identity
	// includes the repository and workflow path.
	CertIdentity string `json:"certIdentity"`
	OidcIssuer   string `json:"oidcIssuer"`
	// SigstoreRoot is a test release's own Sigstore trusted root, in
	// base64, in place of Sigstore's public one, as the Android e2e's
	// fixture releases sign with (M12). Empty in every release.
	SigstoreRoot string `json:"sigstoreRoot"`
	// DevMode builds run as a development instance: storage under the
	// developer's own data directory, no service manager, debug logging.
	// Only the default local build sets it.
	DevMode bool `json:"devMode"`
}

// UserAgent identifies this build to the servers it asks, by its name and
// minor version and where to reach its makers.
func (b BuildInfo) UserAgent() string {
	return fmt.Sprintf("Mozilla/5.0 (compatible; %s/%s; +%s)", b.Name, strings.TrimPrefix(semver.MajorMinor(b.Version), "v"), b.ContactURL)
}

// PrintJSON returns the build info as JSON.
func (b BuildInfo) PrintJSON() string {
	data, err := json.Marshal(b)
	if err != nil {
		return ""
	}
	return string(data)
}

func Info() BuildInfo {
	logLevel := defaultLogLevel
	if logLevel == "" {
		logLevel = "debug"
	}
	appName := name
	if appName == "" {
		// Unit tests and plain go builds have no ldflags.
		appName = "dens"
	}
	return BuildInfo{
		Name:            appName,
		Version:         version,
		ContactURL:      contactURL,
		ReleaseURL:      releaseURL,
		DefaultLogLevel: logLevel,
		CertIdentity:    certIdentity,
		OidcIssuer:      oidcIssuer,
		SigstoreRoot:    sigstoreRoot,
		DevMode:         devMode == "true",
	}
}
