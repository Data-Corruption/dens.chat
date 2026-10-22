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
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/cache"
	"github.com/Data-Corruption/dens.chat/internal/den"
	"github.com/Data-Corruption/dens.chat/internal/denclient"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/instance"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"
	"github.com/Data-Corruption/dens.chat/internal/pairing"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/clientsessions"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/vaultstore"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/platform/release"
	"github.com/Data-Corruption/dens.chat/internal/shortcuts"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/internal/ui"
	"github.com/Data-Corruption/dens.chat/internal/vault"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

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
	// Den is the den this instance holds, created or not, which it serves
	// while hosting is on.
	Den *den.Den
	// Hosting is the switch that serves it (M9.1), which the service runs.
	Hosting Hosting
	// Dens keeps the dens this instance has joined connected.
	Dens *denclient.Manager
	// Shortcuts carries the voice keys from the desktop user's helper to
	// the pages (M7).
	Shortcuts *shortcuts.Hub
	// Cache is the client's cache of other dens' files (M6).
	Cache *cache.Store
	// Media runs the media module's jobs, the den's and the client's, one
	// at a time.
	Media *ffmpeg.Runner

	ReleaseSource release.ReleaseSource
	UserAgent     string

	buildInfo build.BuildInfo

	cleanup     []CleanupFunc
	cleanupOnce sync.Once
	closeErr    error

	// snapshotKept is the snapshot this start kept of an older version's
	// data, for the log (M9.3).
	snapshotKept string

	updateCheckMu sync.Mutex
	// Argon2id makes each password check expensive; the limiter keeps a
	// local caller from turning that into a denial of service.
	passwordMu      sync.Mutex
	passwordLimiter *rate.Limiter
}

// Hosting turns serving the den on and off from the settings (M9.1).
type Hosting interface {
	// State says whether hosting is on, and whether the den serves.
	State() HostingState
	// Set turns hosting on or off. Turning it on binds the den's ports,
	// and fails, changing nothing, while another program holds one.
	Set(ctx context.Context, on bool) error
}

// HostingState is hosting as the page shows it. Serving is whether the
// den listens now; while hosting is on and it doesn't, Problem says why.
type HostingState struct {
	On      bool
	Serving bool
	Problem string
}

// PortError is hosting that can't bind one of the den's ports, as one
// another program holds, or one the system keeps for itself. It names the
// port and the installer flag that moves it.
type PortError struct {
	Port    int
	Network string // tcp or udp
	// Flag and Parameter set the port in dens install and install.ps1.
	Flag, Parameter string
	Err             error
}

func (e *PortError) Error() string {
	return fmt.Sprintf("Hosting can't listen on port %d (%s): another program holds it, or the system keeps it. "+
		"Free it, or move the den to another port by running the installer again with %s, or %s on Windows.",
		e.Port, strings.ToUpper(e.Network), e.Flag, e.Parameter)
}

func (e *PortError) Unwrap() error { return e.Err }

// ErrCantHost is turning hosting on in a form that hosts no den.
var ErrCantHost = errors.New("hosting a den needs Dens installed on Linux or Windows, where it keeps running when nobody is signed in")

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

// FormOf is the form a process of this build runs in (M9): a development
// build's is Development; the release binary in Dens's Flatpak sandbox the
// Flatpak, so it runs as it is; and every other Installed.
func FormOf(bi build.BuildInfo) layout.Form {
	switch {
	case bi.DevMode:
		return layout.Development
	case inFlatpak():
		return layout.Flatpak
	}
	return layout.Installed
}

