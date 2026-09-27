package install

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/vaultstore"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"
)

// fakeSystem records a transaction's platform calls against a temporary
// root. Its StartService behaves like the real service's start check: it
// needs the lifecycle lock free and state that authorizes its version.
type fakeSystem struct {
	base       string
	version    string // the version the installed binary would report
	accounts   map[string]bool
	registered map[string]bool
	running    map[string]bool
	firewall   map[string]bool
	sharedGone bool
	failOn     string
	startErr   error
}

func newFake(t *testing.T) *fakeSystem {
	return &fakeSystem{
		base:       t.TempDir(),
		accounts:   map[string]bool{},
		registered: map[string]bool{},
		running:    map[string]bool{},
		firewall:   map[string]bool{},
	}
}

func (f *fakeSystem) fail(step string) error {
	if f.failOn == step {
		return fmt.Errorf("injected failure in %s", step)
	}
	return nil
}

func (f *fakeSystem) Layout(name string) (layout.Layout, error) {
	l, err := layout.New("dens", name, false)
	if err != nil {
		return l, err
	}
	return layout.WithRoot(l, f.base), nil
}
func (f *fakeSystem) CheckAdmin() error { return nil }
func (f *fakeSystem) Preflight() error  { return nil }
func (f *fakeSystem) DesktopUser(override string) (host.Identity, error) {
	if override != "" {
		return host.Identity{ID: "1001", Name: override}, nil
	}
	return host.Identity{ID: "1000", Name: "alice"}, nil
}
func (f *fakeSystem) PrepareRoot(l layout.Layout) error { return os.MkdirAll(l.Control, 0o700) }
func (f *fakeSystem) EnsureAccount(l layout.Layout) (bool, error) {
	if f.accounts[l.Instance] {
		return false, nil
	}
	f.accounts[l.Instance] = true
	return true, nil
}
func (f *fakeSystem) RemoveAccount(l layout.Layout) error {
	delete(f.accounts, l.Instance)
	return nil
}
func (f *fakeSystem) InstallBinary(l layout.Layout, src string) (func() error, error) {
	previous, err := os.ReadFile(filepath.Join(f.base, "binary"))
	if err := os.WriteFile(filepath.Join(f.base, "binary"), []byte(f.version), 0o600); err != nil {
		return nil, err
	}
	return func() error {
		if err != nil {
			return os.Remove(filepath.Join(f.base, "binary"))
		}
		return os.WriteFile(filepath.Join(f.base, "binary"), previous, 0o600)
	}, nil
}
func (f *fakeSystem) InstallCosign(layout.Layout, string) error { return nil }
func (f *fakeSystem) RegisterService(l layout.Layout) (func() error, error) {
	if err := f.fail("RegisterService"); err != nil {
		return nil, err
	}
	was := f.registered[l.Instance]
	f.registered[l.Instance] = true
	return func() error {
		if !was {
			delete(f.registered, l.Instance)
		}
		return nil
	}, nil
}
func (f *fakeSystem) ServiceAccount(l layout.Layout) (host.Identity, error) {
	if !f.registered[l.Instance] {
		return host.Identity{}, errors.New("not registered")
	}
	return host.Identity{ID: "svc-" + l.Instance, Name: l.Account}, nil
}
func (f *fakeSystem) Secure(l layout.Layout, _ host.Identity) error {
	return os.MkdirAll(l.Data, 0o700)
}
func (f *fakeSystem) WriteControlFile(_ layout.Layout, path string, data []byte, _ host.Identity) error {
	return maintenance.WriteFileAtomic(path, data, nil)
}
func (f *fakeSystem) WrapHostKey(l layout.Layout, key []byte) error {
	return os.WriteFile(l.HostKey, key, 0o600)
}
func (f *fakeSystem) ConfigureFirewall(l layout.Layout, cfg instance.Config) error {
	f.firewall[l.Instance] = cfg.Den.Enabled
	return nil
}
func (f *fakeSystem) RegisterExtras(layout.Layout) error { return f.fail("RegisterExtras") }
func (f *fakeSystem) ServiceRunning(l layout.Layout) (bool, error) {
	return f.running[l.Instance], nil
}
func (f *fakeSystem) StartService(l layout.Layout) error {
	if !f.registered[l.Instance] {
		return errors.New("service not registered")
	}
	if f.startErr != nil {
		return f.startErr
	}
	lock, err := xsyscall.AcquireLock(context.Background(), l.LifecycleLock, xsyscall.LockOptions{Mode: xsyscall.ModeExclusive})
	if err != nil {
		return fmt.Errorf("lifecycle lock: %w", err)
	}
	defer lock.Close()
	state, err := maintenance.ReadState(l.State)
	if err != nil {
		return err
	}
	binary, _ := os.ReadFile(filepath.Join(f.base, "binary"))
	if _, err := maintenance.AuthorizeStart(state, string(binary)); err != nil {
		return err
	}
	f.running[l.Instance] = true
	return nil
}
func (f *fakeSystem) StopService(l layout.Layout) error {
	f.running[l.Instance] = false
	return nil
}
func (f *fakeSystem) UnregisterService(l layout.Layout) error {
	delete(f.registered, l.Instance)
	delete(f.firewall, l.Instance)
	return nil
}
func (f *fakeSystem) RemoveShared(layout.Layout) error {
	f.sharedGone = true
	return nil
}
func (f *fakeSystem) PrepareRestore(l layout.Layout, _ host.Identity) (string, error) {
	dir := l.Data + ".restoring"
	return dir, os.Mkdir(dir, 0o700)
}
func (f *fakeSystem) AdoptRestored(layout.Layout, string, host.Identity) error { return nil }
func (f *fakeSystem) InstallerName() string                                    { return "install.sh" }
func (f *fakeSystem) RunInstaller(context.Context, string, string, []string) error {
	return errors.New("not in tests")
}

