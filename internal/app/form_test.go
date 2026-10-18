package app

import (
	"context"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/types"
)

// A development build runs as a development instance, and every other
// build, a test release's too, as an installed one (M9.2).
func TestFormOfBuild(t *testing.T) {
	for _, c := range []struct {
		bi   build.BuildInfo
		want layout.Form
	}{
		{build.BuildInfo{Name: "dens", Version: "v0.0.0-dev", DevMode: true}, layout.Development},
		{build.BuildInfo{Name: "dens", Version: "v1.1.0"}, layout.Installed},
		{build.BuildInfo{Name: "dens", Version: "v99.0.1-e2e"}, layout.Installed},
	} {
		if got := FormOf(c.bi); got != c.want {
			t.Errorf("FormOf(%s, dev %v) = %s, want %s", c.bi.Version, c.bi.DevMode, got, c.want)
		}
	}
}

// Only an installed instance asks its release host for updates and tells
// the member of one; a development build has nothing to update to.
func TestOnlyAnInstalledInstanceChecksForUpdates(t *testing.T) {
	const source = "https://releases.example"
	found := &types.Configuration{UpdateCheckSource: source, LatestUpdateVersion: "v1.2.0"}
	for _, c := range []struct {
		form    layout.Form
		release string
		checks  bool
	}{
		{layout.Installed, source, true},
		{layout.Installed, "", false},
		{layout.Development, source, false},
	} {
		a := &App{
			buildInfo: build.BuildInfo{Name: "dens", Version: "v1.1.0"},
			Layout:    layout.Layout{Form: c.form},
			Instance:  instance.Config{ReleaseURL: c.release},
		}
		latest, notice := a.LatestUpdate(found)
		if a.ChecksForUpdates() != c.checks || notice != c.checks {
			t.Errorf("%s, release host %q: checks %v, notice %v (%s); want %v", c.form, c.release, a.ChecksForUpdates(), notice, latest, c.checks)
		}
		// Asking would need the release host, so only a refusal is tried.
		if !c.checks {
			if err := a.CheckForUpdate(context.Background()); err == nil {
				t.Errorf("%s, release host %q: CheckForUpdate asked", c.form, c.release)
			}
		}
	}
}
