package app

import (
	"runtime"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
)

// The update notice gives what installs the update. On Windows, for the
// default instance of the build's own release host, that's the install
// guide's line, which needs no elevated terminal; otherwise it's dens
// update, as an administrator runs it, since the line fetches from the
// build's host, not the instance's, and installs the default instance
// where it isn't.
func TestUpdateCommand(t *testing.T) {
	const releases = "https://releases.dens.chat/"
	for _, c := range []struct {
		instance, source string
		linux, windows   string
	}{
		{"main", releases, "sudo dens update", "irm 'https://releases.dens.chat/install.ps1' | iex"},
		{"main", "https://releases.dens.chat", "sudo dens update", "irm 'https://releases.dens.chat/install.ps1' | iex"},
		{"main", "https://mirror.example/dens/", "sudo dens update", "dens update   (in an elevated terminal)"},
		{"second", releases, "sudo dens update --instance second", "dens update --instance second   (in an elevated terminal)"},
	} {
		a := &App{
			buildInfo: build.BuildInfo{Name: "dens", Version: "v1.10.0", ReleaseURL: releases},
			Layout:    layout.Layout{Form: layout.Installed, Instance: c.instance},
			Instance:  instance.Config{ReleaseURL: c.source},
		}
		want := c.linux
		if runtime.GOOS == "windows" {
			want = c.windows
		}
		if got := a.UpdateCommand(); got != want {
			t.Errorf("instance %s from %s: %q, want %q", c.instance, c.source, got, want)
		}
	}
}
