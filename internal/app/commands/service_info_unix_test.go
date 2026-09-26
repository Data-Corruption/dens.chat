//go:build !windows

package commands

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/layout"
)

func TestServiceHelpDistinguishesConfigFromOptionalEnvironment(t *testing.T) {
	a := app.New(build.BuildInfo{Name: "sprout"})
	a.Layout = layout.FromStorage(filepath.Join(t.TempDir(), "state"), "sprout")
	var output bytes.Buffer

	printServiceHelpTo(&output, a)
	got := output.String()
	for _, want := range []string{
		"sprout config set --help  (persistent application settings)",
		a.Layout.Env + " then restart  (optional systemd environment)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("service help missing %q:\n%s", want, got)
		}
	}
}
