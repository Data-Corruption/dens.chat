package layout

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCheckControlRefusesLoosenedOrSwappedFiles(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	l, err := New("dens", "main", Development)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.EnsureSession(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.State, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckControl(l.State); err != nil {
		t.Fatalf("private state refused: %v", err)
	}

	if err := os.Chmod(l.State, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckControl(l.State); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("world-readable state accepted: %v", err)
	}

	if err := os.Remove(l.State); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, l.State); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckControl(l.State); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked state accepted: %v", err)
	}
}

// setFlatpakHome gives a Flatpak layout the directories Flatpak gives the
// app's sandbox, below a temporary one.
func setFlatpakHome(t *testing.T) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), ".var", "app", DesktopID)
	t.Setenv("XDG_DATA_HOME", filepath.Join(app, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(app, "cache"))
	runtime := filepath.Join(t.TempDir(), "run")
	// Flatpak makes the app's runtime directory, which every sandbox of
	// the app shares.
	if err := os.MkdirAll(filepath.Join(runtime, "app", DesktopID), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	return app
}

// The Flatpak keeps its data in the app's data directory, its cache where
// backup tools skip it, and its socket where every sandbox of the app
// finds it (M9.3).
func TestFlatpakLayoutUsesTheAppsDirectories(t *testing.T) {
	app := setFlatpakHome(t)
	l, err := New("dens", "main", Flatpak)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		l.Root:            filepath.Join(app, "data", "dens", "main"),
		l.Cache:           filepath.Join(app, "cache", "dens", "main"),
		l.ControlEndpoint: filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "app", DesktopID, "main", "control.sock"),
		l.HostKey:         filepath.Join(l.Control, "datakey.file"),
		l.KeyringKey:      filepath.Join(l.Control, "datakey.keyring"),
		l.Snapshot:        filepath.Join(app, "data", "dens", "main", "snapshot"),
	} {
		if path != want {
			t.Errorf("path %s, want %s", path, want)
		}
	}
	if l.Binary != "" || l.ServiceName != "" || l.Account != "" || l.DesktopFile != "" {
		t.Fatalf("the Flatpak's layout has system integration: %+v", l)
	}
	if err := l.EnsureSession(); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if err := l.CheckControl(); err != nil {
		t.Fatalf("CheckControl: %v", err)
	}
	if err := l.CheckData(); err != nil {
		t.Fatalf("CheckData: %v", err)
	}
	if err := l.EnsureData(); err != nil {
		t.Fatalf("EnsureData: %v", err)
	}
	if info, err := os.Stat(l.Cache); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("cache directory: %v, %v", info, err)
	}

	t.Setenv("XDG_CACHE_HOME", "cache")
	if _, err := New("dens", "main", Flatpak); err == nil || !strings.Contains(err.Error(), "XDG_CACHE_HOME") {
		t.Fatalf("a relative cache directory = %v, want refused", err)
	}
}

// The Android app keeps everything in the files directory its shell names,
// the cache and the control socket too, beside the key its shell wraps
// (M12).
func TestAndroidLayoutUsesTheAppsFiles(t *testing.T) {
	files := filepath.Join(t.TempDir(), "data", "user", "0", "chat.dens.Dens", "files")
	t.Setenv("XDG_DATA_HOME", files)
	l, err := New("dens", "main", Android)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(files, "dens", "main")
	for path, want := range map[string]string{
		l.Root:            root,
		l.Cache:           filepath.Join(root, "data", "cache"),
		l.ControlEndpoint: filepath.Join(root, "run", "control.sock"),
		l.HostKey:         filepath.Join(root, "control", "datakey.keystore"),
	} {
		if path != want {
			t.Errorf("path %s, want %s", path, want)
		}
	}
	if l.Binary != "" || l.ServiceName != "" || l.Account != "" || l.KeyringKey != "" || l.Snapshot != "" {
		t.Fatalf("the Android app's layout has what another form's has: %+v", l)
	}
	if err := l.EnsureSession(); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if err := os.WriteFile(l.HostKey, []byte("wrapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckControl(l.HostKey); err != nil {
		t.Fatalf("CheckControl: %v", err)
	}
	if err := l.CheckData(); err != nil {
		t.Fatalf("CheckData: %v", err)
	}
	if err := l.EnsureData(); err != nil {
		t.Fatalf("EnsureData: %v", err)
	}
	if !slices.Contains(l.ServiceControlFiles(), l.HostKey) {
		t.Fatal("the service doesn't check the shell's wrapped key")
	}
	if err := os.Chmod(l.HostKey, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckControl(l.HostKey); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("a wrapped key others may read = %v, want refused", err)
	}

	t.Setenv("XDG_DATA_HOME", "files")
	if _, err := New("dens", "main", Android); err == nil || !strings.Contains(err.Error(), "XDG_DATA_HOME") {
		t.Fatalf("a relative files directory = %v, want refused", err)
	}
}
