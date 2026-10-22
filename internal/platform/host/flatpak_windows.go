package host

import (
	"context"
	"errors"
	"os"
	"time"
)

// The Flatpak's portals (M9.3) are Linux's; on Windows each fails with
// errors.ErrUnsupported.

var (
	ErrNothingToUpdate = errors.New("nothing to update")
	ErrUpdateRefused   = errors.New("Flatpak won't install the update from Dens")
)

func RetrieveSecret(context.Context, time.Duration) ([]byte, error) {
	return nil, errors.ErrUnsupported
}

func RequestBackground(context.Context, string, bool, []string) (bool, bool, error) {
	return false, false, errors.ErrUnsupported
}

func SetBackgroundStatus(string) error { return errors.ErrUnsupported }

func Notify(string, string, string) error { return errors.ErrUnsupported }

func SpawnLatest([]string, ...*os.File) error { return errors.ErrUnsupported }

type FlatpakUpdate struct {
	Running, Local, Remote string
}

type UpdateMonitor struct{}

func OpenUpdateMonitor() (*UpdateMonitor, error)     { return nil, errors.ErrUnsupported }
func (*UpdateMonitor) Updates() <-chan FlatpakUpdate { return nil }
func (*UpdateMonitor) Update(context.Context) error  { return errors.ErrUnsupported }
func (*UpdateMonitor) Close() error                  { return nil }

type AppName struct{}

func OwnAppName(string, func()) (*AppName, error) { return nil, errors.ErrUnsupported }
func (*AppName) Close() error                     { return nil }
