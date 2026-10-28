package service

import (
	"context"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/guard"
)

// The stream between the Android app's shell and the service (M12). The
// shell opens it once the service answers, and again whenever it closes,
// as when the shell starts the service again. The service's first line
// says what the shell needs to show the page; what the shell does for the
// service comes over it from M12.2 on.
const msgShellHello = "hello"

// shellHello is the service's first line on the stream: its version, and
// the page's address, whose port the instance config holds.
type shellHello struct {
	Version string `json:"version"`
	Page    string `json:"page"`
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
		go s.KeepAlive(ctx)
		for {
			// Nothing the shell sends needs an answer yet: its pings keep
			// the stream, and a newer shell's lines this service doesn't
			// know, it ignores.
			if _, err := s.Recv(); err != nil {
				a.Log.Info("The app's shell disconnected")
				return err
			}
		}
	}
}
