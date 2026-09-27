package config

import (
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// validate runs inside Update on the resulting configuration before it is
// persisted, so every writer gets the same check.
func validate(cfg *types.Configuration) error {
	_, err := xlog.NormalizeLevel(cfg.LogLevel)
	return err
}
