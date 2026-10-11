package commands

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/shortcuts"

	"github.com/urfave/cli/v3"
)

// shortcutsCommand is the shortcut helper (M7): it runs as the desktop user
// in their session, for every instance that answers them, and carries the
// voice keys to the page's call from other programs.
func shortcutsCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:   shortcuts.Command,
		Hidden: true,
		Usage:  "carry the voice keys to Dens from other programs (started at login)",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			h := &shortcuts.Helper{
				Version:   bi.Version,
				Instances: func() ([]string, error) { return layout.Instances(bi.Name, bi.DevMode) },
				Dial: func(name string) (control.Client, error) {
					client, _, err := controlClient(bi, name)
					return client, err
				},
				Logf: func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) },
			}
			// Under Wayland the desktop owns the keys, through its portal;
			// elsewhere the helper reads them itself.
			if desk, deskErr := host.OpenDesktopKeys(shortcuts.AppID); deskErr == nil {
				first := desk
				h.OpenDesktop = func() (shortcuts.DesktopKeys, error) {
					if first != nil {
						d := first
						first = nil
						return d, nil
					}
					d, err := host.OpenDesktopKeys(shortcuts.AppID)
					if err != nil {
						return nil, err
					}
					return d, nil
				}
			} else if keys, err := host.OpenKeyState(); err != nil {
				h.Problem = errors.Join(deskErr, err).Error()
			} else {
				defer keys.Close()
				h.Keys, h.Note = keys, keys.Note()
			}
			if l, err := layout.New(bi.Name, layout.DefaultInstance, bi.DevMode); err == nil && l.Binary != "" {
				h.Restart = func() error { return host.ReplaceProcess(l.Binary, shortcuts.Command) }
			}
			err := h.Run(ctx)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		},
	}
}
