package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/layout"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xterm/prompt"

	"github.com/urfave/cli/v3"
)

func backupCommand(bi build.BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "backup",
		Usage: "write a backup of this instance, protected by your local password",
		Flags: archiveFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			path, err := saveArchive(bi, cmd, control.OpBackup, "backup")
			if err != nil {
				return err
			}
			fmt.Printf("Backup written to %s\n", path)
			fmt.Println("It is encrypted with your local password; keep both safe.")
			return nil
		},
	}
}

// archiveFlags are the flags of the commands that write an archive.
func archiveFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Usage:   "file to write (default: a dated name in the current directory)",
		},
		&cli.BoolFlag{
			Name:  "password-stdin",
			Usage: "read the local password from the first line of standard input",
		},
	}
}

// saveArchive has the service write an archive, a backup or the den's
// export (M10), as op asks and what names it, to the file --output names
// or under the service's name in the current directory, and returns its
// path.
func saveArchive(bi build.BuildInfo, cmd *cli.Command, op, what string) (string, error) {
	client, l, err := controlClient(bi, cmd.String("instance"))
	if err != nil {
		return "", err
	}
	password, err := readPassword(cmd.Bool("password-stdin"), "Local password")
	if err != nil {
		return "", err
	}

	output := cmd.String("output")
	dir := "."
	if l.Form == layout.Flatpak {
		// The sandbox writes only in the app's own folder, which the
		// member finds at the same path.
		dir = filepath.Dir(os.Getenv("XDG_DATA_HOME"))
	}
	if output != "" {
		dir = filepath.Dir(output)
		if _, err := os.Lstat(output); err == nil {
			return "", fmt.Errorf("%s already exists", output)
		}
	}
	fmt.Printf("Making the %s ...\n", what)
	path, err := writeBackup(client, control.Request{Op: op, Password: password}, dir, output, what)
	if errors.Is(err, control.ErrBodyCut) {
		log := "the log in " + l.Logs
		if l.Form == layout.Installed {
			log = host.ServiceLogHint(l.ServiceName, l.Logs)
		}
		return "", fmt.Errorf("the %s stopped partway, so nothing was kept; for why, see %s: %w", what, log, err)
	}
	if err != nil {
		return "", explainCallError(bi, l, err)
	}
	return path, nil
}

// backupRoom is what a backup leaves free on the disk it's written to,
// beyond its own size, so the disk isn't left full.
const backupRoom = 64 << 20

// freeSpace is host.FreeSpace; tests replace it.
var freeSpace = host.FreeSpace

// writeBackup has the service write an archive into dir, as output or under
// the name the service gives it, and keeps it only once it has come whole.
// It refuses before the archive comes when dir hasn't room for it. what
// names the archive: a backup, or the den's export (M10).
func writeBackup(client control.Client, req control.Request, dir, output, what string) (string, error) {
	temp, err := os.CreateTemp(dir, ".dens-backup-*.tmp")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	var result control.Backup
	callErr := client.CallBody(req, &result, func() (io.Writer, error) {
		free, err := freeSpace(dir)
		if err != nil {
			return nil, fmt.Errorf("check the room in %s: %w", dir, err)
		}
		if free < uint64(result.Size)+backupRoom {
			return nil, fmt.Errorf("the %s takes about %s, and %s has %s free; make room there, or write it elsewhere with --output",
				what, backup.SizeText(result.Size), dir, backup.SizeText(int64(free)))
		}
		fmt.Printf("Writing about %s ...\n", backup.SizeText(result.Size))
		return temp, nil
	})
	if callErr == nil {
		// The name says it's whole, so the file is on the disk first.
		callErr = temp.Sync()
	}
	closeErr := temp.Close()
	if callErr != nil {
		return "", callErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if output == "" {
		output = filepath.Join(dir, filepath.Base(result.Name))
	}
	if _, err := os.Lstat(output); !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s already exists", output)
	}
	if err := os.Rename(tempPath, output); err != nil {
		return "", err
	}
	return output, nil
}

// readPassword prompts on the terminal, or reads one line from standard
// input for scripts.
func readPassword(fromStdin bool, label string) (string, error) {
	passwords, err := readPasswords(fromStdin, label)
	if err != nil {
		return "", err
	}
	return passwords[0], nil
}

// readPasswords asks for a password for each label in turn on the
// terminal, or reads them from standard input for scripts, one a line,
// through one reader so none is lost to another's buffer.
func readPasswords(fromStdin bool, labels ...string) ([]string, error) {
	var in *bufio.Reader
	if fromStdin {
		in = bufio.NewReader(os.Stdin)
	}
	passwords := make([]string, 0, len(labels))
	for _, label := range labels {
		if in == nil {
			password, err := prompt.Secret(label)
			if err != nil {
				return nil, err
			}
			passwords = append(passwords, password)
			continue
		}
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return nil, fmt.Errorf("read password from standard input: %w", err)
		}
		// Windows PowerShell 5.1 starts text it pipes to a program with a
		// byte order mark when $OutputEncoding is UTF-8, as many profiles set.
		line = strings.TrimPrefix(line, "\ufeff")
		passwords = append(passwords, strings.TrimRight(line, "\r\n"))
	}
	return passwords, nil
}
