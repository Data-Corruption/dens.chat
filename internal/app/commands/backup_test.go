package commands

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// serveBackup answers OpBackup with archive on a control endpoint of its
// own, and returns a client for it.
func serveBackup(t *testing.T, archive func() io.Reader) control.Client {
	t.Helper()
	return serveHandlers(t, map[string]control.Handler{
		control.OpBackup: func(context.Context, control.Request) (control.Reply, error) {
			return control.Reply{Result: control.Backup{Name: "dens-main-20261023-120000.backup", Size: 1 << 20}, Body: io.NopCloser(archive())}, nil
		},
	})
}

// serveHandlers answers control requests with handlers on an endpoint of
// its own, and returns a client for it.
func serveHandlers(t *testing.T, handlers map[string]control.Handler) control.Client {
	t.Helper()
	me, err := host.CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	logger, err := xlog.New(filepath.Join(t.TempDir(), "logs"), "error")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := filepath.Join(t.TempDir(), "control.sock")
	if runtime.GOOS == "windows" {
		endpoint = `\\.\pipe\dens-backup-test-` + strings.ReplaceAll(t.Name(), "/", "-") + "-" + time.Now().Format("150405.000000000")
	}
	ln, err := host.ListenControl(endpoint, me)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := &control.Server{Allowed: me, Log: logger, Handlers: handlers}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		logger.Close()
	})
	return control.Client{Endpoint: endpoint, Server: host.ControlServer{Account: me}, Timeout: 5 * time.Second}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) {
	return 0, errors.New("read an upload: input/output error")
}

// A backup that stops partway leaves nothing behind, under its name or the
// temporary one: what came isn't a backup.
func TestWriteBackupKeepsNothingCutShort(t *testing.T) {
	client := serveBackup(t, func() io.Reader { return io.MultiReader(strings.NewReader("half an archive"), brokenReader{}) })
	dir := t.TempDir()
	if _, err := writeBackup(client, control.Request{Op: control.OpBackup}, dir, "", "backup"); !errors.Is(err, control.ErrBodyCut) {
		t.Fatalf("writeBackup: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a backup cut short left %v", entries)
	}
}

// Without room for the archive where it goes, the backup stops before the
// archive comes, saying how large it is.
func TestWriteBackupNeedsRoom(t *testing.T) {
	client := serveBackup(t, func() io.Reader { return strings.NewReader("an archive") })
	free := freeSpace
	freeSpace = func(string) (uint64, error) { return 1 << 20, nil }
	t.Cleanup(func() { freeSpace = free })
	dir := t.TempDir()
	_, err := writeBackup(client, control.Request{Op: control.OpBackup}, dir, "", "backup")
	if err == nil || !strings.Contains(err.Error(), "about 1 MB") {
		t.Fatalf("writeBackup without room: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a refused backup left %v", entries)
	}
}

func TestWriteBackupWritesTheArchive(t *testing.T) {
	client := serveBackup(t, func() io.Reader { return strings.NewReader("a whole archive") })
	dir := t.TempDir()
	path, err := writeBackup(client, control.Request{Op: control.OpBackup}, dir, "", "backup")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "dens-main-20261023-120000.backup") {
		t.Fatalf("written to %s", path)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "a whole archive" {
		t.Fatalf("the backup: %q, %v", data, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("the folder holds %v", entries)
	}
}

func TestReadPasswordFromStdin(t *testing.T) {
	for name, input := range map[string]string{
		"lf":         "correct horse\n",
		"crlf":       "correct horse\r\n",
		"no newline": "correct horse",
		// What Windows PowerShell 5.1 pipes when $OutputEncoding is UTF-8.
		"byte order mark": "\xef\xbb\xbfcorrect horse\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.WriteString(input); err != nil {
				t.Fatal(err)
			}
			w.Close()
			stdin := os.Stdin
			os.Stdin = r
			t.Cleanup(func() {
				os.Stdin = stdin
				r.Close()
			})
			got, err := readPassword(true, "")
			if err != nil {
				t.Fatal(err)
			}
			if got != "correct horse" {
				t.Fatalf("readPassword = %q, want %q", got, "correct horse")
			}
		})
	}
}
