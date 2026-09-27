// Package app composes the service process: it checks the installation,
// takes the lifecycle lease, and opens the logs, the vault and the database
// that the service's components share. CLI commands other than the service
// never open an instance's storage; they talk to the service over the
// control endpoint or, elevated, run a maintenance transaction.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/pairing"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/vaultstore"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/platform/release"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/internal/ui"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"golang.org/x/mod/semver"
	"golang.org/x/time/rate"
)

type CleanupFunc func() error

// App is the service's composition root.
type App struct {
	Log      *xlog.Logger
	DB       *sql.DB
	Layout   layout.Layout
	Instance instance.Config
	Vault    *vault.Vault
	UI       *ui.UI
	Pairing  *pairing.Store
	Lease    *maintenance.Lease

	ReleaseSource release.ReleaseSource
	UserAgent     string

	buildInfo build.BuildInfo

	cleanup     []CleanupFunc
	cleanupOnce sync.Once
	closeErr    error

	updateCheckMu sync.Mutex
	// Argon2id makes each password check expensive; the limiter keeps a
	// local caller from turning that into a denial of service.
	passwordMu      sync.Mutex
	passwordLimiter *rate.Limiter
}

// ErrNoPassword reports that no local password has been set yet.
var ErrNoPassword = errors.New("no local password is set yet")

// ErrTooManyAttempts reports password checks arriving faster than allowed.
var ErrTooManyAttempts = errors.New("too many password attempts; wait a moment and try again")

func New(buildInfo build.BuildInfo) *App {
	return &App{
		buildInfo:       buildInfo,
		Pairing:         pairing.New(),
		passwordLimiter: rate.NewLimiter(rate.Every(2*time.Second), 3),
	}
}

func (a *App) BuildInfo() build.BuildInfo { return a.buildInfo }

// DevMode reports whether this is a development build.
func (a *App) DevMode() bool { return a.buildInfo.DevMode }

// OpenOptions select the instance and development defaults.
type OpenOptions struct {
	Instance string
	// LogLevel overrides the configured level for this run.
	LogLevel string
	// DevClientPort is the client port of a new development instance.
	DevClientPort int
}

// Open prepares the service. Every failure wraps host.ErrRefused: a service
// that can't open its installation must not be restarted in a loop.
func (a *App) Open(opts OpenOptions) error {
	if err := a.open(opts); err != nil {
		if errors.Is(err, host.ErrRefused) {
			return err
		}
		return fmt.Errorf("%w: %w", host.ErrRefused, err)
	}
	return nil
}

func (a *App) open(opts OpenOptions) error {
	bi := a.buildInfo
	if bi.Version == "" {
		return errors.New("this build has no version")
	}
	l, err := layout.New(bi.Name, opts.Instance, bi.DevMode)
	if err != nil {
		return err
	}
	a.Layout = l
	if l.Dev {
		if err := a.prepareDev(opts); err != nil {
			return fmt.Errorf("prepare development instance: %w", err)
		}
	}

	lease, err := maintenance.AcquireServiceLease(l, bi.Version)
	if err != nil {
		return err
	}
	a.Lease = lease
	a.AddCleanup(lease.Close)
	if err := l.CheckControl(l.ServiceControlFiles()...); err != nil {
		return err
	}
	if err := l.CheckData(); err != nil {
		return err
	}
	if err := l.EnsureData(); err != nil {
		return err
	}

	a.Log, err = xlog.New(l.Logs, "debug")
	if err != nil {
		return fmt.Errorf("open logs: %w", err)
	}
	a.Log.Infof("Starting %s %s, instance %s (dev=%t, mode %d)", bi.Name, bi.Version, l.Instance, l.Dev, lease.Mode)

	a.Instance, err = instance.Read(l.InstanceConfig)
	if err != nil {
		return err
	}
	if a.Instance.Name != l.Instance {
		return fmt.Errorf("instance config names %q, not %q", a.Instance.Name, l.Instance)
	}

	key, err := a.loadKey()
	if err != nil {
		return err
	}
	a.Vault, err = vault.New(key)
	clear(key)
	if err != nil {
		return fmt.Errorf("hold data key: %w", err)
	}
	a.AddCleanup(func() error { a.Vault.Close(); return nil })

	policy := database.RequireCurrentSchema
	if lease.Mode == maintenance.StartMigrate {
		policy = database.ApplyPendingMigrations
	}
	a.DB, err = database.New(l.DB, a.Log, bi, policy)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	a.AddCleanup(func() error {
		_, stateErr := config.Update(a.DB, func(cfg *types.Configuration) error {
			cfg.LastShutdownVersion = bi.Version
			return nil
		})
		return errors.Join(stateErr, a.DB.Close())
	})
	if err := a.checkVault(lease.Mode); err != nil {
		return err
	}

	cfg, err := config.View(a.DB)
	if err != nil {
		return err
	}
	level := cfg.LogLevel
	if opts.LogLevel != "" {
		level = opts.LogLevel
	}
	if l.Dev {
		level = "debug"
	}
	if err := a.Log.SetLevel(level); err != nil {
		return err
	}

	mmVer := strings.TrimPrefix(semver.MajorMinor(bi.Version), "v")
	a.UserAgent = fmt.Sprintf("Mozilla/5.0 (compatible; %s/%s; +%s)", bi.Name, mmVer, bi.ContactURL)
	a.ReleaseSource = &release.GenericReleaseSource{UserAgent: a.UserAgent}
	return nil
}

