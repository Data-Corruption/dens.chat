package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"

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
				Flags: []cli.Flag{passwordStdinFlag()},
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
			{
				Name:  "hosting",
				Usage: "turn hosting the den on or off, as the switch in Dens's settings does",
				Commands: []*cli.Command{
					{
						Name:  "on",
						Usage: "host the den, which opens its ports to the internet, protected by your local password",
						Flags: []cli.Flag{passwordStdinFlag(), yesFlag()},
						Action: func(ctx context.Context, cmd *cli.Command) error {
							return hostingOn(bi, cmd)
						},
					},
					{
						Name:  "off",
						Usage: "stop hosting the den, so members can't reach it until hosting is back on; everything in it stays",
						Flags: []cli.Flag{yesFlag()},
						Action: func(ctx context.Context, cmd *cli.Command) error {
							client, l, err := controlClient(bi, cmd.String("instance"))
							if err != nil {
								return err
							}
							if !cmd.Bool("yes") {
								fmt.Println("Members can't reach the den until hosting is back on. Everything in it stays.")
								if err := confirm("Turn hosting off?"); err != nil {
									return err
								}
							}
							var result control.Hosting
							if err := client.Call(control.Request{Op: control.OpHosting, On: false}, &result); err != nil {
								return explainCallError(bi, l, err)
							}
							fmt.Println("Hosting is off.")
							return nil
						},
					},
				},
			},
			{
				Name:  "export",
				Usage: "write the den's export, which moves it to another computer, protected by your local password",
				Description: "The export only holds the den: its members, messages and files, and its identity. None of\n" +
					"this computer's client data goes with it: the dens it joined, its keys there and its settings\n" +
					"stay here. Hosting must be off, so nothing reaches the den once it's exported.\n\n" +
					"On the other computer, restore it with dens restore, as root or Administrator, and turn\n" +
					"hosting on there with dens den hosting on. Once it serves the den, remove the den from this\n" +
					"computer with dens den remove.",
				Flags: archiveFlags(),
				Action: func(ctx context.Context, cmd *cli.Command) error {
					path, err := saveArchive(bi, cmd, control.OpExport, "den's export")
					if err != nil {
						return err
					}
					fmt.Printf("The den's export is written to %s\n", path)
					fmt.Println("It only holds the den, not any of your client data. It is encrypted with your local")
					fmt.Println("password; keep both safe.")
					fmt.Println()
					fmt.Println("On the other computer, restore it with dens restore, as root or Administrator, then turn")
					fmt.Printf("hosting on there with: %s den hosting on\n", bi.Name)
					fmt.Printf("Once that computer serves the den, remove it from this one with: %s\n", instanceCommand(bi, cmd, "den remove"))
					return nil
				},
			},
			{
				Name:  "remove",
				Usage: "delete this computer's copy of the den once another computer serves it, protected by your local password",
				Description: "Once the den's export is restored on another computer, and hosted there, this deletes the\n" +
					"copy on this one: its members, messages and files. Your account in the den stays, and Dens\n" +
					"reaches the den at its address, as any member's does.\n\n" +
					"It checks first that hosting is off here, that the den was exported since hosting was last\n" +
					"on, and that the den answers at its address from this computer as itself.",
				Flags: []cli.Flag{passwordStdinFlag(), yesFlag()},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					client, l, err := controlClient(bi, cmd.String("instance"))
					if err != nil {
						return err
					}
					var status control.Status
					if err := client.Call(control.Request{Op: control.OpStatus}, &status); err != nil {
						return explainCallError(bi, l, err)
					}
					if status.Den == "" {
						return errors.New("this instance holds no den")
					}
					if !cmd.Bool("yes") {
						if err := refuseConfirmFromStdin(cmd); err != nil {
							return err
						}
						fmt.Printf("This deletes this computer's copy of %s: its members, messages and files.\n", status.Den)
						fmt.Println("Your account in the den stays, and Dens reaches the den at its address.")
						if err := confirm("Remove the den from this computer?"); err != nil {
							return err
						}
					}
					password, err := readPassword(cmd.Bool("password-stdin"), "Local password: ")
					if err != nil {
						return err
					}
					// The den's address gets as long to answer as a den's
					// requests do, before the reply.
					client.Timeout = 2 * time.Minute
					var removed control.RemovedDen
					if err := client.Call(control.Request{Op: control.OpRemoveDen, Password: password}, &removed); err != nil {
						return explainCallError(bi, l, err)
					}
					fmt.Printf("Removed %s from this computer. Dens reaches it at %s now, as any member's does.\n", removed.Den, removed.URL)
					return nil
				},
			},
		},
	}
}

// hostingOn turns hosting on, after a confirmation where the den was
// exported, since another computer may host it by now (M10), and says
// what it serves, and where Caddy and the router send it.
func hostingOn(bi build.BuildInfo, cmd *cli.Command) error {
	client, l, err := controlClient(bi, cmd.String("instance"))
	if err != nil {
		return err
	}
	var status control.Status
	if err := client.Call(control.Request{Op: control.OpStatus}, &status); err != nil {
		return explainCallError(bi, l, err)
	}
	if !status.DenExported.IsZero() && !status.DenEnabled && !cmd.Bool("yes") {
		if err := refuseConfirmFromStdin(cmd); err != nil {
			return err
		}
		fmt.Printf("You exported %s on %s. Another computer may host it by now, and members\n",
			status.Den, status.DenExported.Local().Format(time.RFC1123))
		fmt.Println("would then reach whichever one its address points at.")
		if err := confirm("Turn hosting on here anyway?"); err != nil {
			return err
		}
	}
	password, err := readPassword(cmd.Bool("password-stdin"), "Local password: ")
	if err != nil {
		return err
	}
	var h control.Hosting
	if err := client.Call(control.Request{Op: control.OpHosting, On: true, Password: password}, &h); err != nil {
		return explainCallError(bi, l, err)
	}
	switch {
	case h.Problem != "":
		fmt.Printf("Hosting is on, but the den doesn't serve: %s\n", h.Problem)
		return nil
	case h.Den != "":
		fmt.Printf("Hosting is on, and %s serves at %s.\n", h.Den, h.URL)
	default:
		fmt.Println("Hosting is on. No den is here yet: create one in the browser, or restore a den's export.")
	}
	fmt.Printf("Caddy forwards the den's address to 127.0.0.1:%d, and the router forwards\n", h.DenPort)
	fmt.Printf("port %d (UDP) and port %d (TCP) to this computer, for calls.\n", h.MediaUDPPort, h.MediaTCPPort)
	return nil
}

func passwordStdinFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:  "password-stdin",
		Usage: "read the local password from the first line of standard input",
	}
}

func yesFlag() cli.Flag {
	return &cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "don't ask for confirmation"}
}

// refuseConfirmFromStdin refuses to ask a question on standard input that
// --password-stdin reads the password from.
func refuseConfirmFromStdin(cmd *cli.Command) error {
	if cmd.Bool("password-stdin") {
		return errors.New("--password-stdin reads standard input, which this asks a question on; add --yes to answer it")
	}
	return nil
}

// instanceCommand is a dens command for the instance cmd names, as the
// desktop user types it.
func instanceCommand(bi build.BuildInfo, cmd *cli.Command, args string) string {
	command := bi.Name + " " + args
	if name := cmd.String("instance"); name != "" && name != layout.DefaultInstance {
		command += " --instance " + name
	}
	return command
}
