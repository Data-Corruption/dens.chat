package config

import (
	"fmt"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// validate runs inside Update on the resulting configuration before it is
// persisted, so every writer gets the same check.
func validate(cfg *types.Configuration) error {
	if _, err := xlog.NormalizeLevel(cfg.LogLevel); err != nil {
		return err
	}
	return validateVoiceKeys(cfg.VoiceKeys)
}

// validateVoiceKeys checks keys as the page sends them: known actions, a
// code of letters and digits, as KeyboardEvent.code's are, and a short
// printable label.
func validateVoiceKeys(keys map[string]types.VoiceKey) error {
	for action, k := range keys {
		if !slices.Contains(types.VoiceActions, action) {
			return fmt.Errorf("no voice action %q", action)
		}
		if k.Code == "" || len(k.Code) > 32 {
			return fmt.Errorf("the key for %s has no code", action)
		}
		for _, c := range k.Code {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
				return fmt.Errorf("the key for %s has an invalid code", action)
			}
		}
		if k.Label == "" || utf8.RuneCountInString(k.Label) > 32 || !utf8.ValidString(k.Label) {
			return fmt.Errorf("the key for %s needs a label of 1 to 32 characters", action)
		}
		for _, c := range k.Label {
			if !unicode.IsPrint(c) {
				return fmt.Errorf("the key for %s has an unprintable label", action)
			}
		}
	}
	return nil
}
