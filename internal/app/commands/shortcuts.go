package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/notify"
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
			l, err := layout.New(bi.Name, layout.DefaultInstance, app.FormOf(bi))
			if err == nil && l.Binary != "" {
				h.Restart = func() error { return host.ReplaceProcess(l.Binary, shortcuts.Command) }
			}
			// Notifications show as the desktop's own where it has something
			// to show them with (M12.3), and a click opens the page paired,
			// as dens open opens it.
			if notes, err := openNotifier(l); err != nil {
				h.Logf("Notifications show only in a Dens tab: %v", err)
			} else {
				defer notes.Close()
				h.Notifier = notes
			}
			h.OpenPage = func(instance string, n notify.Notification) error {
				client, _, err := controlClient(bi, instance)
				if err != nil {
					return err
				}
				var p control.Pair
				if err := client.Call(control.Request{Op: control.OpPair}, &p); err != nil {
					return err
				}
				origin, token, ok := strings.Cut(p.URL, "/#token=")
				if !ok {
					return fmt.Errorf("the service's pairing link is %d characters with no token", len(p.URL))
				}
				return host.OpenURL(n.URL(origin, token))
			}
			err = h.Run(ctx)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		},
	}
}

// openNotifier opens the desktop's notifications for the helper. Windows
// shows Dens's toasts only under the app ID its Start menu shortcut
// carries, which the installer makes.
func openNotifier(l layout.Layout) (*host.Notifier, error) {
	if l.StartMenuShortcut != "" {
		if _, err := os.Stat(l.StartMenuShortcut); err != nil {
			return nil, fmt.Errorf("Windows shows Dens's notifications once its Start menu shortcut is there: %w", err)
		}
	}
	return host.OpenNotifier(layout.DesktopID, false)
}
