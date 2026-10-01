package commands

import (
	"context"
	"os"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/media/ffmpeg"

	"github.com/urfave/cli/v3"
)

// mediaWorkerCommand is the media module's worker process, which the service
// starts for each job and talks to over its standard streams (see
// internal/media/ffmpeg). It opens nothing of the instance's.
func mediaWorkerCommand(build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:   ffmpeg.WorkerCommand,
		Hidden: true,
		Usage:  "run a media job for the service (the service does this)",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			os.Exit(ffmpeg.Work(os.Stdin, os.Stdout))
			return nil
		},
	}
}
