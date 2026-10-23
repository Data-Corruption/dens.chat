package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/app"
	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/internal/control"
	"github.com/Data-Corruption/dens.chat/internal/denproto"
)

// TestBackupHoldsDeletedFiles checks that a backup holds every file its
// database snapshot names, though the den deletes one while the archive is
// written (M8): the den takes it off the disk only once the backup is done.
// It runs the backup's handler in process, as a control request would, so
// it opens no port: the den's own listeners bind every interface, which
// Windows asks the desktop user about.
func TestBackupHoldsDeletedFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("LOCALAPPDATA", dir)
	a := app.New(build.BuildInfo{Name: "dens", Version: "v1.0.0", DefaultLogLevel: "warn", DevMode: true})
	// The den's port makes the instance host a den, whose listeners only
	// service.Run binds.
	if err := a.Open(app.OpenOptions{Instance: "backup", DevClientPort: 18484, DevDenPort: 18485}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()
	ctx := context.Background()
	if err := a.SetPassword(ctx, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	code, err := a.Den.Create(ctx, "Test Den", "https://den.test")
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(nil)
	info, _ := a.Den.Info()
	challenge, err := a.Den.Challenge(denproto.Random(denproto.NonceSize))
	if err != nil {
		t.Fatal(err)
	}
	joined, err := a.Den.Join(ctx, denproto.JoinRequest{
		Invite: code, Username: "alice", DisplayName: "Alice", Verifier: denproto.Random(denproto.VerifierSize),
		PublicKey: denproto.Bytes(key.Public().(ed25519.PublicKey)), DeviceLabel: "test", Nonce: challenge.Nonce,
		Proof: denproto.Prove(key, info.ID, challenge.Nonce), SealCheck: denproto.Random(denproto.SealCheckSize),
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := a.Den.Authenticate(ctx, joined.Token)
	if err != nil {
		t.Fatal(err)
	}
	name := "general"
	channel, err := a.Den.CreateChannel(ctx, s, denproto.ChannelRequest{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	notes := []byte("notes the backup must keep")
	file, err := a.Den.Upload(ctx, s, "notes.txt", int64(len(notes)), bytes.NewReader(notes), false, "")
	if err != nil {
		t.Fatal(err)
	}
	m, err := a.Den.Send(ctx, s, channel.ID, denproto.SendRequest{Nonce: denproto.Random(denproto.NonceBytes), Attachments: []string{file.ID}})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(a.Layout.Uploads)
	if err != nil || len(entries) != 1 {
		t.Fatalf("stored files %v, %v", entries, err)
	}
	stored := filepath.Join(a.Layout.Uploads, entries[0].Name())

	// The snapshot is taken before the reply, and the archive waits on its
	// reader from its first byte, so it hasn't walked the uploads yet.
	reply, err := controlHandlers(a, func(string) {})[control.OpBackup](ctx, control.Request{Op: control.OpBackup, Password: "correct horse battery"})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if err := a.Den.Delete(ctx, s, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("a file the backup's snapshot names went while it was written: %v", err)
	}
	archive, err := io.ReadAll(reply.Body)
	reply.Body.Close()
	if err != nil {
		t.Fatalf("read the archive: %v", err)
	}
	out := t.TempDir()
	if _, err := backup.Extract(bytes.NewReader(archive), out, backup.ExtractOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(backup.UploadsPath(out), entries[0].Name())); err != nil || len(got) == 0 {
		t.Fatalf("the archive lacks a file its snapshot names: %v", err)
	}
	// The archive ends only once the backup let its files go.
	if _, err := os.Stat(stored); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the deleted file stayed after the backup: %v", err)
	}
}
