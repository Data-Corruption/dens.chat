// Package instance is the per-instance configuration an elevated
// maintenance command writes and the service reads: ports, the desktop user
// allowed to pair, and the release source. The service can't change it; the
// control directory is read-only to the service account.
package instance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"
)

// Default ports of the main instance. Other instances choose their own.
const (
	DefaultClientPort   = 8484
	DefaultDenPort      = 8485
	DefaultMediaUDPPort = 7881
	DefaultMediaTCPPort = 7882
)

// FlatpakClientPort is the Flatpak's client port (M9.3), apart from an
// installed Dens's, so neither takes the other's port or cookie.
const FlatpakClientPort = 8494

// AndroidDevClientPort is the client port of a development build's Android
// app (M12), which runs beside the release's, on DefaultClientPort, on a
// phone whose apps all share loopback.
const AndroidDevClientPort = 18484

const maxConfigSize = 16 * 1024

// Den holds the den's ports, which it listens on while hosting is on. Its
// listener sits on loopback behind Caddy; the media ports face the
// internet. Enabled is the den role v1.0's installer gave an instance, which
// the first start of a later version takes as the hosting switch's first
// setting (M9.1). Nothing else reads it; it stays because the config decodes
// strictly.
type Den struct {
	Enabled      bool `json:"enabled"`
	Port         int  `json:"port"`
	MediaUDPPort int  `json:"mediaUDPPort"`
	MediaTCPPort int  `json:"mediaTCPPort"`
}

// Config is one instance's configuration.
type Config struct {
	Name       string `json:"name"`
	ClientPort int    `json:"clientPort"`
	Den        Den    `json:"den"`
	// DesktopUser is the one account allowed on the control endpoint.
	DesktopUser host.Identity `json:"desktopUser"`
	// ReleaseURL is where update checks look. Empty disables them.
	ReleaseURL string `json:"releaseURL"`
}

// Default returns the default configuration for instance name.
func Default(name string) Config {
	return Config{
		Name:       name,
		ClientPort: DefaultClientPort,
		Den: Den{
			Port:         DefaultDenPort,
			MediaUDPPort: DefaultMediaUDPPort,
			MediaTCPPort: DefaultMediaTCPPort,
		},
	}
}

// port is one of an instance's ports, with the installer flag that sets it.
type port struct {
	name, flag string
	value      int
	tcp        bool
}

func (c Config) ports() []port {
	return []port{
		{"client port", "--client-port", c.ClientPort, true},
		{"den port", "--den-port", c.Den.Port, true},
		{"media UDP port", "--media-udp-port", c.Den.MediaUDPPort, false},
		{"media TCP port", "--media-tcp-port", c.Den.MediaTCPPort, true},
	}
}

// Validate checks the configuration is complete and self-consistent: every
// port in range, and no two TCP ports the same. Any instance may host, so
// the den's ports count whether it hosts now or not.
func (c Config) Validate() error { return c.validate(true) }

// validate checks c, with the den's ports or without them. A config read
// from disk is checked as v1.0 checked it, the den's ports only while it
// had the den role, so an update can read one whose ports clash and say
// which flag fixes them.
func (c Config) validate(denPorts bool) error {
	if err := layout.ValidateInstance(c.Name); err != nil {
		return err
	}
	if c.DesktopUser.ID == "" {
		return errors.New("desktop user is not set")
	}
	ports := c.ports()
	if !denPorts {
		ports = ports[:1]
	}
	seenTCP := map[int]port{}
	for _, p := range ports {
		if p.value < 1 || p.value > 65535 {
			return fmt.Errorf("the %s, %d, is out of range; choose another with %s", p.name, p.value, p.flag)
		}
		if !p.tcp {
			continue
		}
		if other, ok := seenTCP[p.value]; ok {
			return fmt.Errorf("the %s and the %s are both %d; move one with %s or %s", other.name, p.name, p.value, other.flag, p.flag)
		}
		seenTCP[p.value] = p
	}
	if c.ReleaseURL != "" {
		u, err := url.Parse(c.ReleaseURL)
		if err != nil || !(u.Scheme == "https" || u.Scheme == "file" || LoopbackHTTP(u)) || !strings.HasSuffix(c.ReleaseURL, "/") {
			return fmt.Errorf("release URL %q must be an https (or file, or http on loopback, for testing) URL ending in /", c.ReleaseURL)
		}
	}
	return nil
}

// LoopbackHTTP reports whether u is plain http to this machine's loopback,
// which only a test release's host is: the Android e2e's, which adb
// forwards the emulator's loopback to (M12). Nothing on the way can change
// what loopback carries, and a release's signature covers it anyway.
func LoopbackHTTP(u *url.URL) bool {
	if u.Scheme != "http" {
		return false
	}
	if u.Hostname() == "localhost" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

// Encode returns the file contents for c.
func Encode(c Config) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Read loads and validates the configuration at path, strictly: unknown
// fields and trailing data are errors.
func Read(path string) (Config, error) {
	file, err := xsyscall.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigSize+1))
	if err != nil {
		return Config{}, fmt.Errorf("read instance config: %w", err)
	}
	if len(data) > maxConfigSize {
		return Config{}, fmt.Errorf("instance config exceeds %d bytes", maxConfigSize)
	}
	return Decode(data)
}

// Decode parses and validates configuration file contents.
func Decode(data []byte) (Config, error) {
	var c Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("decode instance config: %w", err)
	}
	if decoder.More() {
		return Config{}, errors.New("instance config has trailing data")
	}
	if err := c.validate(c.Den.Enabled); err != nil {
		return Config{}, fmt.Errorf("instance config: %w", err)
	}
	return c, nil
}
