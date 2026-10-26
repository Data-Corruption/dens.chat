package app

import (
	"fmt"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/types"
)

// LogsLimit is how much of the newest logs a download holds (M11).
const LogsLimit = 32 << 20

// Logs returns the newest logs, up to LogsLimit and oldest first, for the
// member to send with a problem, with the name they download as (M11): an
// installed Dens keeps them in the service's data directory, out of its
// desktop user's reach.
func (a *App) Logs() (name string, logs []byte, err error) {
	logs, err = a.Log.Newest(LogsLimit)
	name = fmt.Sprintf("%s-%s-%s-logs.txt", a.buildInfo.Name, a.Layout.Instance, time.Now().Format("20060102-150405"))
	return name, logs, err
}

// SetLogLevel keeps how much the logs say from now on, and applies it,
// returning it as kept; a development instance logs everything whatever
// it keeps.
func (a *App) SetLogLevel(level string) (string, error) {
	cfg, err := config.Update(a.DB, func(cfg *types.Configuration) error {
		cfg.LogLevel = level
		return nil
	})
	if err != nil {
		return "", err
	}
	if a.Layout.Form != layout.Development {
		if err := a.Log.SetLevel(cfg.LogLevel); err != nil {
			return "", err
		}
	}
	return cfg.LogLevel, nil
}