// OpenOptions select the instance and a development instance's defaults.
type OpenOptions struct {
	Instance string
	// LogLevel overrides the configured level for this run.
	LogLevel string
	// DevClientPort is the client port of a new development instance.
	DevClientPort int
	// DevDenPort, when set, gives a development instance's den that port
	// and turns its hosting on. Installed instances get their ports from
	// the installer, and hosting from the settings.
	DevDenPort int
	// DevMediaPorts, where set, are the UDP and TCP ports of a development
	// instance's calls, so two can host dens on one machine.
	DevMediaPorts [2]int
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
	l, err := layout.New(bi.Name, opts.Instance, FormOf(bi))
	if err != nil {
		return err
	}
	a.Layout = l
	var lease *maintenance.Lease
	if l.Form.Session() {
		if err := a.prepareSession(opts); err != nil {
			return fmt.Errorf("prepare the %s instance: %w", l.Form, err)
		}
		lease, err = maintenance.AcquireSessionLease(l, bi.Version, a.sessionOptions())
		err = a.explainRollback(err)
	} else {
		lease, err = maintenance.AcquireServiceLease(l, bi.Version)
	}
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
	a.Log.Infof("Starting %s %s, instance %s (%s, mode %d)", bi.Name, bi.Version, l.Instance, l.Form, lease.Mode)
	if a.snapshotKept != "" {
		a.Log.Infof("Kept a snapshot of the data from %s, before updating it, in %s", lease.State.Version, a.snapshotKept)
	}
	// tmp holds files only while the service works on them: uploads
	// arriving, files being prepared for sending, a backup's snapshot. So
	// whatever a crash left there goes before anything uses it (M8).
	if err := emptyDir(l.Temp); err != nil {
		return fmt.Errorf("empty %s: %w", l.Temp, err)
	}

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
	if lease.State.Phase == maintenance.PhaseRestoring {
		// Pairing belongs to a machine's browsers, not to the data: a
		// restored backup starts with none paired.
		if err := clientsessions.DeleteAll(context.Background(), a.DB); err != nil {
			return err
		}
		a.Log.Info("Cleared the browser sessions of the restored backup")
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find this binary: %w", err)
	}
	a.Media = ffmpeg.NewRunner([]string{exe, ffmpeg.WorkerCommand}, host.StartLowPriority,
		func(s string) { a.Log.Debugf("Media module: %s", s) })

	// Any instance may host, so each opens its den, which before it's
	// created is one row's read; the service serves it while hosting is on.
	storage := den.Storage{Dir: l.Uploads, Temp: l.Temp, Media: a.Media}
	if a.Den, err = den.Open(context.Background(), a.DB, a.Vault, a.Log, storage); err != nil {
		return err
	}
	a.AddCleanup(func() error { a.Den.Close(); return nil })
	if lease.State.Phase == maintenance.PhaseRestoring {
		// A backup can give a new message an ID that a member's client
		// holds for another, so their caches of this den go (M6).
		if err := a.Den.NewSyncEpoch(context.Background()); err != nil {
			return fmt.Errorf("start the restored den's sync epoch: %w", err)
		}
	}
	if l.Form == layout.Development {
		a.Den.RelaxLimits()
	}

	cfg, err := a.prepareHosting(lease.State.Phase, opts.DevDenPort != 0)
	if err != nil {
		return err
	}
	level := cfg.LogLevel
	if opts.LogLevel != "" {
		level = opts.LogLevel
	}
	if l.Form == layout.Development {
		level = "debug"
	}
	if err := a.Log.SetLevel(level); err != nil {
		return err
	}

	a.UserAgent = bi.UserAgent()
	a.ReleaseSource = &release.GenericReleaseSource{UserAgent: a.UserAgent}
	a.Dens = denclient.New(a.DB, a.Vault, a.Log, a.UserAgent, a.OwnDen)
	a.Shortcuts = shortcuts.NewHub(bi.Version, cfg.VoiceKeys, a.Log)
	a.Shortcuts.UseDesktop(cfg.DesktopKeys, func(on bool) {
		if _, err := config.Update(a.DB, func(c *types.Configuration) error {
			c.DesktopKeys = on
			return nil
		}); err != nil {
			a.Log.Warnf("Couldn't keep the member's use of the desktop's keys: %v", err)
		}
	})
	a.Dens.OnCall = a.Shortcuts.SetCall
	a.Dens.TempDir = l.Temp
	a.Dens.Media = a.Media
	if a.Cache, err = cache.Open(l.Cache, a.Vault, a.Log, cfg.CacheLimit); err != nil {
		return err
	}
	a.AddCleanup(a.Cache.Close)
	a.Dens.Cache = a.Cache
	return nil
}

// prepareHosting settles the hosting switch as the service starts, and
// returns the configuration with it (M9.1). The first start of a version
// that has the switch takes it from the den role v1.0's installer gave the
// instance, so one that hosted keeps hosting and every other starts off;
// from then on the switch alone decides. A restored backup starts off, as
// its browsers start unpaired: hosting is the machine's choice, not the
// data's. A development instance given a den port turns it on, as that
// gave it the den role before.
func (a *App) prepareHosting(phase maintenance.Phase, devDen bool) (*types.Configuration, error) {
	cfg, err := config.View(a.DB)
	if err != nil {
		return nil, err
	}
	var on bool
	var why string
	switch {
	case !a.CanHost():
		on, why = false, "since the "+a.Layout.Form.String()+" hosts no den"
	case phase == maintenance.PhaseRestoring:
		on, why = false, "after the restore"
	case devDen && a.Layout.Form == layout.Development:
		on, why = true, "for the den port this run gave"
	case cfg.Hosting == nil:
		on, why = a.Instance.Den.Enabled, "from the den role in the instance config"
	default:
		return cfg, nil
	}
	if cfg.Hosting != nil && *cfg.Hosting == on {
		return cfg, nil
	}
	if on {
		a.Log.Infof("Hosting is on, %s", why)
	} else {
		a.Log.Infof("Hosting is off, %s", why)
	}
	return config.Update(a.DB, func(c *types.Configuration) error {
		c.Hosting = &on
		return nil
	})
}

