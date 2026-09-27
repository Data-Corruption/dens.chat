// Package commands provides the dens CLI commands.
//
// Only the service opens an instance's storage. The desktop user's commands
// talk to the service over the control endpoint, and the elevated
// maintenance commands run installer transactions.
package commands

import (
	"github.com/Data-Corruption/dens.chat/internal/build"

	"github.com/urfave/cli/v3"
)

type constructor func(bi build.BuildInfo) *cli.Command

var constructors = []constructor{
	openCommand,
	statusCommand,
	backupCommand,
	serviceCommand,
	installCommand,
	updateCommand,
	uninstallCommand,
	restoreCommand,
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

// InstanceFlag selects the instance every command acts on.
var InstanceFlag = &cli.StringFlag{
	Name:    "instance",
	Aliases: []string{"i"},
	Value:   "main",
	Usage:   "the instance to use",
}
