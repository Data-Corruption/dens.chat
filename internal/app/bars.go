package app

import (
	"errors"
	"regexp"
	"strings"
	"sync"
)

// The system bars (M13.3). The Android app's shell paints its status and
// navigation bars with the page's background, which the page says as its
// theme changes, so the bars follow a theme the member picks as well as
// the phone's light or dark mode. The service keeps the last color the
// page said, in memory, and tells the shell of it as it connects and as
// it changes.

// BarsShell is the shell, as it hears the bars' color.
type BarsShell interface {
	Bars(color string)
}

type barsState struct {
	mu    sync.Mutex
	color string
	shell BarsShell
}

// ErrBarsColor is a color for the bars that isn't #rrggbb.
var ErrBarsColor = errors.New("the bars' color must be #rrggbb")

var barsColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// SetBars keeps color, #rrggbb in either case, as the page's background,
// and tells the shell, if one is connected, of a change. The shell hears
// colors in the order the page said them, so the lock holds while it's
// told.
func (a *App) SetBars(color string) error {
	color = strings.ToLower(color)
	if !barsColor.MatchString(color) {
		return ErrBarsColor
	}
	a.bars.mu.Lock()
	defer a.bars.mu.Unlock()
	if color == a.bars.color {
		return nil
	}
	a.bars.color = color
	if a.bars.shell != nil {
		a.bars.shell.Bars(color)
	}
	return nil
}

// SetBarsShell is the shell connecting, which hears the bars' color at
// once, where the page has said one.
func (a *App) SetBarsShell(s BarsShell) {
	a.bars.mu.Lock()
	defer a.bars.mu.Unlock()
	a.bars.shell = s
	if a.bars.color != "" {
		s.Bars(a.bars.color)
	}
}

// DropBarsShell is the shell s disconnecting.
func (a *App) DropBarsShell(s BarsShell) {
	a.bars.mu.Lock()
	defer a.bars.mu.Unlock()
	if a.bars.shell == s {
		a.bars.shell = nil
	}
}
