package install

import (
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

// The Flatpak restores as the member, over data whatever version left
// them, as after a rollback, once its service has stopped. The restored
// instance forgets the key it remembered and the cache it kept, and its
// next start resumes the restore (M9.3).
func TestFlatpakRestoresAsTheMember(t *testing.T) {
	app := filepath.Join(t.TempDir(), "app")
	t.Setenv("XDG_DATA_HOME", filepath.Join(app, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(app, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(app, "run"))
	l, err := layout.New("dens", "main", layout.Flatpak)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.EnsureSession(); err != nil {
		t.Fatal(err)
	}
	newer := maintenance.NewState(maintenance.PhaseReady, "v1.2.0", "")
	if err := maintenance.WriteState(l.State, newer, nil); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		filepath.Join(l.Data, "marker"):  "before",
		l.HostKey:                        "an old key",
		l.KeyringKey:                     "an old sealed key",
		filepath.Join(l.Cache, "cached"): "a den's file",
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	key := make([]byte, host.HostKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	archive := makeBackup(t, key, "correct horse battery")
	bi := buildInfo("v1.1.0")
	opts := Options{Instance: "main", Out: io.Discard}
	stops := 0
	stop := func() error { stops++; return nil }

	if err := RestoreSession(context.Background(), bi, l, archive, "wrong wrong wrong", stop, opts); err == nil {
		t.Fatal("restore accepted the wrong password")
	}
	if state, err := maintenance.ReadState(l.State); err != nil || state != newer || stops != 0 {
		t.Fatalf("a refused restore left %+v, %v, and stopped the service %d times", state, err, stops)
	}
	if err := RestoreSession(context.Background(), bi, l, archive, "correct horse battery", stop, opts); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if stops != 1 {
		t.Fatalf("the restore stopped the service %d times", stops)
	}
	if _, err := os.Stat(filepath.Join(l.DB, database.FileName)); err != nil {
		t.Fatalf("restored database missing: %v", err)
	}
	for _, path := range []string{filepath.Join(l.Data, "marker"), l.HostKey, l.KeyringKey, filepath.Join(l.Cache, "cached")} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s is still there after the restore", path)
		}
	}
	replaced, _ := filepath.Glob(l.Data + ".replaced-*")
	if len(replaced) != 1 {
		t.Fatalf("the replaced data: %v", replaced)
	}
	if data, err := os.ReadFile(filepath.Join(replaced[0], "marker")); err != nil || string(data) != "before" {
		t.Fatalf("the replaced data's marker: %q, %v", data, err)
	}

	// Its next start resumes the restore, as one at this version.
	lease, err := maintenance.AcquireSessionLease(l, bi.Version, maintenance.SessionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if lease.Mode != maintenance.StartMigrate || lease.State.Phase != maintenance.PhaseRestoring || lease.State.TargetVersion != bi.Version {
		t.Fatalf("the restored data's start: %+v", lease)
	}
}
