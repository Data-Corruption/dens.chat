package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"

	"github.com/urfave/cli/v3"
	"golang.org/x/mod/semver"
)

// startWait bounds how long a launch waits for the service it starts: a
// start waits up to 30 seconds for an older one to stop, and may migrate.
const startWait = 5 * time.Minute

// spawnLatest is Flatpak's portal's Spawn; tests answer for it.
var spawnLatest = host.SpawnLatest

// launchCommand is what Dens's desktop file runs (M9.3): it reaches the
// running service, or starts one, then opens Dens in the browser, pairing
// it. The Flatpak's service is started through Flatpak's portal, as a
// sandbox of its own that nothing the launch ran in holds, and one older
// than the launch stops for the newest build to take over, so a launch
// never pairs with it. Elsewhere the service manager starts the service,
// and this is dens open.
func launchCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:   "launch",
		Hidden: true,
		Usage:  "open Dens in your browser, starting it first if it isn't running (its desktop file runs this)",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return launch(ctx, bi, cmd.String("instance"))
		},
	}
}

func launch(ctx context.Context, bi build.BuildInfo, instance string) error {
	client, l, err := controlClient(bi, instance)
	if err != nil {
		return err
	}
	if l.Form == layout.Flatpak {
		if err := ensureFlatpakService(ctx, bi, client, l); err != nil {
			// From the apps menu nothing shows a launch's output.
			_ = host.Notify("launch", "Dens didn't start", err.Error())
			return err
		}
	}
	return openPage(bi, client, l, false)
}

// ensureFlatpakService has a service of this build or newer running. One
// launch at a time starts one, so two at once start a service between
// them: the second waits for the first, then finds its service.
func ensureFlatpakService(ctx context.Context, bi build.BuildInfo, client control.Client, l layout.Layout) error {
	if err := os.MkdirAll(l.Runtime, 0o700); err != nil {
		return err
	}
	lock, err := xsyscall.AcquireLock(ctx, filepath.Join(l.Runtime, "launch.lock"), xsyscall.LockOptions{
		Mode: xsyscall.ModeExclusive, Timeout: startWait, Poll: 100 * time.Millisecond,
	})
	if err != nil {
		return fmt.Errorf("wait for another launch of Dens: %w", err)
	}
	defer lock.Close()
	var status control.Status
	err = client.Call(control.Request{Op: control.OpStatus}, &status)
	switch {
	case err == nil && !olderThan(status.Version, bi.Version):
		return nil
	case err == nil:
		fmt.Printf("Dens %s is running; %s takes over.\n", status.Version, bi.Version)
		if err := client.Call(control.Request{Op: control.OpQuit}, nil); err != nil {
			return explainCallError(bi, l, err)
		}
	case !notRunning(err):
		return explainCallError(bi, l, err)
	}
	return spawnService(ctx, bi, client, l)
}

// olderThan says whether a service's version is before the launch's.
// Versions that aren't semantic, as a development build's, never are.
func olderThan(service, launch string) bool {
	return semver.IsValid(service) && semver.IsValid(launch) && semver.Compare(service, launch) < 0
}

// spawnService starts the Flatpak's service from the newest build
// installed, and waits for it to say it's ready, or why it stopped, on a
// pipe it hands it. Another service of this build or newer may answer
// first, as one starting at sign-in: then the one this starts waits for its
// lock, and the launch takes whichever answers.
func spawnService(ctx context.Context, bi build.BuildInfo, client control.Client, l layout.Layout) error {
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer r.Close()
	argv := []string{bi.Name}
	if l.Instance != layout.DefaultInstance {
		argv = append(argv, "--instance", l.Instance)
	}
	argv = append(argv, "service", "run", "--ready-fd", "3")
	err = spawnLatest(argv, w)
	// The new process has its own copy now, so the read ends when it
	// closes that.
	w.Close()
	if err != nil {
		return fmt.Errorf("start Dens: %w", err)
	}
	said := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(io.LimitReader(r, 64<<10))
		said <- strings.TrimSpace(string(data))
	}()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	timeout := time.After(startWait)
	for {
		select {
		case answer := <-said:
			switch answer {
			case readyWord:
				return nil
			case "":
				return fmt.Errorf("Dens stopped as it started; its log in %s says why", l.Logs)
			}
			return errors.New(answer)
		case <-poll.C:
			var status control.Status
			if client.Call(control.Request{Op: control.OpStatus}, &status) == nil && !olderThan(status.Version, bi.Version) {
				return nil
			}
		case <-timeout:
			return fmt.Errorf("Dens didn't start within %v; its log in %s says why", startWait, l.Logs)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
