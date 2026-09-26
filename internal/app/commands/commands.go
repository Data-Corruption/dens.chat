// Package commands provides CLI command definitions for the application.
package commands

import (
	"github.com/Data-Corruption/dens.chat/internal/app"

	"github.com/urfave/cli/v3"
)

type constructor func(a *app.App) *cli.Command

var constructors = []constructor{
	updateCommand,
	uninstallCommand,
	configCommand,
	usersCommand,
	hashCommand,
	serviceCommand,
}

// All builds the commands enabled for this application.
func All(a *app.App) []*cli.Command {
	commands := make([]*cli.Command, 0, len(constructors))
	for _, construct := range constructors {
		if command := construct(a); command != nil {
			commands = append(commands, command)
		}
	}
	return commands
}