func buildInfo(version string) build.BuildInfo {
	return build.BuildInfo{Name: "dens", Version: version, ReleaseURL: "https://releases.dens.chat/", DefaultLogLevel: "warn"}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func install(t *testing.T, f *fakeSystem, version, name string, opts Options) error {
	t.Helper()
	f.version = version
	opts.Instance = name
	opts.Binary = os.Args[0]
	if opts.ClientPort == 0 {
		if l, err := f.Layout(name); err == nil {
			if _, err := os.Stat(l.InstanceConfig); err != nil {
				opts.ClientPort = freePort(t)
			}
		}
	}
	opts.Out = io.Discard
	return Install(context.Background(), f, buildInfo(version), opts)
}

func stateOf(t *testing.T, f *fakeSystem, name string) maintenance.State {
	t.Helper()
	l, _ := f.Layout(name)
	state, err := maintenance.ReadState(l.State)
	if err != nil {
		t.Fatalf("state of %s: %v", name, err)
	}
	return state
}

func TestFreshInstall(t *testing.T) {
	f := newFake(t)
	if err := install(t, f, "v1.0.0", "main", Options{}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if s := stateOf(t, f, "main"); s.Phase != maintenance.PhaseReady || s.Version != "v1.0.0" {
		t.Fatalf("state = %+v", s)
	}
	if !f.accounts["main"] || !f.registered["main"] || !f.running["main"] {
		t.Fatalf("account %v registered %v running %v", f.accounts, f.registered, f.running)
	}
	l, _ := f.Layout("main")
	key, err := os.ReadFile(l.HostKey)
	if err != nil || len(key) != host.HostKeySize {
		t.Fatalf("data key = %d bytes, %v", len(key), err)
	}
	cfg, err := instance.Read(l.InstanceConfig)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DesktopUser.Name != "alice" || cfg.ReleaseURL != "https://releases.dens.chat/" || cfg.Name != "main" {
		t.Fatalf("instance config = %+v", cfg)
	}
}

func TestUpdateMovesEveryInstance(t *testing.T) {
	f := newFake(t)
	if err := install(t, f, "v1.0.0", "main", Options{}); err != nil {
		t.Fatal(err)
	}
	if err := install(t, f, "v1.0.0", "second", Options{}); err != nil {
		t.Fatal(err)
	}
	f.running["second"] = false // the administrator stopped it

	if err := install(t, f, "v1.1.0", "main", Options{}); err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, name := range []string{"main", "second"} {
		if s := stateOf(t, f, name); s.Phase != maintenance.PhaseReady || s.Version != "v1.1.0" {
			t.Fatalf("%s state = %+v", name, s)
		}
	}
	if !f.running["main"] || f.running["second"] {
		t.Fatalf("running = %v; the stopped instance must stay stopped", f.running)
	}
}

func TestFailureBeforeStartRollsBack(t *testing.T) {
	f := newFake(t)
	if err := install(t, f, "v1.0.0", "main", Options{}); err != nil {
		t.Fatal(err)
	}
	f.failOn = "RegisterExtras"
	if err := install(t, f, "v1.1.0", "main", Options{}); err == nil {
		t.Fatal("update succeeded despite a failure")
	}
	if s := stateOf(t, f, "main"); s.Phase != maintenance.PhaseReady || s.Version != "v1.0.0" {
		t.Fatalf("state after rollback = %+v", s)
	}
	if binary, _ := os.ReadFile(filepath.Join(f.base, "binary")); string(binary) != "v1.0.0" {
		t.Fatalf("binary after rollback = %q", binary)
	}
	if !f.running["main"] {
		t.Fatal("the service that was running wasn't restarted")
	}
}

func TestFailedFreshInstallLeavesNothing(t *testing.T) {
	f := newFake(t)
	f.failOn = "RegisterExtras"
	if err := install(t, f, "v1.0.0", "main", Options{}); err == nil {
		t.Fatal("install succeeded despite a failure")
	}
	l, _ := f.Layout("main")
	for _, path := range []string{l.State, l.HostKey, l.InstanceConfig} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s survived the rollback", path)
		}
	}
	if f.accounts["main"] || f.registered["main"] {
		t.Fatal("account or service survived the rollback")
	}
}

