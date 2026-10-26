package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"

	"github.com/urfave/cli/v3"
)

// logsCommand saves the newest logs into a file, as the settings' Logging
// card downloads them, or sets how much they say, as the card's level does
// (M11), so a server needs the page for neither.
func logsCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "logs",
		Usage: "save the newest logs into a file, to send with a problem, or set how much they say",
		Description: "The logs say what Dens did and when. They hold no messages, keys or addresses, but they do\n" +
			"name the dens this instance joined or hosts, with their IDs. At debug they say the most, calls\n" +
			"in detail: set it before a problem happens again, then save the logs once it has.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "level",
				Usage: "set how much the logs say from now on (" + xlog.ValidLevels + "), instead of saving them",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "file to write (default: a dated name in the current directory)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, l, err := controlClient(bi, cmd.String("instance"))
			if err != nil {
				return err
			}
			if level := cmd.String("level"); level != "" {
				var kept control.LogLevel
				if err := client.Call(control.Request{Op: control.OpLogs, Level: level}, &kept); err != nil {
					return explainCallError(bi, l, err)
				}
				fmt.Printf("The logs say what happens at %s from now on.\n", kept.Level)
				return nil
			}
			output := cmd.String("output")
			dir, err := outputDir(l, output)
			if err != nil {
				return err
			}
			path, err := writeBackup(client, control.Request{Op: control.OpLogs}, dir, output, "log file")
			if err != nil {
				return explainCallError(bi, l, err)
			}
			fmt.Printf("Logs written to %s\n", path)
			fmt.Println("They hold no messages, keys or addresses, but they do name your dens, with their IDs.")
			return nil
		},
	}
}

// outputDir is the folder a command writes a file into: output's, or the
// current one, or in the Flatpak the app's own folder, as the sandbox
// writes only there and the member finds it at the same path.
func outputDir(l layout.Layout, output string) (string, error) {
	if output != "" {
		if _, err := os.Lstat(output); err == nil {
			return "", fmt.Errorf("%s already exists", output)
		}
		return filepath.Dir(output), nil
	}
	if l.Form == layout.Flatpak {
		return filepath.Dir(os.Getenv("XDG_DATA_HOME")), nil
	}
	return ".", nil
}
