// Package app composes the application and owns process-local cleanup.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"

	"github.com/Data-Corruption/dens.chat/internal/platform/release"
	"github.com/Data-Corruption/dens.chat/internal/platform/secrets"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/internal/ui"
	"github.com/Data-Corruption/dens.chat/pkg/x"
	"github.com/Data-Corruption/dens.chat/pkg/xhttp"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
	"github.com/Data-Corruption/dens.chat/pkg/xsyscall"

	"github.com/urfave/cli/v3"
	"golang.org/x/mod/semver"
)

type CleanupFunc func() error

// App is the process composition root. Filesystem policy and cross-process
// install/update/uninstall coordination are in layout and maintenance.
type App struct {
	DB  *sql.DB
	Log *xlog.Logger

	Server      *xhttp.Server
	ProxyServer *xhttp.Server
	Secrets     *secrets.Store
	UI          *ui.UI
	BaseURL     string

	UserAgent string
	Layout    layout.Layout

	ReleaseSource release.ReleaseSource

	// DevMode is baked into local development builds. Its storage and lifecycle
	// state are isolated from production installations.
	DevMode   bool
	buildInfo build.BuildInfo

	cleanup            []CleanupFunc
	cleanupOnce        sync.Once
	closeErr           error
	maintenanceMu      sync.Mutex
	maintenanceStarted bool
	maintenanceAction  maintenance.Action
	maintenanceJob     string
	maintenanceLog     string
	// maintenanceAdmitted bounds the runner's startup grace in ProbeJob.
	maintenanceAdmitted time.Time

	updateCheckMu sync.Mutex
	// serviceStopLease is the Windows stop lease this process wrote, if any.
	// Unused on Unix, where SIGTERM is the cooperative stop.
	serviceStopMu    sync.Mutex
	serviceStopLease []byte
	// Commands should treat cancellation as a request to stop blocking work and
	// return promptly.
	Context context.Context
}

func New(buildInfo build.BuildInfo) *App { return &App{buildInfo: buildInfo} }

func (a *App) BuildInfo() build.BuildInfo { return a.buildInfo }

func (a *App) Init(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	a.DevMode = a.buildInfo.DevMode

	resolved, err := layout.New(a.buildInfo.Name, a.DevMode)
	if err != nil {
		return ctx, fmt.Errorf("resolve application layout: %w", err)
	}
	// Only retained directories may be prepared before the lifecycle lease.
	// Creating data here could race an uninstall and recreate what it removed.
	if err := resolved.EnsureRetained(); err != nil {
		return ctx, fmt.Errorf("prepare retained application layout: %w", err)
	}
	a.Layout = resolved

	migrator := cmd.Bool("migrate")
	guardCtx, guard, err := maintenance.OpenGuard(ctx, a.Layout, maintenance.GuardOptions{
		Version:  a.buildInfo.Version,
		Migrator: migrator,
		DevMode:  a.DevMode,
	})
	if err != nil {
		return guardCtx, fmt.Errorf("establish maintenance guard: %w", err)
	}
	ctx = guardCtx
	a.AddCleanup(guard.Close)
	if err := resolved.EnsureData(); err != nil {
		return ctx, fmt.Errorf("prepare application data layout: %w", err)
	}

	if a.DevMode {
		fmt.Printf("dev mode: using isolated storage dir %s\n", a.Layout.Storage)
	}
	if migrator {
		fmt.Printf("%s version %s\n", a.buildInfo.Name, a.buildInfo.Version)
	}
	if cmd.String("log") == "debug" || a.DevMode {
		fmt.Println("Storage:", a.Layout.Storage)
	}

	a.Log, err = xlog.New(a.Layout.Logs, "debug")
	if err != nil {
		return ctx, fmt.Errorf("initialize logger: %w", err)
	}
	a.Log.Infof("Starting %s %s (dev=%t), storage: %s",
		a.buildInfo.Name, a.buildInfo.Version, a.DevMode, a.Layout.Storage)

	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	migrationPolicy := database.RequireCurrentSchema
	if migrator {
		migrationPolicy = database.ApplyPendingMigrations
	} else if a.DevMode {
		migrationPolicy = database.InitializeFreshSchema
	}
	a.DB, err = database.New(a.Layout.DB, a.Log, a.buildInfo, migrationPolicy)
	if err != nil {
		return ctx, fmt.Errorf("initialize database: %w", err)
	}
	a.AddCleanup(func() error {
		var stateErr error
		if !migrator {
			if _, err := config.Update(a.DB, func(cfg *types.Configuration) error {
				cfg.LastShutdownVersion = a.buildInfo.Version
				return nil
			}); err != nil {
				stateErr = fmt.Errorf("record last shutdown version: %w", err)
			}
		}
		return errors.Join(stateErr, a.DB.Close())
	})
	a.Log.Debug("Database initialized")

	cfg, err := config.View(a.DB)
	if err != nil {
		return ctx, fmt.Errorf("view config: %w", err)
	}
	mmVer := strings.TrimPrefix(semver.MajorMinor(a.buildInfo.Version), "v")
	a.UserAgent = fmt.Sprintf("Mozilla/5.0 (compatible; %s/%s; +%s)",
		a.buildInfo.Name, mmVer, a.buildInfo.ContactURL)

	logLevel := x.Ternary(cmd.IsSet("log"), cmd.String("log"), cfg.LogLevel)
	if a.DevMode {
		logLevel = "debug"
	}
	if err := a.Log.SetLevel(logLevel); err != nil {
		return ctx, fmt.Errorf("set log level: %w", err)
	}
	a.Log.Debugf("Log level set to %q", logLevel)

	ctx = xlog.IntoContext(ctx, a.Log)
	a.Context = ctx
	a.ReleaseSource = &release.GenericReleaseSource{UserAgent: a.UserAgent}
	return ctx, nil
}

// Close runs registered cleanup functions in reverse order, logs their joined
// failure while the logger is still alive, and closes the logger last. The
// cached result makes repeated calls safe and observable.
func (a *App) Close() error {
	a.cleanupOnce.Do(func() {
		var joined error
		for _, v := range slices.Backward(a.cleanup) {
			if err := v(); err != nil {
				joined = errors.Join(joined, err)
			}
		}
		if joined != nil && a.Log != nil {
			a.Log.Errorf("application cleanup failed: %v", joined)
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

func (a *App) AcquireServiceLock() (*xsyscall.Lock, error) {
	return maintenance.AcquireServiceLock(a.Layout)
}

var ErrServiceAlreadyRunning = maintenance.ErrServiceAlreadyRunning