func TestStartFailureKeepsTransitionForRecovery(t *testing.T) {
	f := newFake(t)
	if err := install(t, f, "v1.0.0", "main", Options{}); err != nil {
		t.Fatal(err)
	}
	f.startErr = errors.New("migration failed")
	if err := install(t, f, "v1.1.0", "main", Options{}); err == nil {
		t.Fatal("update succeeded though the service didn't start")
	}
	if s := stateOf(t, f, "main"); s.Phase != maintenance.PhaseUpdating || s.TargetVersion != "v1.1.0" {
		t.Fatalf("state after a failed start = %+v", s)
	}
	if binary, _ := os.ReadFile(filepath.Join(f.base, "binary")); string(binary) != "v1.1.0" {
		t.Fatal("a failure after the point of no return was rolled back")
	}

	f.startErr = nil
	if err := install(t, f, "v1.1.0", "main", Options{}); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if s := stateOf(t, f, "main"); s.Phase != maintenance.PhaseReady || s.Version != "v1.1.0" {
		t.Fatalf("state after recovery = %+v", s)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	f := newFake(t)
	if err := install(t, f, "v1.0.0", "main", Options{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	l, _ := f.Layout("main")
	if _, err := os.Stat(l.Root); err == nil {
		t.Fatal("dry run created the installation root")
	}
}

func TestInstallRefusesPortInUse(t *testing.T) {
	f := newFake(t)
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	err = install(t, f, "v1.0.0", "main", Options{ClientPort: busy.Addr().(*net.TCPAddr).Port})
	if err == nil {
		t.Fatal("install took a port another program holds")
	}
	l, _ := f.Layout("main")
	if _, err := os.Stat(l.Root); err == nil {
		t.Fatal("a refused install changed the system")
	}
}

func TestDenRoleOpensFirewall(t *testing.T) {
	f := newFake(t)
	on := true
	if err := install(t, f, "v1.0.0", "main", Options{Den: &on, DenPort: freePort(t), MediaUDPPort: freePort(t), MediaTCPPort: freePort(t)}); err != nil {
		t.Fatal(err)
	}
	if !f.firewall["main"] {
		t.Fatal("den role didn't open the media ports")
	}
	off := false
	if err := install(t, f, "v1.0.0", "main", Options{Den: &off}); err != nil {
		t.Fatal(err)
	}
	if f.firewall["main"] {
		t.Fatal("turning the den role off left the media ports open")
	}
}

func TestUninstall(t *testing.T) {
	f := newFake(t)
	if err := install(t, f, "v1.0.0", "main", Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(context.Background(), f, buildInfo("v1.0.0"), Options{Instance: "main", Out: io.Discard}); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	l, _ := f.Layout("main")
	if _, err := os.Stat(l.Root); err == nil {
		t.Fatal("installation root survived")
	}
	if f.accounts["main"] || f.registered["main"] || !f.sharedGone {
		t.Fatalf("account %v registered %v shared removed %v", f.accounts, f.registered, f.sharedGone)
	}
	if err := Uninstall(context.Background(), f, buildInfo("v1.0.0"), Options{Instance: "main", Out: io.Discard}); err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
}

func TestUninstallKeepsSharedFilesForOtherInstances(t *testing.T) {
	f := newFake(t)
	for _, name := range []string{"main", "second"} {
		if err := install(t, f, "v1.0.0", name, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := Uninstall(context.Background(), f, buildInfo("v1.0.0"), Options{Instance: "second", Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if f.sharedGone {
		t.Fatal("shared files removed while another instance remains")
	}
	if s := stateOf(t, f, "main"); s.Phase != maintenance.PhaseReady {
		t.Fatalf("the remaining instance's state = %+v", s)
	}
}

// makeBackup builds a real archive: a database with a vault whose key is
// sealed under password.
func makeBackup(t *testing.T, key []byte, password string) string {
	t.Helper()
	dir := t.TempDir()
	logger, err := xlog.New(filepath.Join(dir, "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	bi := buildInfo("v1.0.0")
	db, err := database.New(filepath.Join(dir, "db"), logger, bi, database.ApplyPendingMigrations)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	v, err := vault.New(key)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ctx := context.Background()
	if err := vaultstore.Init(ctx, db, v.CheckValue()); err != nil {
		t.Fatal(err)
	}
	wrap, err := v.WrapWithPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := vault.MarshalWrap(wrap)
	if err := vaultstore.SetPasswordWrap(ctx, db, encoded); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(dir, "snapshot.db")
	if err := database.Snapshot(ctx, db, snapshot); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "test.backup")
	out, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	manifest := backup.Manifest{App: "dens", Version: "v1.0.0", Instance: "main", CreatedAt: time.Now(), Platform: "linux/amd64"}
	if err := backup.Write(out, manifest, snapshot, filepath.Join(dir, "no-uploads")); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestRestore(t *testing.T) {
	f := newFake(t)
	if err := install(t, f, "v1.0.0", "main", Options{}); err != nil {
		t.Fatal(err)
	}
	l, _ := f.Layout("main")
	if err := os.WriteFile(filepath.Join(l.Data, "marker"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, host.HostKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	archive := makeBackup(t, key, "correct horse battery")
	bi := buildInfo("v1.0.0")
	opts := Options{Instance: "main", Out: io.Discard}

	if err := Restore(context.Background(), f, bi, opts, archive, "wrong wrong wrong"); err == nil {
		t.Fatal("restore accepted the wrong password")
	}
	if _, err := os.Stat(filepath.Join(l.Data, "marker")); err != nil {
		t.Fatal("a refused restore changed the data")
	}

	if err := Restore(context.Background(), f, bi, opts, archive, "correct horse battery"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(l.Data, "marker")); err == nil {
		t.Fatal("the old data is still in place")
	}
	if _, err := os.Stat(filepath.Join(l.DB, database.FileName)); err != nil {
		t.Fatalf("restored database missing: %v", err)
	}
	wrapped, err := os.ReadFile(l.HostKey)
	if err != nil || !bytes.Equal(wrapped, key) {
		t.Fatal("the backup's data key wasn't wrapped for this host")
	}
	if s := stateOf(t, f, "main"); s.Phase != maintenance.PhaseReady || !f.running["main"] {
		t.Fatalf("after restore: state %+v running %v", s, f.running["main"])
	}
	leftovers, _ := filepath.Glob(l.Data + ".*")
	if len(leftovers) != 0 {
		t.Fatalf("restore left %v behind", leftovers)
	}
}
