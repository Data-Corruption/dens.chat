package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Data-Corruption/dens.chat/internal/app/commands"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"

	"github.com/urfave/cli/v3"
)

func main() {
	os.Exit(runMain())
}

func notifyProcessContext(parent context.Context) (context.Context, context.CancelFunc) {
	// On Windows, the Go runtime installs SetConsoleCtrlHandler. It maps delivered
	// CTRL_C_EVENT and CTRL_BREAK_EVENT to os.Interrupt, and maps
	// CTRL_CLOSE_EVENT, CTRL_LOGOFF_EVENT, and CTRL_SHUTDOWN_EVENT to SIGTERM.
	// Under the SCM, stop requests arrive through the service handler instead.
	ctx, stopSignals := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	// After the first signal, restore default handling so a second one ends
	// a process that isn't shutting down.
	go func() {
		<-ctx.Done()
		stopSignals()
	}()
	return ctx, stopSignals
}

func runMain() int {
	ctx, stopSignals := notifyProcessContext(context.Background())
	defer stopSignals()

	bi := build.Info()
	root := &cli.Command{
		Name:    bi.Name,
		Version: bi.Version,
		Usage:   "Self-hosted chat with text, voice and screen share.",
		Flags: []cli.Flag{
			commands.InstanceFlag,
			&cli.BoolFlag{
				Name:   "build-vars",
				Hidden: true,
				Usage:  "print build variables and exit",
			},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			if cmd.Bool("build-vars") {
				fmt.Println(bi.PrintJSON())
				os.Exit(0)
			}
			return ctx, nil
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Printf("%s version %s\n", bi.Name, bi.Version)
			fmt.Printf("Use '%s open' to open Dens, or '%s help' for all commands.\n", bi.Name, bi.Name)
			return nil
		},
		Commands: commands.All(bi),
	}

	if err := root.Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if errors.Is(err, host.ErrRefused) {
			return host.ExitRefused
		}
		return 1
	}
	return 0
}
