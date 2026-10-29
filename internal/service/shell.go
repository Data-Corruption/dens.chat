package service

import (
	"context"
	"encoding/json"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/notify"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/guard"
	"github.com/Data-Corruption/dens.chat/internal/types"
)

// The stream between the Android app's shell and the service (M12). The
// shell opens it once the service answers, and again whenever it closes,
// as when the shell starts the service again. Lines on it, besides pings:
// the service sends hello first, saying what the shell needs to show the
// page, and from M12.3 notify for a notification to post, which replaces
// its channel's, unnotify for those to take back, and notifying, whether
// the member has any turned on, which the shell asks Android to let it
// post. The shell sends front as the app comes in front and leaves it, and
// notifies, whether Android lets it post notifications, as the stream
// opens and as that changes.
const (
	msgShellHello     = "hello"
	msgShellNotify    = "notify"
	msgShellUnnotify  = "unnotify"
	msgShellNotifying = "notifying"
	msgShellFront     = "front"
	msgShellNotifies  = "notifies"
)

// shellHello is the service's first line on the stream: its version, and
// the page's address, whose port the instance config holds.
type shellHello struct {
	Version string `json:"version"`
	Page    string `json:"page"`
}

// shellSwitch is a line that turns something on or off.
type shellSwitch struct {
	On bool `json:"on"`
}

// shellUnnotify names the notifications to take back, by their keys.
type shellUnnotify struct {
	Keys []string `json:"keys"`
}

// shellPlace is the shell, as a place notifications show (M12.3).
type shellPlace struct{ s *control.Stream }

func (p shellPlace) Show(n notify.Notification) bool { return p.s.Send(msgShellNotify, n) == nil }

func (p shellPlace) Withdraw(keys []string) {
	_ = p.s.Send(msgShellUnnotify, shellUnnotify{Keys: keys})
}

func (p shellPlace) Settings(s types.Notifications) {
	_ = p.s.Send(msgShellNotifying, shellSwitch{On: s.DMs || s.Mentions})
}

// shellStream serves the shell's stream until either side closes it, or
// the service stops.
func shellStream(a *app.App) func(ctx context.Context, s *control.Stream) error {
	return func(ctx context.Context, s *control.Stream) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		context.AfterFunc(ctx, func() { _ = s.Close() })
		hello := shellHello{Version: a.BuildInfo().Version, Page: guard.PageOrigin(a.Instance.ClientPort) + "/"}
		if err := s.Send(msgShellHello, hello); err != nil {
			return err
		}
		a.Log.Info("The app's shell connected")
		place := shellPlace{s}
		a.Notify.SetShell(place)
		defer a.Notify.DropShell(place)
		go s.KeepAlive(ctx)
		for {
			// A newer shell's lines this service doesn't know, it ignores.
			msg, err := s.Recv()
			if err != nil {
				a.Log.Info("The app's shell disconnected")
				return err
			}
			var on shellSwitch
			switch msg.T {
			case msgShellFront:
				if json.Unmarshal(msg.D, &on) == nil {
					a.Notify.SetFront(on.On)
				}
			case msgShellNotifies:
				if json.Unmarshal(msg.D, &on) == nil {
					a.Notify.SetAllowed(on.On)
				}
			}
		}
	}
}
