package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/vaultstore"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"
)

// The Flatpak's data key has no copy that opens without the member, unless
// they ask Dens to remember it (M9.3). The service starts locked, serving
// the page nothing but its unlock screen, and the local password, whose
// copy of the key every install keeps, opens it. Remembered, a copy sealed
// with the secret the member's keyring keeps for the app, through the
// Secret portal, opens it as it starts; where the keyring can't be trusted,
// a copy in a file does. A new instance's key waits in that file until the
// local password exists.

// ErrLocked is what needs the data key, asked of a locked Flatpak.
var ErrLocked = errors.New("Dens is locked; unlock it with the local password")

// keyringWait is how long a request waits for the Secret portal's
// interface, which can be missing for a few seconds at sign-in while the
// desktop starts its backends.
const keyringWait = time.Minute

// Remembering is the Flatpak's remembered copy of its key, as the settings
// show it (M9.3). On is whether a copy opens Dens without the local
// password, and Where it is: "keyring" or "file". Asking is the keyring
// answering a request to remember; Problem is why the last try kept the key
// in a file rather than the keyring.
type Remembering struct {
	On      bool   `json:"on"`
	Where   string `json:"where,omitempty"`
	Asking  bool   `json:"asking,omitempty"`
	Problem string `json:"problem,omitempty"`
}

// keyState is the Flatpak's locked start and the requests to its keyring:
// one at a time, each under a context that ends with the service.
type keyState struct {
	mu       sync.Mutex
	locked   bool
	unlocked chan struct{} // closed once the key opens

	ctx     context.Context
	cancel  context.CancelFunc
	running sync.WaitGroup
	// asking is a request to remember under way, which cancelAsk stops;
	// answered is closed once it's done.
	asking    bool
	cancelAsk context.CancelFunc
	answered  chan struct{}
	problem   string
}

// backgroundLocked runs f under a context of the keys' own, which Close
// ends and waits for before anything f uses closes. It returns the
// context's cancel.
func (k *keyState) backgroundLocked(f func(ctx context.Context)) context.CancelFunc {
	if k.ctx == nil {
		k.ctx, k.cancel = context.WithCancel(context.Background())
	}
	ctx, cancel := context.WithCancel(k.ctx)
	k.running.Add(1)
	go func() {
		defer k.running.Done()
		defer cancel()
		f(ctx)
	}()
	return cancel
}

func (k *keyState) stop() {
	k.mu.Lock()
	if k.cancel != nil {
		k.cancel()
	}
	k.mu.Unlock()
	k.running.Wait()
}

// Locked reports whether the instance waits for its key.
func (a *App) Locked() bool {
	a.keys.mu.Lock()
	defer a.keys.mu.Unlock()
	return a.keys.locked
}

// Unlocked is closed once the instance has its key.
func (a *App) Unlocked() <-chan struct{} { return a.keys.unlocked }

// flatpakStartKey is the Flatpak's key where a copy opens it as it starts:
// the file a new instance's key waits in until the local password exists,
// or the remembered one. It's nil where the member must open it, locked.
func (a *App) flatpakStartKey() ([]byte, error) {
	l := a.Layout
	key, err := readKeyFile(l, l.HostKey)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return key, err
	}
	row, err := vaultstore.Get(context.Background(), a.DB)
	if err != nil {
		return nil, err
	}
	switch {
	case row == nil:
		key = make([]byte, host.HostKeySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate the data key: %w", err)
		}
		if err := writeKeyFile(l.HostKey, key); err != nil {
			clear(key)
			return nil, err
		}
		a.Log.Info("Made a data key, which waits in a file until the local password exists")
		return key, nil
	case row.PasswordWrap == "":
		return nil, fmt.Errorf("Dens's data key is gone: it waited in %s for a local password, which was never set, and the file is missing. "+
			"The data can't be opened; to start over, remove %s", l.HostKey, l.Root)
	}
	return nil, nil
}

// readKeyFile reads a data key kept in a file of the control directory,
// whose ownership and permissions it checks first, as the control files'.
func readKeyFile(l layout.Layout, path string) ([]byte, error) {
	if _, err := os.Lstat(path); err != nil {
		return nil, err
	}
	if err := l.CheckControl(path); err != nil {
		return nil, err
	}
	file, err := xsyscall.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4096))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// writeKeyFile keeps data in a file only the member may read.
func writeKeyFile(path string, data []byte) error {
	if err := maintenance.WriteFileAtomic(path, data, func(temp string) error { return os.Chmod(temp, 0o600) }); err != nil {
		return fmt.Errorf("keep the data key: %w", err)
	}
	return nil
}

