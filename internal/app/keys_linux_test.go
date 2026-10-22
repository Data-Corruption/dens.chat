package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/vault"
)

const flatpakPassword = "correct horse battery"

var flatpakBuild = build.BuildInfo{Name: "dens", Version: "v1.1.0", DefaultLogLevel: "warn"}

// flatpakHome gives the Flatpak's layout the directories Flatpak gives its
// sandbox, below the test's own.
func flatpakHome(t *testing.T) {
	t.Helper()
	app := filepath.Join(t.TempDir(), "app")
	t.Setenv("XDG_DATA_HOME", filepath.Join(app, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(app, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(app, "run"))
}

// keyring answers the Secret portal's requests as the test sets it: its
// secret, a random one each time as KWallet before 6.30 answers a
// cancelled prompt, no keyring at all, or a request held as a prompt holds
// it.
type keyring struct {
	mu     sync.Mutex
	secret []byte
	random bool
	none   bool
	hold   chan struct{}
	asked  int
}

func (k *keyring) retrieve(ctx context.Context, _ time.Duration) ([]byte, error) {
	k.mu.Lock()
	k.asked++
	hold, none, random, secret := k.hold, k.none, k.random, append([]byte(nil), k.secret...)
	k.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	switch {
	case none:
		return nil, errors.ErrUnsupported
	case random:
		answer := make([]byte, 64)
		_, _ = rand.Read(answer)
		return answer, nil
	}
	return secret, nil
}

func (k *keyring) set(f func(k *keyring)) {
	k.mu.Lock()
	defer k.mu.Unlock()
	f(k)
}

// openFlatpak opens the Flatpak's instance in the test's directories, its
// keyring the test's.
func openFlatpak(t *testing.T, k *keyring) *App {
	t.Helper()
	a := New(flatpakBuild)
	a.RetrieveSecret = k.retrieve
	form := layout.Flatpak
	if err := a.Open(OpenOptions{Instance: "main", Form: &form}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("waited for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// A new Flatpak's key waits in a file until the local password exists,
// which removes it unless the member asks Dens to remember; from then on
// the service starts locked, and only the password opens it, which is
// never written anywhere (M9.3).
func TestFlatpakStartsLockedOnceItHasAPassword(t *testing.T) {
	flatpakHome(t)
	k := &keyring{none: true}
	a := openFlatpak(t, k)
	l := a.Layout
	if a.Locked() || !exists(l.HostKey) || a.Den == nil {
		t.Fatalf("a new instance: locked %v, its key's file there %v", a.Locked(), exists(l.HostKey))
	}
	if info, err := os.Stat(l.HostKey); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the key's file: %v, %v", info, err)
	}
	if err := a.SetPassword(context.Background(), flatpakPassword); err != nil {
		t.Fatal(err)
	}
	if err := a.PasswordChosen(false); err != nil {
		t.Fatal(err)
	}
	if exists(l.HostKey) || a.Remembering().On {
		t.Fatalf("after the password, nothing remembered: %+v, file %v", a.Remembering(), exists(l.HostKey))
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	a = openFlatpak(t, k)
	if !a.Locked() || a.Den != nil || a.Vault != nil {
		t.Fatal("the instance starts unlocked with nothing remembered")
	}
	select {
	case <-a.Unlocked():
		t.Fatal("Unlocked is closed while locked")
	default:
	}
	if err := a.VerifyPassword(context.Background(), flatpakPassword); !errors.Is(err, ErrLocked) {
		t.Fatalf("VerifyPassword while locked = %v", err)
	}
	if err := a.UnlockWithPassword(context.Background(), "wrong wrong wrong", false); !errors.Is(err, vault.ErrWrongPassword) {
		t.Fatalf("a wrong password = %v", err)
	}
	if err := a.UnlockWithPassword(context.Background(), flatpakPassword, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a.Unlocked():
	default:
		t.Fatal("Unlocked isn't closed once unlocked")
	}
	if a.Locked() || a.Den == nil || a.Dens == nil || a.Cache == nil || a.Shortcuts == nil {
		t.Fatal("unlocking didn't finish opening")
	}
	// The password is never written anywhere: not in the instance's files.
	_ = filepath.WalkDir(filepath.Dir(filepath.Dir(l.Root)), func(path string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if data, err := os.ReadFile(path); err == nil && bytes.Contains(data, []byte(flatpakPassword)) {
				t.Errorf("%s holds the local password", path)
			}
		}
		return nil
	})
}

// Remember keeps the key in the keyring once it answers the same twice, and
// Dens then starts unlocked through it; a keyring answering differently
// each time, or none, has the key kept in a file, which the settings say;
// and turning Remember off deletes the copy (M9.3).
func TestFlatpakRemembers(t *testing.T) {
	flatpakHome(t)
	secret := bytes.Repeat([]byte{7}, 64)
	k := &keyring{secret: secret}
	a := openFlatpak(t, k)
	l := a.Layout
	if err := a.SetPassword(context.Background(), flatpakPassword); err != nil {
		t.Fatal(err)
	}
	if err := a.PasswordChosen(true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the keyring copy", func() bool { r := a.Remembering(); return r.Where == "keyring" && !r.Asking })
	if exists(l.HostKey) {
		t.Fatal("the key's file stayed beside the keyring's copy")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	a = openFlatpak(t, k)
	if !a.Locked() {
		t.Fatal("a Flatpak remembering in the keyring opened before asking it")
	}
	a.TryKeyring()
	waitFor(t, "the keyring to open Dens", func() bool { return !a.Locked() })

	for _, c := range []struct {
		why     string
		set     func(k *keyring)
		problem string
	}{
		{"a keyring answering differently", func(k *keyring) { k.random = true }, "different secret"},
		{"no keyring", func(k *keyring) { k.random, k.none = false, true }, "no keyring"},
	} {
		if err := a.SetRemember(context.Background(), "", false); err != nil {
			t.Fatal(err)
		}
		if exists(l.KeyringKey) || exists(l.HostKey) || a.Remembering().On {
			t.Fatalf("%s: turning Remember off left %+v", c.why, a.Remembering())
		}
		k.set(c.set)
		if err := a.SetRemember(context.Background(), "wrong wrong wrong", true); !errors.Is(err, vault.ErrWrongPassword) {
			t.Fatalf("%s: Remember with a wrong password = %v", c.why, err)
		}
		time.Sleep(2 * time.Second) // the password limiter's pace
		if err := a.SetRemember(context.Background(), flatpakPassword, true); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the copy in a file", func() bool { r := a.Remembering(); return r.Where == "file" && !r.Asking })
		if r := a.Remembering(); !bytes.Contains([]byte(r.Problem), []byte(c.problem)) || exists(l.KeyringKey) {
			t.Fatalf("%s: %+v", c.why, r)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a = openFlatpak(t, k)
	if a.Locked() {
		t.Fatal("a Flatpak remembering in a file started locked")
	}
}

// A copy remembered in the keyring that its answer no longer opens leaves
// Dens locked and stays while it doesn't; once the password has opened
// Dens, an answer the same as the last is the keyring's secret now, and
// Dens remembers anew, but not on answers that differ (M9.3).
func TestFlatpakKeyringThatStopsOpening(t *testing.T) {
	flatpakHome(t)
	first := bytes.Repeat([]byte{1}, 64)
	k := &keyring{secret: first}
	a := openFlatpak(t, k)
	l := a.Layout
	if err := a.SetPassword(context.Background(), flatpakPassword); err != nil {
		t.Fatal(err)
	}
	if err := a.PasswordChosen(true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the keyring copy", func() bool { return a.Remembering().Where == "keyring" && !a.Remembering().Asking })
	sealed, err := os.ReadFile(l.KeyringKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// KWallet's answer to a cancelled prompt: random, each time.
	k.set(func(k *keyring) { k.random = true })
	a = openFlatpak(t, k)
	a.TryKeyring()
	waitFor(t, "the first answer", func() bool { k.mu.Lock(); defer k.mu.Unlock(); return k.asked >= 3 })
	if !a.Locked() {
		t.Fatal("a random answer opened Dens")
	}
	if err := a.UnlockWithPassword(context.Background(), flatpakPassword, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second answer", func() bool { k.mu.Lock(); defer k.mu.Unlock(); return k.asked >= 4 })
	time.Sleep(100 * time.Millisecond)
	if now, err := os.ReadFile(l.KeyringKey); err != nil || !bytes.Equal(now, sealed) {
		t.Fatalf("answers that differ replaced the copy: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// A keyring reset: a new secret, the same each time.
	second := bytes.Repeat([]byte{2}, 64)
	k.set(func(k *keyring) { k.random, k.secret = false, second })
	a = openFlatpak(t, k)
	a.TryKeyring()
	waitFor(t, "the first answer", func() bool { k.mu.Lock(); defer k.mu.Unlock(); return k.asked >= 5 })
	if !a.Locked() {
		t.Fatal("another secret opened Dens")
	}
	if now, err := os.ReadFile(l.KeyringKey); err != nil || !bytes.Equal(now, sealed) {
		t.Fatal("the copy went while it didn't open")
	}
	if err := a.UnlockWithPassword(context.Background(), flatpakPassword, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the copy remembered anew", func() bool {
		now, err := os.ReadFile(l.KeyringKey)
		return err == nil && !bytes.Equal(now, sealed)
	})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a = openFlatpak(t, k)
	a.TryKeyring()
	waitFor(t, "the new secret to open Dens", func() bool { return !a.Locked() })
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// Opened with the password and Remember turned off, while the keyring
	// answers what doesn't open the copy, the copy goes, and the keyring's
	// next answer brings none back.
	third := bytes.Repeat([]byte{4}, 64)
	k.set(func(k *keyring) { k.secret = third })
	a = openFlatpak(t, k)
	a.TryKeyring()
	waitFor(t, "the first answer", func() bool { k.mu.Lock(); defer k.mu.Unlock(); return k.asked >= 8 })
	if err := a.UnlockWithPassword(context.Background(), flatpakPassword, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if exists(l.KeyringKey) || exists(l.HostKey) {
		t.Fatal("Dens remembered a key unlocked without Remember")
	}
}

// A request a prompt holds doesn't hold the start: the local password
// opens Dens meanwhile, and a request to remember that's turned off while
// held writes nothing (M9.3).
func TestFlatpakKeyringPromptHeld(t *testing.T) {
	flatpakHome(t)
	secret := bytes.Repeat([]byte{3}, 64)
	k := &keyring{secret: secret}
	a := openFlatpak(t, k)
	l := a.Layout
	if err := a.SetPassword(context.Background(), flatpakPassword); err != nil {
		t.Fatal(err)
	}
	if err := a.PasswordChosen(true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the keyring copy", func() bool { return a.Remembering().Where == "keyring" && !a.Remembering().Asking })
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	hold := make(chan struct{})
	k.set(func(k *keyring) { k.hold = hold })
	a = openFlatpak(t, k)
	a.TryKeyring()
	if err := a.UnlockWithPassword(context.Background(), flatpakPassword, true); err != nil {
		t.Fatal(err)
	}
	if a.Locked() {
		t.Fatal("the password didn't open Dens while the keyring's prompt was up")
	}
	close(hold)

	k.set(func(k *keyring) { k.hold = make(chan struct{}) })
	if err := a.SetRemember(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	if err := a.SetRemember(context.Background(), flatpakPassword, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the request to remember", func() bool { return a.Remembering().Asking })
	if err := a.SetRemember(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	if r := a.Remembering(); r.On || r.Asking || exists(l.KeyringKey) || exists(l.HostKey) {
		t.Fatalf("Remember turned off while its request was held: %+v", r)
	}
}
