// Package commands provides the dens CLI commands.
//
// Only the service opens an instance's storage. The desktop user's commands
// talk to the service over the control endpoint, and the elevated
// maintenance commands run installer transactions.
package commands

import (
	"context"
	"fmt"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/layout"

	"github.com/urfave/cli/v3"
)

type constructor func(bi build.BuildInfo) *cli.Command

var constructors = []constructor{
	openCommand,
	launchCommand,
	statusCommand,
	passwordCommand,
	backupCommand,
	logsCommand,
	denCommand,
	serviceCommand,
	installCommand,
	updateCommand,
	uninstallCommand,
	restoreCommand,
	licensesCommand,
	mediaWorkerCommand,
	shortcutsCommand,
}

// All builds the commands for this build.
func All(bi build.BuildInfo) []*cli.Command {
	commands := make([]*cli.Command, 0, len(constructors))
	for _, construct := range constructors {
		if command := construct(bi); command != nil {
			commands = append(commands, command)
		}
	}
	return commands
}

// Root is what dens does with no command. In the Flatpak, which flatpak run
// chat.dens.Dens runs so, it opens Dens, as the desktop file's launch does
// (M9.3); elsewhere it says how to.
func Root(bi build.BuildInfo) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		if app.FormOf(bi) == layout.Flatpak {
			return launch(ctx, bi, cmd.String("instance"))
		}
		fmt.Printf("%s version %s\n", bi.Name, bi.Version)
		fmt.Printf("Use '%s open' to open Dens, or '%s help' for all commands.\n", bi.Name, bi.Name)
		return nil
	}
}

// InstanceFlag selects the instance every command acts on.
var InstanceFlag = &cli.StringFlag{
	Name:    "instance",
	Aliases: []string{"i"},
	Value:   "main",
	Usage:   "the instance to use",
}
