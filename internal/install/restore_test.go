package install

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Data-Corruption/dens.chat/internal/backup"
	"github.com/Data-Corruption/dens.chat/internal/platform/host"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, host.HostKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

// A wrong password is refused once the database is out, before any upload
// is written, rather than after all of them (M10).
func TestRestoreChecksBeforeUploads(t *testing.T) {
	key := testKey(t)
	archive := makeBackupWith(t, key, "correct horse battery", map[string]string{"0123456789abcdef": "sealed bytes"})
	bi := buildInfo("v1.0.0")

	staging := t.TempDir()
	if _, _, err := openBackup(bi, archive, staging, "wrong wrong wrong"); err == nil {
		t.Fatal("the wrong password opened the backup")
	}
	if _, err := os.Stat(backup.UploadsPath(staging)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an upload was written before the password was checked: %v", err)
	}

	staging = t.TempDir()
	got, _, err := openBackup(bi, archive, staging, "correct horse battery")
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("openBackup: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(backup.UploadsPath(staging), "0123456789abcdef")); err != nil || string(data) != "sealed bytes" {
		t.Fatalf("the upload: %q, %v", data, err)
	}
}

// A disk without room for the backup is refused before anything is
// written, and one that runs out partway stops before it fills, saying
// how much more the rest needs (M10).
func TestRestoreNeedsRoom(t *testing.T) {
	archive := makeBackupWith(t, testKey(t), "correct horse battery", map[string]string{"0123456789abcdef": "sealed bytes"})
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	bi := buildInfo("v1.0.0")
	free := freeSpace
	t.Cleanup(func() { freeSpace = free })

	freeSpace = func(string) (uint64, error) { return uint64(info.Size()) + restoreFloor - 1, nil }
	staging := t.TempDir()
	_, _, err = openBackup(bi, archive, staging, "correct horse battery")
	if err == nil || !strings.Contains(err.Error(), "takes that much, and 1 GB to spare") {
		t.Fatalf("a backup the disk hasn't room for: %v", err)
	}
	if entries, _ := os.ReadDir(staging); len(entries) != 0 {
		t.Fatalf("a refused restore wrote %v", entries)
	}

	// Room as it starts, and then none.
	asks := 0
	freeSpace = func(string) (uint64, error) {
		asks++
		if asks == 1 {
			return 1 << 40, nil
		}
		return restoreFloor, nil
	}
	_, _, err = openBackup(bi, archive, t.TempDir(), "correct horse battery")
	if err == nil || !strings.Contains(err.Error(), "ran out of room partway") || !strings.Contains(err.Error(), "more") {
		t.Fatalf("a restore that ran out of room: %v", err)
	}
}