func (a *App) loadKey() ([]byte, error) {
	if a.Layout.Dev {
		return host.LoadOrCreateDevKey(a.Layout.HostKey)
	}
	return host.LoadHostKey(a.Layout.HostKey)
}

// checkVault ties the host-bound data key to this database. A first start
// records the key's check value; every later start requires a match, so a
// data directory paired with the wrong key is refused rather than read as
// garbage.
func (a *App) checkVault(mode maintenance.StartMode) error {
	ctx := context.Background()
	row, err := vaultstore.Get(ctx, a.DB)
	if err != nil {
		return err
	}
	if row == nil {
		if mode != maintenance.StartMigrate {
			return errors.New("the database has no vault; run the installer")
		}
		a.Log.Info("Initializing vault")
		return vaultstore.Init(ctx, a.DB, a.Vault.CheckValue())
	}
	if err := a.Vault.VerifyCheckValue(row.CheckValue); err != nil {
		return fmt.Errorf("%w: the host-bound key belongs to another installation", err)
	}
	return nil
}

// prepareDev creates a development instance the way an installer would:
// directories, an instance config naming the developer, and a
// lifecycle state that lets this binary start.
func (a *App) prepareDev(opts OpenOptions) error {
	l := a.Layout
	if err := l.EnsureDev(); err != nil {
		return err
	}
	if _, err := instance.Read(l.InstanceConfig); errors.Is(err, fs.ErrNotExist) {
		me, err := host.CurrentUser()
		if err != nil {
			return err
		}
		cfg := instance.Default(l.Instance)
		cfg.DesktopUser = me
		if opts.DevClientPort != 0 {
			cfg.ClientPort = opts.DevClientPort
		}
		data, err := instance.Encode(cfg)
		if err != nil {
			return err
		}
		if err := maintenance.WriteFileAtomic(l.InstanceConfig, data, nil); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	// Create the key now so the control directory checks find it.
	key, err := host.LoadOrCreateDevKey(l.HostKey)
	if err != nil {
		return err
	}
	clear(key)
	return maintenance.EnsureDevReady(l, a.buildInfo.Version)
}

// PasswordSet reports whether a local password has been set.
func (a *App) PasswordSet(ctx context.Context) (bool, error) {
	row, err := vaultstore.Get(ctx, a.DB)
	if err != nil {
		return false, err
	}
	return row != nil && row.PasswordWrap != "", nil
}

// VerifyPassword checks the local password, rate limited.
func (a *App) VerifyPassword(ctx context.Context, password string) error {
	if !a.passwordLimiter.Allow() {
		return ErrTooManyAttempts
	}
	a.passwordMu.Lock()
	defer a.passwordMu.Unlock()
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
	return a.Vault.VerifyPassword(wrap, password)
}

// SetPassword wraps the data key with a new local password.
func (a *App) SetPassword(ctx context.Context, password string) error {
	a.passwordMu.Lock()
	defer a.passwordMu.Unlock()
	wrap, err := a.Vault.WrapWithPassword(password)
	if err != nil {
		return err
	}
	encoded, err := vault.MarshalWrap(wrap)
	if err != nil {
		return err
	}
	return vaultstore.SetPasswordWrap(ctx, a.DB, encoded)
}

// Close runs cleanup functions in reverse order, logs their joined failure
// while the logger is still alive, and closes the logger last.
func (a *App) Close() error {
	a.cleanupOnce.Do(func() {
		var joined error
		for _, f := range slices.Backward(a.cleanup) {
			if err := f(); err != nil {
				joined = errors.Join(joined, err)
			}
		}
		if joined != nil && a.Log != nil {
			a.Log.Errorf("cleanup failed: %v", joined)
		}
		if a.Log != nil {
			joined = errors.Join(joined, a.Log.Close())
		}
		a.closeErr = joined
	})
	return a.closeErr
}

func (a *App) AddCleanup(f CleanupFunc) {
	if f != nil {
		a.cleanup = append(a.cleanup, f)
	}
}
