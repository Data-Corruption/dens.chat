package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/maintenance"
	"github.com/Data-Corruption/dens.chat/internal/platform/database/config"
	"github.com/Data-Corruption/dens.chat/internal/types"
	"github.com/Data-Corruption/dens.chat/pkg/xterm/prompt"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func updateCommand(a *app.App) *cli.Command {
	usage := "check for updates"
	usage = "check for and apply updates"
	return &cli.Command{
		Name:  "update",
		Usage: usage,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "notify",
				Usage: "show update notices (--notify=false to hide them)",
				Value: true,
			},
			&cli.BoolFlag{Name: "background", Value: true, Usage: "check periodically (--background=false to disable)"},
			&cli.BoolFlag{
				Name:    "yes",
				Aliases: []string{"y"},
				Usage:   "apply an available update without prompting",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			settingsChanged := cmd.IsSet("notify") || cmd.IsSet("background")
			if settingsChanged {
				if _, err := config.Update(a.DB, func(cfg *types.Configuration) error {
					if cmd.IsSet("notify") {
						cfg.UpdateNotifications = cmd.Bool("notify")
					}
					if cmd.IsSet("background") {
						cfg.BackgroundUpdateChecks = cmd.Bool("background")
					}
					return nil
				}); err != nil {
					return fmt.Errorf("save update preferences: %w", err)
				}
				fmt.Println("Update preferences saved.")
				return nil
			}

			checkForUpdate := a.CheckForUpdate

			updateAvailable, err := checkForUpdate(ctx)
			if err != nil {
				if errors.Is(err, app.ErrUpdatesDisabled) {
					fmt.Printf("This installation does not manage updates. %s\n", app.UpdateGuidance)
					return nil
				}
				return fmt.Errorf("check for updates: %w", err)
			}
			if !updateAvailable {
				fmt.Println("Already running the latest version.")
				return nil
			}

			applyHandled := false
			applyHandled = true
			if err := applyAvailableUpdate(
				cmd.Bool("yes"),
				term.IsTerminal(int(os.Stdin.Fd())),
				prompt.YesNo,
				func() (string, error) {
					return a.StartMaintenance(ctx, maintenance.ActionUpdate)
				},
				os.Stdout,
			); err != nil {
				return err
			}
			if applyHandled {
				return nil
			}

			fmt.Printf("An update is available. %s\n", app.UpdateGuidance)
			return nil
		},
	}
}

func applyAvailableUpdate(
	yes bool,
	interactive bool,
	ask func(string) (bool, error),
	schedule func() (string, error),
	output io.Writer,
) error {
	if !yes {
		if !interactive {
			return fmt.Errorf("update confirmation requires interactive input; use --yes")
		}
		confirmed, err := ask("Apply the available update?")
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("update confirmation requires interactive input; use --yes")
			}
			return fmt.Errorf("read update confirmation: %w", err)
		}
		if !confirmed {
			fmt.Fprintln(output, "Update declined.")
			return nil
		}
	}

	logPath, err := schedule()
	if err != nil {
		if errors.Is(err, app.ErrUpdatesDisabled) {
			fmt.Fprintf(output, "The update cannot be applied by this installation. %s\n", app.UpdateGuidance)
			return nil
		}
		return fmt.Errorf("start self-update: %w", err)
	}
	fmt.Fprintf(output, "Update accepted and will now start in the background.\nIt may take a few moments.\nTo view the update logs see: %s\n", logPath)
	return nil
}
