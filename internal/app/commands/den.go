package commands

import (
	"context"
	"fmt"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"

	"github.com/urfave/cli/v3"
)

func denCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "den",
		Usage: "manage the den this instance hosts",
		Commands: []*cli.Command{
			{
				Name:  "owner-invite",
				Usage: "make a one-time invite that hands the den to whoever redeems it, protected by your local password",
				Description: "Whoever redeems the invite becomes the den's owner, and the owner now becomes a moderator.\n" +
					"It hands the den to someone else, and it's the way back into a den whose owner account\n" +
					"you're locked out of: join with it under a new username.",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "password-stdin",
						Usage: "read the local password from the first line of standard input",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client, l, err := controlClient(bi, cmd.String("instance"))
					if err != nil {
						return err
					}
					password, err := readPassword(cmd.Bool("password-stdin"), "Local password: ")
					if err != nil {
						return err
					}
					var result control.OwnerInvite
					if err := client.Call(control.Request{Op: control.OpOwnerInvite, Password: password}, &result); err != nil {
						return explainCallError(bi, l, err)
					}
					fmt.Printf("An owner invite for %s:\n\n%s\n\n", result.Den, result.Invite)
					fmt.Println("Whoever pastes it into the Join box of their Dens page becomes the den's owner,")
					fmt.Println("and the owner now becomes a moderator. It works once, within the hour.")
					return nil
				},
			},
		},
	}
}
