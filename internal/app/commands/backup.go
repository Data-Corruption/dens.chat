package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/pkg/xterm/prompt"

	"github.com/urfave/cli/v3"
)

func backupCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "backup",
		Usage: "write a backup of this instance, protected by your local password",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "file to write (default: a dated name in the current directory)",
			},
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

			output := cmd.String("output")
			dir := "."
			if output != "" {
				dir = filepath.Dir(output)
				if _, err := os.Lstat(output); err == nil {
					return fmt.Errorf("%s already exists", output)
				}
			}
			temp, err := os.CreateTemp(dir, ".dens-backup-*.tmp")
			if err != nil {
				return err
			}
			tempPath := temp.Name()
			defer os.Remove(tempPath)

			var result struct {
				Name string `json:"name"`
			}
			callErr := client.CallBody(control.Request{Op: control.OpBackup, Password: password}, &result, temp)
			closeErr := temp.Close()
			if callErr != nil {
				return explainCallError(bi, l, callErr)
			}
			if closeErr != nil {
				return closeErr
			}
			if output == "" {
				output = filepath.Join(dir, filepath.Base(result.Name))
			}
			if _, err := os.Lstat(output); !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("%s already exists", output)
			}
			if err := os.Rename(tempPath, output); err != nil {
				return err
			}
			fmt.Printf("Backup written to %s\n", output)
			fmt.Println("It is encrypted with your local password; keep both safe.")
			return nil
		},
	}
}

// readPassword prompts on the terminal, or reads one line from standard
// input for scripts.
func readPassword(fromStdin bool, label string) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read password from standard input: %w", err)
		}
		// Windows PowerShell 5.1 starts text it pipes to a program with a
		// byte order mark when $OutputEncoding is UTF-8, as many profiles set.
		line = strings.TrimPrefix(line, "\ufeff")
		return strings.TrimRight(line, "\r\n"), nil
	}
	return prompt.Secret(label)
}
