package instance

import (
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

func validConfig() Config {
	c := Default("main")
	c.DesktopUser = host.Identity{ID: "1000", Name: "alice"}
	c.ReleaseURL = "https://releases.dens.chat/"
	return c
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	c := validConfig()
	c.Den.Enabled = true
	data, err := Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got != c {
		t.Fatalf("round trip = %+v, want %+v", got, c)
	}
}

func TestValidateRejectsBadConfigs(t *testing.T) {
	cases := map[string]func(*Config){
		"no desktop user":     func(c *Config) { c.DesktopUser = host.Identity{} },
		"bad name":            func(c *Config) { c.Name = "Main" },
		"port out of range":   func(c *Config) { c.ClientPort = 70000 },
		"den port collides":   func(c *Config) { c.Den.Enabled = true; c.Den.Port = c.ClientPort },
		"plain http release":  func(c *Config) { c.ReleaseURL = "http://releases.dens.chat/" },
		"release without /":   func(c *Config) { c.ReleaseURL = "https://releases.dens.chat" },
		"media tcp collides":  func(c *Config) { c.Den.Enabled = true; c.Den.MediaTCPPort = c.Den.Port },
		"negative media port": func(c *Config) { c.Den.Enabled = true; c.Den.MediaUDPPort = -1 },
	}
	for name, mutate := range cases {
		c := validConfig()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Den ports don't matter while the role is off.
	c := validConfig()
	c.Den.Port = c.ClientPort
	if err := c.Validate(); err != nil {
		t.Errorf("disabled den with colliding port refused: %v", err)
	}
}

func TestDecodeIsStrict(t *testing.T) {
	data, err := Encode(validConfig())
	if err != nil {
		t.Fatal(err)
	}
	extra := strings.Replace(string(data), `"name"`, `"surprise": 1, "name"`, 1)
	if _, err := Decode([]byte(extra)); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := Decode(append(data, []byte(`{}`)...)); err == nil {
		t.Fatal("trailing data accepted")
	}
}