// CanHost reports whether the instance may host its den. The Flatpak may
// not: it runs in the member's session, which a den has to outlive, and
// has no Caddy beside it (M9.3).
func (a *App) CanHost() bool {
	switch a.Layout.Form {
	case layout.Flatpak:
		return false
	}
	return true
}

// emptyDir removes everything in dir, leaving dir itself.
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// OwnDen describes the den this instance hosts, and where its own client
// reaches it: the den listener on loopback, not the public address, so it
// works before DNS and Caddy do and behind routers without hairpin NAT.
func (a *App) OwnDen() (denproto.Den, string, bool) {
	if a.Den == nil {
		return denproto.Den{}, "", false
	}
	info, ok := a.Den.Info()
	return info, "http://127.0.0.1:" + strconv.Itoa(a.Instance.Den.Port), ok
}

func (a *App) loadKey() ([]byte, error) {
	switch a.Layout.Form {
	case layout.Installed:
		return host.LoadHostKey(a.Layout.HostKey)
	case layout.Development:
		return host.LoadOrCreateDevKey(a.Layout.HostKey)
	}
	return nil, fmt.Errorf("the %s has no data key", a.Layout.Form)
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

// prepareSession creates a session form's instance the way an installer
// creates an installed one: directories, an instance config naming its
// user, and a lifecycle state that lets this binary start.
func (a *App) prepareSession(opts OpenOptions) error {
	l := a.Layout
	if err := l.EnsureSession(); err != nil {
		return err
	}
	cfg, err := instance.Read(l.InstanceConfig)
	changed := false
	if errors.Is(err, fs.ErrNotExist) {
		me, err := host.CurrentUser()
		if err != nil {
			return err
		}
		cfg = instance.Default(l.Instance)
		cfg.DesktopUser = me
		if l.Form == layout.Flatpak {
			cfg.ClientPort = instance.FlatpakClientPort
		}
		if opts.DevClientPort != 0 {
			cfg.ClientPort = opts.DevClientPort
		}
		changed = true
	} else if err != nil {
		return err
	}
	// The den's ports matter only where the instance may host.
	if l.Form == layout.Development {
		if opts.DevDenPort != 0 && cfg.Den.Port != opts.DevDenPort {
			cfg.Den.Port = opts.DevDenPort
			changed = true
		}
		for i, port := range []*int{&cfg.Den.MediaUDPPort, &cfg.Den.MediaTCPPort} {
			if p := opts.DevMediaPorts[i]; p != 0 && *port != p {
				*port = p
				changed = true
			}
		}
	}
	if changed {
		data, err := instance.Encode(cfg)
		if err != nil {
			return err
		}
		if err := maintenance.WriteFileAtomic(l.InstanceConfig, data, nil); err != nil {
			return err
		}
	}
	// A development instance's key is a file of its own, made now so the
	// control directory's checks find it.
	if l.Form == layout.Development {
		key, err := host.LoadOrCreateDevKey(l.HostKey)
		if err != nil {
			return err
		}
		clear(key)
	}
	return nil
}

// MarkSessionReady publishes ready once a session form's start has done
// what its transition needed, as an installer does once the service it
// started is up. A restore's data replaced the instance's, which goes now.
func (a *App) MarkSessionReady() error {
	l := a.Layout
	if err := maintenance.MarkSessionReady(l, a.buildInfo.Version); err != nil {
		return err
	}
	if a.Lease.State.Phase != maintenance.PhaseRestoring {
		return nil
	}
	replaced, err := filepath.Glob(l.Data + ".replaced-*")
	if err != nil {
		return err
	}
	for _, dir := range replaced {
		if err := os.RemoveAll(dir); err != nil {
			a.Log.Warnf("Couldn't remove the data the restore replaced, in %s: %v", dir, err)
			continue
		}
		a.Log.Infof("Removed the data the restore replaced, from %s", dir)
	}
	return nil
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
