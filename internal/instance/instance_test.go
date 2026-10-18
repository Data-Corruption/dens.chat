package instance

import (
	"os"
	"path/filepath"
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
		"den port collides":   func(c *Config) { c.Den.Port = c.ClientPort },
		"plain http release":  func(c *Config) { c.ReleaseURL = "http://releases.dens.chat/" },
		"release without /":   func(c *Config) { c.ReleaseURL = "https://releases.dens.chat" },
		"media tcp collides":  func(c *Config) { c.Den.MediaTCPPort = c.Den.Port },
		"negative media port": func(c *Config) { c.Den.MediaUDPPort = -1 },
	}
	for name, mutate := range cases {
		c := validConfig()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The UDP port may share its number with a TCP one.
	c := validConfig()
	c.Den.MediaUDPPort = c.Den.MediaTCPPort
	if err := c.Validate(); err != nil {
		t.Errorf("a UDP port numbered as a TCP one refused: %v", err)
	}
}

// v1.0.0's installer wrote these, from its own code. Every later version
// reads them.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "v1.0.0", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDecodesWhatV1Wrote(t *testing.T) {
	c, err := Decode(readFixture(t, "hosting.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := validConfig()
	want.Den.Enabled = true
	if c != want {
		t.Fatalf("decoded %+v, want %+v", c, want)
	}
}

// v1.0 checked a den's ports only for an instance with the den role, so it
// could write a config whose client port is its den's. That reads, and the
// check the installer makes before writing it again names the flags.
func TestReadsV1sClashAndNamesTheFlags(t *testing.T) {
	c, err := Decode(readFixture(t, "clashing.json"))
	if err != nil {
		t.Fatalf("v1.0's config refused: %v", err)
	}
	err = c.Validate()
	if err == nil {
		t.Fatal("clashing ports accepted")
	}
	for _, want := range []string{"8485", "--client-port", "--den-port"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q doesn't say %s", err, want)
		}
	}
	if _, err := Encode(c); err == nil {
		t.Fatal("clashing ports written")
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
