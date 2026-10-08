package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/internal/platform/http/guard"
	"github.com/Data-Corruption/dens.chat/internal/service"
	"github.com/Data-Corruption/dens.chat/internal/ui"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"github.com/urfave/cli/v3"
)

const serviceStopTimeout = 20 * time.Second

func serviceCommand(bi build.BuildInfo) *cli.Command {
	control := func(name, usage string, run func(l layout.Layout) error) *cli.Command {
		return &cli.Command{
			Name:  name,
			Usage: usage,
			Action: func(ctx context.Context, cmd *cli.Command) error {
				l, err := layout.New(bi.Name, cmd.String("instance"), bi.DevMode)
				if err != nil {
					return err
				}
				if l.Dev {
					return fmt.Errorf("development instances have no service manager; run %s service run in a terminal", bi.Name)
				}
				return run(l)
			},
		}
	}
	return &cli.Command{
		Name:  "service",
		Usage: "control the background service",
		Commands: []*cli.Command{
			{
				Name:   "run",
				Hidden: true,
				Usage:  "run the service in the foreground (the service manager does this)",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "log", Usage: "override the log level for this run (" + xlog.ValidLevels + ")"},
					&cli.IntFlag{Name: "port", Usage: "client port of a new development instance"},
					&cli.IntFlag{Name: "den-port", Usage: "host a den on this port (development instances only)"},
					&cli.IntFlag{Name: "media-udp-port", Usage: "UDP port for the den's calls (development instances only)"},
					&cli.IntFlag{Name: "media-tcp-port", Usage: "TCP fallback port for the den's calls (development instances only)"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runService(ctx, bi, cmd)
				},
			},
			control("start", "start the service", func(l layout.Layout) error {
				if err := host.ServiceStart(l.ServiceName); err != nil {
					return err
				}
				fmt.Println("Started", l.ServiceName)
				return nil
			}),
			control("stop", "stop the service", func(l layout.Layout) error {
				if err := host.ServiceStop(l.ServiceName, serviceStopTimeout); err != nil {
					return err
				}
				fmt.Println("Stopped", l.ServiceName)
				return nil
			}),
			control("restart", "restart the service", func(l layout.Layout) error {
				if err := host.ServiceStop(l.ServiceName, serviceStopTimeout); err != nil {
					return err
				}
				if err := host.ServiceStart(l.ServiceName); err != nil {
					return err
				}
				fmt.Println("Restarted", l.ServiceName)
				return nil
			}),
			control("status", "show whether the service is running", func(l layout.Layout) error {
				state, err := host.ServiceStatus(l.ServiceName)
				if errors.Is(err, host.ErrServiceNotInstalled) {
					fmt.Printf("%s is not installed\n", l.ServiceName)
					return nil
				}
				if err != nil {
					return err
				}
				if state.Running {
					fmt.Printf("%s is running (process %d)\n", l.ServiceName, state.PID)
				} else {
					fmt.Printf("%s is stopped (last exit code %d)\n", l.ServiceName, state.ExitCode)
				}
				return nil
			}),
		},
	}
}

// runService hosts the service under the platform's service manager first,
// so that even a refused start is reported to it properly, then opens the
// installation and runs.
func runService(ctx context.Context, bi build.BuildInfo, cmd *cli.Command) error {
	l, err := layout.New(bi.Name, cmd.String("instance"), bi.DevMode)
	if err != nil {
		return fmt.Errorf("%w: %w", host.ErrRefused, err)
	}
	return host.RunService(ctx, l.ServiceName, func(ctx context.Context, ready func()) error {
		a := app.New(bi)
		defer a.Close()
		err := serve(ctx, a, bi, l, cmd, ready)
		// A Windows service has no stderr, so its own log is where an
		// operator finds out why it stopped.
		if err != nil && a.Log != nil {
			if errors.Is(err, host.ErrRefused) {
				a.Log.Errorf("%v", err)
			} else {
				a.Log.Errorf("Stopped: %v", err)
			}
		}
		return err
	})
}

func serve(ctx context.Context, a *app.App, bi build.BuildInfo, l layout.Layout, cmd *cli.Command, ready func()) error {
	if err := a.Open(app.OpenOptions{
		Instance:      l.Instance,
		LogLevel:      cmd.String("log"),
		DevClientPort: cmd.Int("port"),
		DevDenPort:    cmd.Int("den-port"),
		DevMediaPorts: [2]int{cmd.Int("media-udp-port"), cmd.Int("media-tcp-port")},
	}); err != nil {
		return err
	}
	pages, err := ui.New()
	if err != nil {
		return fmt.Errorf("%w: load pages: %w", host.ErrRefused, err)
	}
	a.UI = pages
	if l.Dev {
		fmt.Printf("Development instance %s: %s (pair with: %s open)\n", l.Instance, guard.PageOrigin(a.Instance.ClientPort), bi.Name)
	}
	return service.Run(ctx, a, ready)
}
