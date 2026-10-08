package commands

import (
	"context"
	"fmt"

	"github.com/Data-Corruption/dens.chat/internal/build"

	"github.com/urfave/cli/v3"
)

// licensesCommand prints the licenses of the third-party software in the
// binary, as their licenses ask copies of it to carry.
func licensesCommand(build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "licenses",
		Usage: "print the licenses of the third-party software Dens includes",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			_, err := fmt.Print(build.Notices())
			return err
		},
	}
}