// UnlockWithPassword opens a locked Flatpak with the local password, which
// opens the key's password copy, and finishes opening it (M9.3). With
// remember, a copy that opens Dens without the password is kept, where
// there's none yet; without, the one there is goes. Unlocked already, it
// does nothing.
func (a *App) UnlockWithPassword(ctx context.Context, password string, remember bool) error {
	if !a.passwordLimiter.Allow() {
		return ErrTooManyAttempts
	}
	a.passwordMu.Lock()
	defer a.passwordMu.Unlock()
	if !a.Locked() {
		return nil
	}
	row, err := vaultstore.Get(ctx, a.DB)
	if err != nil {
		return err
	}
	if row == nil || row.PasswordWrap == "" {
		return ErrNoPassword
	}
	wrap, err := vault.UnmarshalWrap(row.PasswordWrap)
	if err != nil {
		return err
	}
	key, err := vault.UnwrapWithPassword(wrap, password)
	if err != nil {
		return err
	}
	defer clear(key)
	if err := a.unlockWith(key, "the local password"); err != nil {
		return err
	}
	switch on := a.Remembering().On; {
	case remember && !on:
		a.startRemembering()
	case !remember && on:
		return a.forget()
	}
	return nil
}

// unlockWith finishes opening a locked instance with its key.
func (a *App) unlockWith(key []byte, how string) error {
	a.keys.mu.Lock()
	defer a.keys.mu.Unlock()
	if !a.keys.locked {
		return nil
	}
	if err := a.unlock(key); err != nil {
		return err
	}
	a.keys.locked = false
	close(a.keys.unlocked)
	a.Log.Infof("Unlocked with %s", how)
	return nil
}

// TryKeyring opens a locked Flatpak with the copy it remembered in the
// keyring, if it has one, while the page offers the local password (M9.3).
// It asks the Secret portal one request at a time, and never gives up on
// one, which a prompt holds for as long as the member takes. A copy the
// answer doesn't open stays, and Dens stays locked: the keyring was reset,
// or answered a cancelled prompt with a random secret, as KWallet before
// KDE Frameworks 6.30 does. Once the password has opened Dens, it asks once
// more, and an answer the same as that one is the keyring's secret now, so
// it remembers anew.
func (a *App) TryKeyring() {
	data, err := readKeyFile(a.Layout, a.Layout.KeyringKey)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			a.Log.Warnf("Couldn't read the key remembered in the keyring: %v", err)
		}
		return
	}
	wrap, err := vault.UnmarshalKeyringWrap(data)
	if err != nil {
		a.Log.Warnf("Couldn't read the key remembered in the keyring: %v", err)
		return
	}
	a.keys.mu.Lock()
	defer a.keys.mu.Unlock()
	a.keys.backgroundLocked(func(ctx context.Context) {
		var stale []byte
		for range 2 {
			secret, err := a.RetrieveSecret(ctx, keyringWait)
			if err != nil {
				if ctx.Err() == nil {
					a.Log.Infof("The keyring didn't open Dens: %v", err)
				}
				return
			}
			key, err := vault.UnwrapWithKeyring(wrap, secret)
			if err == nil {
				clear(secret)
				if err := a.unlockWith(key, "the key remembered in the keyring"); err != nil {
					a.Log.Errorf("Couldn't finish opening with the key the keyring opened: %v", err)
				}
				clear(key)
				return
			}
			sum := sha256.Sum256(secret)
			if stale != nil && bytes.Equal(stale, sum[:]) && !a.Locked() {
				a.rememberInKeyring(secret)
				clear(secret)
				return
			}
			clear(secret)
			stale = sum[:]
			a.Log.Warnf("The keyring's answer doesn't open the key Dens remembered there; the local password opens Dens")
			select {
			case <-a.Unlocked():
			case <-ctx.Done():
				return
			}
			// Unlocked without Remember, the copy went, and stays gone.
			if _, err := os.Lstat(a.Layout.KeyringKey); err != nil {
				return
			}
		}
	})
}

// rememberInKeyring keeps a copy of the key sealed with the keyring's
// secret, in place of the copy in a file, if any.
func (a *App) rememberInKeyring(secret []byte) {
	wrap, err := a.Vault.WrapWithKeyring(secret)
	var data []byte
	if err == nil {
		data, err = vault.MarshalKeyringWrap(wrap)
	}
	if err == nil {
		err = writeKeyFile(a.Layout.KeyringKey, data)
	}
	if err != nil {
		a.Log.Errorf("Couldn't remember the key in the keyring: %v", err)
		return
	}
	if err := os.Remove(a.Layout.HostKey); err != nil && !errors.Is(err, fs.ErrNotExist) {
		a.Log.Errorf("Couldn't remove the copy of the key in a file: %v", err)
	}
	a.Log.Info("Remembered the key in the keyring")
}

