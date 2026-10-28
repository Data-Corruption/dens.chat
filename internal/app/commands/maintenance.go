package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/install"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xterm/prompt"

	"github.com/urfave/cli/v3"
)

func installCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "install this binary as an instance, or update or reconfigure one (the installer runs this)",
		Description: "Installs or updates the given instance with this binary, and moves every other installed\n" +
			"instance to the same version. Run it as root or Administrator. The installer scripts\n" +
			"download and verify a release, then run this.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "user", Usage: "the desktop account allowed to pair (default: the account that elevated)"},
			// v1.0's den role, refused rather than dropped, so nobody
			// takes it for one that did something (M9.1).
			&cli.BoolFlag{Name: "den", Hidden: true},
			&cli.IntFlag{Name: "client-port", Usage: "loopback port of the chat page"},
			&cli.IntFlag{Name: "den-port", Usage: "loopback port Caddy forwards the den to, while hosting is on"},
			&cli.IntFlag{Name: "media-udp-port", Usage: "UDP port for voice and screen share"},
			&cli.IntFlag{Name: "media-tcp-port", Usage: "TCP fallback port for voice and screen share"},
			&cli.StringFlag{Name: "release-url", Usage: "where updates come from (the installer passes its own)"},
			&cli.StringFlag{Name: "cosign", Usage: "a verified cosign binary to keep for dens update"},
			&cli.BoolFlag{Name: "dry-run", Usage: "print what would change, and change nothing"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if err := refusePackaged(bi, "Dens runs here as a Flatpak, which Flatpak installs and updates; dens install installs Dens where its installer runs"); err != nil {
				return err
			}
			if cmd.IsSet("den") {
				return errors.New("--den is gone: every install can host a den, and hosting is turned on and off in Dens's settings, in the System tab")
			}
			opts := install.Options{
				Instance:     cmd.String("instance"),
				User:         cmd.String("user"),
				ClientPort:   cmd.Int("client-port"),
				DenPort:      cmd.Int("den-port"),
				MediaUDPPort: cmd.Int("media-udp-port"),
				MediaTCPPort: cmd.Int("media-tcp-port"),
				ReleaseURL:   cmd.String("release-url"),
				Cosign:       cmd.String("cosign"),
				DryRun:       cmd.Bool("dry-run"),
			}
			return install.Install(ctx, install.NewSystem(bi.Name), bi, opts)
		},
	}
}

func updateCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "update Dens to the latest release (as root or Administrator)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "release-url", Usage: "update from this release source for this run"},
			&cli.BoolFlag{Name: "skip-verify", Usage: "don't verify the installer or the release, for unsigned test releases (a release refuses it)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if err := refusePackaged(bi, "Dens runs here as a Flatpak: update it in your software center, or with: flatpak update "+layout.DesktopID); err != nil {
				return err
			}
			return install.Update(ctx, install.NewSystem(bi.Name), bi, install.Options{
				Instance:   cmd.String("instance"),
				ReleaseURL: cmd.String("release-url"),
				SkipVerify: cmd.Bool("skip-verify"),
			})
		},
	}
}

func uninstallCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "uninstall",
		Usage: "uninstall an instance and delete its data (as root or Administrator)",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "don't ask for confirmation"},
			&cli.BoolFlag{Name: "dry-run", Usage: "print what would be removed, and remove nothing"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if err := refusePackaged(bi, "Dens runs here as a Flatpak: remove it in your software center, or with: flatpak uninstall "+layout.DesktopID+", which keeps its data unless you add --delete-data"); err != nil {
				return err
			}
			opts := install.Options{Instance: cmd.String("instance"), DryRun: cmd.Bool("dry-run")}
			if !cmd.Bool("yes") {
				opts.Confirm = func(holds string) error { return confirmUninstall(bi, opts.Instance, holds) }
			}
			return install.Uninstall(ctx, install.NewSystem(bi.Name), bi, opts)
		},
	}
}

// deletePhrase is what an uninstall asks to be typed (M10): more than the
// y a reflex gives.
const deletePhrase = "perm delete data"

// confirmUninstall says what an uninstall deletes, with what the instance
// holds where it could be read, points to a backup first, and to stopping
// the service for someone who only wants Dens off, and asks for the
// phrase.
func confirmUninstall(bi build.BuildInfo, name, holds string) error {
	fmt.Printf("This deletes instance %s and all of its data, including its keys", name)
	if holds != "" {
		fmt.Printf(": it %s", holds)
	}
	fmt.Println(".")
	fmt.Printf("Make a backup first if you may want it back: %s\n", commandIn(bi, name, "backup"))
	fmt.Printf("To stop Dens only until the computer restarts, deleting nothing: %s\n", host.AdminCommand(commandIn(bi, name, "service stop")))
	return confirmPhrase("uninstall instance " + name)
}

// askLine is prompt.Line; tests replace it.
var askLine = prompt.Line

// confirmPhrase asks for deletePhrase before what doing names; any other
// answer cancels.
func confirmPhrase(doing string) error {
	answer, err := askLine(fmt.Sprintf("Type %q to %s: ", deletePhrase, doing))
	if err != nil {
		return fmt.Errorf("read confirmation (use --yes in scripts): %w", err)
	}
	if answer != deletePhrase {
		return errCancelled
	}
	return nil
}

func restoreCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:      "restore",
		Usage:     "replace an instance's data with a backup (installed: as root or Administrator)",
		ArgsUsage: "BACKUP",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "password-stdin", Usage: "read the backup's local password from the first line of standard input"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "don't ask for confirmation"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return errors.New("usage: restore BACKUP")
			}
			name := cmd.String("instance")
			if !cmd.Bool("yes") {
				fmt.Printf("This replaces all data of instance %s with the backup.\n", name)
				if err := confirm("Restore into " + name + "?"); err != nil {
					return err
				}
			}
			password, err := readPassword(cmd.Bool("password-stdin"), "The backup's local password")
			if err != nil {
				return err
			}
			opts := install.Options{Instance: name}
			if form := app.FormOf(bi); form.Session() {
				l, err := layout.New(bi.Name, name, form)
				if err != nil {
					return err
				}
				return install.RestoreSession(ctx, bi, l, cmd.Args().First(), password, func() error { return quitService(bi, name) }, opts)
			}
			return install.Restore(ctx, install.NewSystem(bi.Name), bi, opts, cmd.Args().First(), password)
		},
	}
}

// refusePackaged refuses a maintenance command in the Flatpak, which
// Flatpak installs, updates and removes (M9.3), saying why, and in the
// Android app, which Android does (M12).
func refusePackaged(bi build.BuildInfo, flatpakWhy string) error {
	switch app.FormOf(bi) {
	case layout.Flatpak:
		return errors.New(flatpakWhy)
	case layout.Android:
		return errors.New("Dens runs here as the Android app, which Android installs, updates and removes")
	}
	return nil
}

// askYesNo is prompt.YesNo; tests replace it.
var askYesNo = prompt.YesNo

func confirm(question string) error {
	yes, err := askYesNo(question)
	if err != nil {
		return fmt.Errorf("read confirmation (use --yes in scripts): %w", err)
	}
	if !yes {
		return errCancelled
	}
	return nil
}

var errCancelled = errors.New("cancelled")
