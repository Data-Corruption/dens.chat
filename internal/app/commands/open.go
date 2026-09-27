package commands

import (
	"context"
	"fmt"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"

	"github.com/urfave/cli/v3"
)

func openCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "open",
		Usage: "open Dens in your browser, pairing it if needed",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "print",
				Usage: "print the one-time pairing link instead of opening a browser",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, l, err := controlClient(bi, cmd.String("instance"))
			if err != nil {
				return err
			}
			var pair control.Pair
			if err := client.Call(control.Request{Op: control.OpPair}, &pair); err != nil {
				return explainCallError(bi, l, err)
			}
			if cmd.Bool("print") {
				fmt.Println(pair.URL)
				return nil
			}
			if err := host.OpenURL(pair.URL); err != nil {
				fmt.Println("Couldn't open a browser. Open this link within two minutes; it works once:")
				fmt.Println(pair.URL)
				return nil
			}
			fmt.Println("Opened Dens in your browser.")
			return nil
		},
	}
}

func statusCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "show the running service's status",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, l, err := controlClient(bi, cmd.String("instance"))
			if err != nil {
				return err
			}
			var status control.Status
			if err := client.Call(control.Request{Op: control.OpStatus}, &status); err != nil {
				return explainCallError(bi, l, err)
			}
			fmt.Printf("instance:  %s\n", status.Instance)
			fmt.Printf("version:   %s\n", status.Version)
			fmt.Printf("client:    %s\n", status.ClientURL)
			fmt.Printf("password:  %s\n", setOrNot(status.PasswordSet))
			fmt.Printf("den:       %s\n", enabledOrNot(status.DenEnabled))
			if status.UpdateAvailable != "" {
				fmt.Printf("update:    %s is available\n", status.UpdateAvailable)
			}
			return nil
		},
	}
}

func setOrNot(v bool) string {
	if v {
		return "set"
	}
	return "not set (set one in the browser: dens open)"
}

func enabledOrNot(v bool) string {
	if v {
		return "hosting a den"
	}
	return "not hosting a den"
}
