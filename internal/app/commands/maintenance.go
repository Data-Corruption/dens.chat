package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/install"
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
			&cli.BoolFlag{Name: "den", Usage: "host a den with this instance (--den=false to stop hosting)"},
			&cli.IntFlag{Name: "client-port", Usage: "loopback port of the chat page"},
			&cli.IntFlag{Name: "den-port", Usage: "loopback port Caddy forwards the den to"},
			&cli.IntFlag{Name: "media-udp-port", Usage: "UDP port for voice and screen share"},
			&cli.IntFlag{Name: "media-tcp-port", Usage: "TCP fallback port for voice and screen share"},
			&cli.StringFlag{Name: "release-url", Usage: "where updates come from (the installer passes its own)"},
			&cli.StringFlag{Name: "cosign", Usage: "a verified cosign binary to keep for dens update"},
			&cli.BoolFlag{Name: "dry-run", Usage: "print what would change, and change nothing"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
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
			if cmd.IsSet("den") {
				den := cmd.Bool("den")
				opts.Den = &den
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
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return install.Update(ctx, install.NewSystem(bi.Name), bi, install.Options{
				Instance:   cmd.String("instance"),
				ReleaseURL: cmd.String("release-url"),
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
			name := cmd.String("instance")
			if !cmd.Bool("yes") && !cmd.Bool("dry-run") {
				fmt.Printf("This deletes instance %s and all of its data, including its keys.\n", name)
				fmt.Printf("Make a backup first if you may want it back: %s backup\n", bi.Name)
				if err := confirm("Uninstall " + name + "?"); err != nil {
					return err
				}
			}
			return install.Uninstall(ctx, install.NewSystem(bi.Name), bi, install.Options{
				Instance: name,
				DryRun:   cmd.Bool("dry-run"),
			})
		},
	}
}

func restoreCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:      "restore",
		Usage:     "replace an instance's data with a backup (as root or Administrator)",
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
			password, err := readPassword(cmd.Bool("password-stdin"), "The backup's local password: ")
			if err != nil {
				return err
			}
			return install.Restore(ctx, install.NewSystem(bi.Name), bi, install.Options{Instance: name}, cmd.Args().First(), password)
		},
	}
}

func confirm(question string) error {
	yes, err := prompt.YesNo(question)
	if err != nil {
		return fmt.Errorf("read confirmation (use --yes in scripts): %w", err)
	}
	if !yes {
		return errors.New("cancelled")
	}
	return nil
}
