// Package maintenance is the lifecycle protocol between the service and the
// elevated maintenance commands (install, update, restore, uninstall).
//
// Only maintenance commands write state.json. The service reads it at start
// and runs only when it is ready for the service's own version, or migrates
// first when a transition names its version as the target. Maintenance
// commands serialize on the operation lock; the service holds the lifecycle
// lock exclusively for its whole run, so a command that takes it knows the
// service is down.
package maintenance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"

	"golang.org/x/mod/semver"
)

const maxStateSize = 16 * 1024

// Phase is the durable lifecycle phase of an installation.
type Phase string

const (
	PhaseInstalling   Phase = "installing"
	PhaseUpdating     Phase = "updating"
	PhaseRestoring    Phase = "restoring"
	PhaseReady        Phase = "ready"
	PhaseUninstalling Phase = "uninstalling"
)

// State is the durable lifecycle state. Version is the installed version
// (empty during a first install); TargetVersion is set during transitions
// that end with the service starting on a new or restored installation.
type State struct {
	Phase         Phase  `json:"phase"`
	Version       string `json:"version"`
	TargetVersion string `json:"targetVersion"`
	ChangedAt     string `json:"changedAt"`
}

// Transitional reports whether the phase is one the service may migrate in.
func (p Phase) Transitional() bool {
	return p == PhaseInstalling || p == PhaseUpdating || p == PhaseRestoring
}

// Validate enforces the state machine's representation.
func (s State) Validate() error {
	if _, err := time.Parse(time.RFC3339Nano, s.ChangedAt); err != nil {
		return fmt.Errorf("changedAt %q is not RFC 3339: %w", s.ChangedAt, err)
	}
	for name, v := range map[string]string{"version": s.Version, "targetVersion": s.TargetVersion} {
		if v != "" && !semver.IsValid(v) {
			return fmt.Errorf("%s %q is not a semantic version", name, v)
		}
	}
	switch s.Phase {
	case PhaseInstalling:
		if s.TargetVersion == "" {
			return errors.New("installing requires targetVersion")
		}
	case PhaseUpdating, PhaseRestoring:
		if s.Version == "" || s.TargetVersion == "" {
			return fmt.Errorf("%s requires version and targetVersion", s.Phase)
		}
		if s.Phase == PhaseRestoring && s.Version != s.TargetVersion {
			return errors.New("restoring must not change the version")
		}
	case PhaseReady, PhaseUninstalling:
		if s.Version == "" {
			return fmt.Errorf("%s requires version", s.Phase)
		}
		if s.TargetVersion != "" {
			return fmt.Errorf("%s must not have targetVersion", s.Phase)
		}
	default:
		return fmt.Errorf("unknown phase %q", s.Phase)
	}
	return nil
}

// NewState returns a state with the current time.
func NewState(phase Phase, version, target string) State {
	return State{Phase: phase, Version: version, TargetVersion: target, ChangedAt: time.Now().UTC().Format(time.RFC3339Nano)}
}

// ReadState reads and strictly validates the state file at path without
// following a symlink. A missing file is reported as os.ErrNotExist.
func ReadState(path string) (State, error) {
	file, err := xsyscall.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return State{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxStateSize+1))
	if err != nil {
		return State{}, fmt.Errorf("read lifecycle state: %w", err)
	}
	if len(data) > maxStateSize {
		return State{}, fmt.Errorf("lifecycle state exceeds %d bytes", maxStateSize)
	}
	var s State
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&s); err != nil {
		return State{}, fmt.Errorf("decode lifecycle state: %w", err)
	}
	if decoder.More() {
		return State{}, errors.New("lifecycle state has trailing data")
	}
	if err := s.Validate(); err != nil {
		return State{}, fmt.Errorf("lifecycle state: %w", err)
	}
	return s, nil
}

// WriteState atomically replaces the state file at path. prepare sets the
// temporary file's ownership and permissions before it is published; the
// state is never visible with the wrong ones.
func WriteState(path string, s State, prepare func(tempPath string) error) error {
	data, err := EncodeState(s)
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, data, prepare)
}

// EncodeState validates s and returns its file contents.
func EncodeState(s State) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// WriteFileAtomic writes data to a temporary file beside path, syncs it,
// lets prepare adjust it, and renames it over path.
func WriteFileAtomic(path string, data []byte, prepare func(tempPath string) error) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("stage %s: %w", filepath.Base(path), err)
	}
	tempPath := temp.Name()
	published := false
	defer func() {
		_ = temp.Close()
		if !published {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", filepath.Base(path), err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(path), err)
	}
	if prepare != nil {
		if err := prepare(tempPath); err != nil {
			return fmt.Errorf("prepare %s: %w", filepath.Base(path), err)
		}
	}
	if err := replaceFile(tempPath, path); err != nil {
		return fmt.Errorf("publish %s: %w", filepath.Base(path), err)
	}
	published = true
	return syncDir(filepath.Dir(path))
}
