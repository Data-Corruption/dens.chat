package service_test

import (
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/control"
)

// Only the Android app's shell opens its stream.
func TestOnlyTheAndroidAppKeepsTheShellsStream(t *testing.T) {
	_, client := startDevService(t)
	if _, err := client.Open(control.Request{Op: control.OpShell}); err == nil || !strings.Contains(err.Error(), "Android") {
		t.Fatalf("a development instance's shell stream = %v, want refused", err)
	}
}
