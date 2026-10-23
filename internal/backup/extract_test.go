package backup

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Check runs once the database is out and before any upload, so a backup
// that won't restore, as under a wrong password, writes none of its files.
func TestExtractChecksBeforeUploads(t *testing.T) {
	archive := buildArchive(t, manifestEntry(t), entry{name: databaseName, body: "db"}, entry{name: "uploads/ab", body: "a file"})
	dest := t.TempDir()
	wrong := errors.New("wrong password")
	checks := 0
	_, err := Extract(bytes.NewReader(archive), dest, ExtractOptions{Check: func(m Manifest, database string) error {
		checks++
		if m.Format != FormatVersion {
			t.Errorf("Check got manifest %+v", m)
		}
		if data, err := os.ReadFile(database); err != nil || string(data) != "db" {
			t.Errorf("Check's database: %q, %v", data, err)
		}
		return wrong
	}})
	if !errors.Is(err, wrong) || checks != 1 {
		t.Fatalf("Extract: %v after %d checks", err, checks)
	}
	if _, err := os.Stat(UploadsPath(dest)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an upload was written before the check: %v", err)
	}
}

// An archive v1.2.0 wrote, at gzip's default level, extracts as one
// written at the fastest.
func TestExtractReadsDefaultLevel(t *testing.T) {
	archive := buildArchive(t, manifestEntry(t), entry{name: databaseName, body: "db"}, entry{name: "uploads/ab", body: "a file"})
	dest := t.TempDir()
	if _, err := Extract(bytes.NewReader(archive), dest, ExtractOptions{}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(UploadsPath(dest), "ab")); err != nil || string(data) != "a file" {
		t.Fatalf("upload: %q, %v", data, err)
	}
}

// Extract stops before an entry would leave less than the floor free, and
// says how much was free; with room, it extracts everything.
func TestExtractKeepsTheFloorFree(t *testing.T) {
	archive := buildArchive(t, manifestEntry(t), entry{name: databaseName, body: "db"},
		entry{name: "uploads/small", body: strings.Repeat("x", 100)}, entry{name: "uploads/large", body: strings.Repeat("x", 5000)})
	var free uint64
	asks := 0
	opts := ExtractOptions{Floor: 1000, FreeSpace: func(string) (uint64, error) {
		asks++
		return free, nil
	}}
	// The fake disk never fills, so it says the same when asked again.
	free = 2000
	_, err := Extract(bytes.NewReader(archive), t.TempDir(), opts)
	var noRoom *NoRoomError
	if !errors.As(err, &noRoom) || noRoom.Free != 2000 {
		t.Fatalf("Extract without room: %v", err)
	}
	// Once at the start, and again for the entry that didn't fit.
	if asks != 2 {
		t.Errorf("asked for the free space %d times", asks)
	}

	free, asks = 1<<40, 0
	if _, err := Extract(bytes.NewReader(archive), t.TempDir(), opts); err != nil {
		t.Fatalf("Extract with room: %v", err)
	}
	if asks != 1 {
		t.Errorf("asked for the free space %d times with room to spare", asks)
	}
}

// Estimate is at least the archive's size, even for files that don't
// compress at all, as sealed uploads don't, and for many small ones.
func TestEstimateIsAnUpperBound(t *testing.T) {
	for _, shape := range []struct {
		files int
		size  int
	}{{0, 0}, {3, 300 << 10}, {400, 100}, {2, 3 << 20}} {
		src := t.TempDir()
		db := filepath.Join(src, "snapshot.db")
		writeRandom(t, db, 64<<10)
		uploads := filepath.Join(src, "uploads")
		if err := os.MkdirAll(uploads, 0o700); err != nil {
			t.Fatal(err)
		}
		for i := range shape.files {
			writeRandom(t, filepath.Join(uploads, strings.Repeat("a", 32)+string(rune('a'+i%26))+strings.Repeat("b", i/26)), shape.size)
		}
		var archive bytes.Buffer
		if _, err := Write(&archive, Manifest{App: "dens"}, db, uploads); err != nil {
			t.Fatal(err)
		}
		size, files, err := UploadsSize(uploads)
		if err != nil || files != shape.files || size != int64(shape.files*shape.size) {
			t.Fatalf("UploadsSize: %d bytes in %d files, %v", size, files, err)
		}
		if estimate := Estimate(64<<10, size, files); int64(archive.Len()) > estimate {
			t.Errorf("%d files of %d bytes: the archive is %d bytes, over the estimate of %d", shape.files, shape.size, archive.Len(), estimate)
		}
	}
	if size, files, err := UploadsSize(filepath.Join(t.TempDir(), "missing")); size != 0 || files != 0 || err != nil {
		t.Errorf("UploadsSize of no directory: %d, %d, %v", size, files, err)
	}
}

func writeRandom(t *testing.T, path string, n int) {
	t.Helper()
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSizeText(t *testing.T) {
	for n, want := range map[int64]string{
		0:               "0 B",
		1536:            "1.5 KB",
		10 << 20:        "10 MB",
		5 << 30:         "5 GB",
		200_000_000_000: "186 GB",
	} {
		if got := SizeText(n); got != want {
			t.Errorf("SizeText(%d) = %q, want %q", n, got, want)
		}
	}
}
