package commands

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Data-Corruption/dens.chat/internal/app"
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
				Instances: func() ([]string, error) { return layout.Instances(bi.Name, app.FormOf(bi)) },
				Open: func(name string) (*control.Stream, error) {
					client, _, err := controlClient(bi, name)
					if err != nil {
						return nil, err
					}
					return client.Open(control.Request{Op: control.OpShortcuts})
				},
				Logf: func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) },
			}
			// Under Wayland the desktop owns the keys, through its portal;
			// elsewhere the helper reads them itself.
			defer h.UseSystem(false)()
			if l, err := layout.New(bi.Name, layout.DefaultInstance, app.FormOf(bi)); err == nil && l.Binary != "" {
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
