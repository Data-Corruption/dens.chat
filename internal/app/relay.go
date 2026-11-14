package app

import (
	"sync"

	"github.com/Data-Corruption/dens.chat/internal/relay"
)

// What Relay wants of Android goes to the shell, which asks the member
// for the local network and plays a listening as media. The service keeps
// the last word in memory, and tells the shell as it connects and as the
// word changes.

// RelayShell is the shell, as it hears what Relay wants of Android.
type RelayShell interface {
	Relay(w relay.Wants)
}

type relayShellState struct {
	mu    sync.Mutex
	wants relay.Wants
	shell RelayShell
}

// relayWants keeps what Relay wants and tells the shell, if one is
// connected, under the lock, so the shell hears each in Relay's order.
func (a *App) relayWants(w relay.Wants) {
	a.relayShell.mu.Lock()
	defer a.relayShell.mu.Unlock()
	a.relayShell.wants = w
	if a.relayShell.shell != nil {
		a.relayShell.shell.Relay(w)
	}
}

// SetRelayShell is the shell connecting. It hears what Relay wants only
// where Relay wants something: a shell lets go of everything as its
// stream closes.
func (a *App) SetRelayShell(s RelayShell) {
	a.relayShell.mu.Lock()
	defer a.relayShell.mu.Unlock()
	a.relayShell.shell = s
	if a.relayShell.wants != (relay.Wants{}) {
		s.Relay(a.relayShell.wants)
	}
}

// DropRelayShell is the shell s disconnecting.
func (a *App) DropRelayShell(s RelayShell) {
	a.relayShell.mu.Lock()
	defer a.relayShell.mu.Unlock()
	if a.relayShell.shell == s {
		a.relayShell.shell = nil
	}
}
