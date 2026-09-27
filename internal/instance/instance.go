// Package instance is the per-instance configuration an elevated
// maintenance command writes and the service reads: ports, whether the
// instance hosts a den, the desktop user allowed to pair, and the release
// source. The service can't change it; the control directory is read-only to
// the service account.
package instance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const maxConfigSize = 16 * 1024

// Den is the den role. Its listener sits on loopback behind Caddy; the
// media ports face the internet.
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

// Validate checks the configuration is complete and self-consistent.
func (c Config) Validate() error {
	if err := layout.ValidateInstance(c.Name); err != nil {
		return err
	}
	if c.DesktopUser.ID == "" {
		return errors.New("desktop user is not set")
	}
	ports := map[string]int{"client port": c.ClientPort}
	if c.Den.Enabled {
		ports["den port"] = c.Den.Port
		ports["media UDP port"] = c.Den.MediaUDPPort
		ports["media TCP port"] = c.Den.MediaTCPPort
	}
	seenTCP := map[int]string{}
	for name, port := range ports {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%s %d is out of range", name, port)
		}
		if name == "media UDP port" {
			continue
		}
		if other, ok := seenTCP[port]; ok {
			return fmt.Errorf("%s and %s are both %d", name, other, port)
		}
		seenTCP[port] = name
	}
	if c.ReleaseURL != "" {
		u, err := url.Parse(c.ReleaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "file") || !strings.HasSuffix(c.ReleaseURL, "/") {
			return fmt.Errorf("release URL %q must be an https (or file, for testing) URL ending in /", c.ReleaseURL)
		}
	}
	return nil
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
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("instance config: %w", err)
	}
	return c, nil
}
