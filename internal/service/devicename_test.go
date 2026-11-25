package service_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
)

// The member names this install in the settings, which keep the name
// and make the label it goes by on dens and in Relay from it, what the
// computer is still in parentheses; no name gives it the computer's.
func TestTheSettingsNameTheDevice(t *testing.T) {
	a, client := startDevService(t)
	b := pair(t, a, client)
	var settings struct {
		DeviceName  string `json:"deviceName"`
		DeviceLabel string `json:"deviceLabel"`
	}
	b.expect(http.StatusOK, "the settings", "GET", "/api/settings", nil, &settings)
	if settings.DeviceName != "" || settings.DeviceLabel != denclient.ComputerLabel("") {
		t.Fatalf("before a name the settings say %+v", settings)
	}
	for _, bad := range []string{"​", strings.Repeat("a", 33)} {
		if code, body := b.do("POST", "/api/settings", map[string]string{"deviceName": bad}); code != http.StatusBadRequest {
			t.Errorf("the name %q: %d %s", bad, code, body)
		}
	}
	b.expect(http.StatusOK, "name the device", "POST", "/api/settings", map[string]string{"deviceName": "  Bob's‮ desk "})
	b.expect(http.StatusOK, "the settings", "GET", "/api/settings", nil, &settings)
	want := denclient.ComputerLabel("Bob's desk")
	if settings.DeviceName != "Bob's desk" || settings.DeviceLabel != want || a.Dens.Label() != want || !strings.HasPrefix(want, "Bob's desk (") {
		t.Fatalf("named, the settings say %+v and the dens get %q", settings, a.Dens.Label())
	}
	if cfg, err := config.View(a.DB); err != nil || cfg.DeviceName != "Bob's desk" {
		t.Fatalf("the settings keep %q, %v", cfg.DeviceName, err)
	}
	b.expect(http.StatusOK, "clear the name", "POST", "/api/settings", map[string]string{"deviceName": ""})
	if a.Dens.Label() != denclient.ComputerLabel("") {
		t.Fatalf("with no name the dens get %q", a.Dens.Label())
	}
}
