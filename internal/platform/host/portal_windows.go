package host

import (
	"context"
	"errors"
	"fmt"
)

// DesktopKeys is a desktop that owns the keys, as Wayland's global
// shortcuts portal does (M7.2). Windows has none: the helper reads the keys
// itself (KeyState).
type DesktopKeys struct{}

// OpenDesktopKeys fails with errors.ErrUnsupported on Windows.
func OpenDesktopKeys(string) (*DesktopKeys, error) {
	return nil, fmt.Errorf("Windows has no desktop shortcuts portal: %w", errors.ErrUnsupported)
}

func (*DesktopKeys) Bind(context.Context, []DesktopAction) (map[string]string, error) {
	return nil, errors.ErrUnsupported
}
func (*DesktopKeys) Presses() <-chan DesktopPress      { return nil }
func (*DesktopKeys) Changes() <-chan map[string]string { return nil }
func (*DesktopKeys) Closed() <-chan struct{}           { return nil }
func (*DesktopKeys) CanConfigure() bool                { return false }
func (*DesktopKeys) Configure() error                  { return errors.ErrUnsupported }
func (*DesktopKeys) Note() string                      { return "" }
func (*DesktopKeys) Close() error                      { return nil }
