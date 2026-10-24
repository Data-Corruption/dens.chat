package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"

	"github.com/urfave/cli/v3"
)

// passwordCommand sets the local password, or changes it, as the page does
// (M10), so a server never needs the page.
func passwordCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "password",
		Usage: "set the local password, or change it given the current one",
		Description: "The local password protects this computer's Dens: pairing a browser takes it, and so do\n" +
			"backups, hosting and the den's commands. Changing it changes nothing else: a backup made\n" +
			"before still opens with the password it was made under.",
		Flags: []cli.Flag{&cli.BoolFlag{
			Name:  "password-stdin",
			Usage: "read the new password from standard input, on the line after the current one where one is set",
		}},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, l, err := controlClient(bi, cmd.String("instance"))
			if err != nil {
				return err
			}
			var status control.Status
			if err := client.Call(control.Request{Op: control.OpStatus}, &status); err != nil {
				return explainCallError(bi, l, err)
			}
			labels := []string{"New local password", "The new one again"}
			if cmd.Bool("password-stdin") {
				labels = labels[:1]
			}
			if status.PasswordSet {
				labels = append([]string{"Current local password"}, labels...)
			}
			passwords, err := readPasswords(cmd.Bool("password-stdin"), labels...)
			if err != nil {
				return err
			}
			var current string
			if status.PasswordSet {
				current, passwords = passwords[0], passwords[1:]
			}
			if len(passwords) == 2 && passwords[0] != passwords[1] {
				return errors.New("the new passwords don't match")
			}
			var result control.PasswordChanged
			if err := client.Call(control.Request{Op: control.OpPassword, Password: current, NewPassword: passwords[0]}, &result); err != nil {
				return explainCallError(bi, l, err)
			}
			if result.First {
				fmt.Println("The local password is set.")
				return nil
			}
			fmt.Println("The local password is changed. A backup made before still opens with the one it was made under.")
			return nil
		},
	}
}
