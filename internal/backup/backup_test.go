package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteExtractRoundTrip(t *testing.T) {
	src := t.TempDir()
	dbPath := filepath.Join(src, "snapshot.db")
	if err := os.WriteFile(dbPath, []byte("sqlite bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	uploads := filepath.Join(src, "uploads")
	if err := os.MkdirAll(filepath.Join(uploads, "ab"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploads, "ab", "cdef.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	want := Manifest{App: "dens", Version: "v1.0.0", SchemaVersion: 3, Instance: "main", CreatedAt: time.Now().UTC().Truncate(time.Second), Platform: "linux/amd64"}
	if err := Write(&archive, want, dbPath, uploads); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	got, err := Extract(bytes.NewReader(archive.Bytes()), dest, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	want.Format = FormatVersion
	if got != want {
		t.Fatalf("manifest = %+v, want %+v", got, want)
	}
	if data, _ := os.ReadFile(DatabasePath(dest)); string(data) != "sqlite bytes" {
		t.Fatalf("database = %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(UploadsPath(dest), "ab", "cdef.png")); string(data) != "png" {
		t.Fatalf("upload = %q", data)
	}
}

func TestWriteWithoutUploads(t *testing.T) {
	src := t.TempDir()
	dbPath := filepath.Join(src, "snapshot.db")
	if err := os.WriteFile(dbPath, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := Write(&archive, Manifest{}, dbPath, filepath.Join(src, "missing")); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(&archive, t.TempDir(), DefaultLimits); err != nil {
		t.Fatal(err)
	}
}

type entry struct {
	name     string
	body     string
	typeflag byte
}

func buildArchive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{Name: e.name, Mode: 0o600, Size: int64(len(e.body)), Typeflag: typeflag}
		if typeflag == tar.TypeSymlink {
			header.Size = 0
			header.Linkname = "/etc/passwd"
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func manifestEntry(t *testing.T) entry {
	data, err := json.Marshal(Manifest{Format: FormatVersion})
	if err != nil {
		t.Fatal(err)
	}
	return entry{name: manifestName, body: string(data)}
}

func TestExtractRejectsUnsafeArchives(t *testing.T) {
	db := entry{name: databaseName, body: "db"}
	cases := map[string][]byte{
		"no manifest":      buildArchive(t, db),
		"traversal":        buildArchive(t, manifestEntry(t), db, entry{name: "uploads/../../escape", body: "x"}),
		"absolute":         buildArchive(t, manifestEntry(t), db, entry{name: "uploads//etc/passwd", body: "x"}),
		"backslash":        buildArchive(t, manifestEntry(t), db, entry{name: `uploads/..\escape`, body: "x"}),
		"drive letter":     buildArchive(t, manifestEntry(t), db, entry{name: "uploads/C:evil", body: "x"}),
		"symlink":          buildArchive(t, manifestEntry(t), db, entry{name: "uploads/link", typeflag: tar.TypeSymlink}),
		"unknown entry":    buildArchive(t, manifestEntry(t), db, entry{name: "surprise", body: "x"}),
		"second database":  buildArchive(t, manifestEntry(t), db, db),
		"missing database": buildArchive(t, manifestEntry(t)),
		"not gzip":         []byte("plain text"),
	}
	for name, archive := range cases {
		if _, err := Extract(bytes.NewReader(archive), t.TempDir(), DefaultLimits); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	big := buildArchive(t, manifestEntry(t), entry{name: databaseName, body: strings.Repeat("x", 100)})
	if _, err := Extract(bytes.NewReader(big), t.TempDir(), Limits{MaxBytes: 10, MaxFiles: 10}); err == nil {
		t.Error("oversized archive accepted")
	}
}