// Remembering says whether the Flatpak keeps a copy of its key that opens
// it without the local password, and where.
func (a *App) Remembering() Remembering {
	l := a.Layout
	if l.Form != layout.Flatpak {
		return Remembering{}
	}
	a.keys.mu.Lock()
	r := Remembering{Asking: a.keys.asking, Problem: a.keys.problem}
	a.keys.mu.Unlock()
	if _, err := os.Lstat(l.KeyringKey); err == nil {
		r.On, r.Where = true, "keyring"
	} else if _, err := os.Lstat(l.HostKey); err == nil {
		// Before the local password exists, the file is where the key
		// waits for it, not a copy the member asked for.
		if set, err := a.PasswordSet(context.Background()); err == nil && set {
			r.On, r.Where = true, "file"
		}
	}
	return r
}

// SetRemember turns remembering on, after the local password, or off,
// deleting the remembered copy (M9.3).
func (a *App) SetRemember(ctx context.Context, password string, on bool) error {
	if a.Layout.Form != layout.Flatpak {
		return errors.New("only the Flatpak remembers its key; the others open it themselves")
	}
	if !on {
		return a.forget()
	}
	if err := a.VerifyPassword(ctx, password); err != nil {
		return err
	}
	if !a.Remembering().On {
		a.startRemembering()
	}
	return nil
}

// startRemembering asks the keyring for its secret twice, in the
// background, and keeps a copy of the key sealed with it once both answers
// match. Anything else, a keyring that's missing, fails, or answers
// differently, keeps the copy in a file, which the settings then say.
func (a *App) startRemembering() {
	a.keys.mu.Lock()
	defer a.keys.mu.Unlock()
	if a.keys.asking {
		return
	}
	done := make(chan struct{})
	a.keys.asking, a.keys.answered, a.keys.problem = true, done, ""
	a.keys.cancelAsk = a.keys.backgroundLocked(func(ctx context.Context) {
		defer func() {
			a.keys.mu.Lock()
			a.keys.asking, a.keys.cancelAsk = false, nil
			a.keys.mu.Unlock()
			close(done)
		}()
		problem := a.askKeyringTwice(ctx)
		if ctx.Err() != nil {
			return
		}
		if problem == "" {
			return
		}
		a.Log.Infof("Remembering the key in a file: %s", problem)
		if err := os.Remove(a.Layout.KeyringKey); err != nil && !errors.Is(err, fs.ErrNotExist) {
			a.Log.Errorf("Couldn't remove the key remembered in the keyring: %v", err)
		}
		if err := a.rememberInFile(); err != nil {
			a.Log.Errorf("Couldn't remember the key in a file: %v", err)
			problem = "Dens couldn't keep it in a file either: " + err.Error()
		}
		a.keys.mu.Lock()
		a.keys.problem = problem
		a.keys.mu.Unlock()
	})
}

// askKeyringTwice remembers the key in the keyring if it answers the same
// twice, or says why not.
func (a *App) askKeyringTwice(ctx context.Context) string {
	first, err := a.RetrieveSecret(ctx, keyringWait)
	if err != nil {
		return keyringProblem(err)
	}
	defer clear(first)
	second, err := a.RetrieveSecret(ctx, keyringWait)
	if err != nil {
		return keyringProblem(err)
	}
	defer clear(second)
	if !bytes.Equal(first, second) {
		return "Your keyring answered with a different secret each time, so Dens can't trust it."
	}
	a.rememberInKeyring(first)
	return ""
}

// keyringProblem says why the keyring didn't give Dens its secret.
func keyringProblem(err error) string {
	switch {
	case errors.Is(err, errors.ErrUnsupported):
		return "Your desktop has no keyring Dens can reach."
	case errors.Is(err, host.ErrDesktopCancelled):
		return "Your keyring's prompt was turned down."
	}
	return "Your keyring didn't give Dens its secret: " + err.Error()
}

// rememberInFile keeps a copy of the key in a file beside the data.
func (a *App) rememberInFile() error {
	key := a.Vault.CopyKey()
	defer clear(key)
	return writeKeyFile(a.Layout.HostKey, key)
}

// forget stops a request to remember and deletes the remembered copies.
func (a *App) forget() error {
	a.keys.mu.Lock()
	asking, cancel, done := a.keys.asking, a.keys.cancelAsk, a.keys.answered
	a.keys.problem = ""
	a.keys.mu.Unlock()
	if asking {
		cancel()
		<-done
	}
	forgot := false
	for _, path := range []string{a.Layout.KeyringKey, a.Layout.HostKey} {
		switch err := os.Remove(path); {
		case err == nil:
			forgot = true
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("forget the remembered key: %w", err)
		}
	}
	if forgot {
		a.Log.Info("Forgot the remembered key")
	}
	return nil
}

// PasswordChosen does what the Flatpak does as the local password is first
// set (M9.3): the key's file, where it waited for the password, goes unless
// the member asks Dens to remember, which keeps a copy as Remember does.
func (a *App) PasswordChosen(remember bool) error {
	if a.Layout.Form != layout.Flatpak {
		return nil
	}
	if remember {
		a.startRemembering()
		return nil
	}
	if err := os.Remove(a.Layout.HostKey); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the key's file: %w", err)
	}
	return nil
}
