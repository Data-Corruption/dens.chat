// Package backup reads and writes backup archives: a gzip-compressed tar
// holding a manifest, a consistent snapshot of the database and the
// uploads. The data key travels inside the database, sealed under the local
// password, so an archive is useless without that password. The format is
// the same on every platform.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// FormatVersion is the archive format this build writes and reads.
const FormatVersion = 1

const (
	manifestName  = "manifest.json"
	databaseName  = "db.sqlite"
	uploadsPrefix = "uploads/"
)

// Manifest describes an archive. It is always the first entry.
type Manifest struct {
	Format        int       `json:"format"`
	App           string    `json:"app"`
	Version       string    `json:"version"`
	SchemaVersion int       `json:"schemaVersion"`
	Instance      string    `json:"instance"`
	CreatedAt     time.Time `json:"createdAt"`
	Platform      string    `json:"platform"`
}

// Limits bound what Extract accepts from an archive.
type Limits struct {
	MaxBytes int64
	MaxFiles int
}

// DefaultLimits stop a malformed or hostile archive from filling the disk.
var DefaultLimits = Limits{MaxBytes: 256 << 30, MaxFiles: 1_000_000}

// Write streams an archive to w: the manifest, the database snapshot at
// dbPath, and every regular file under uploadsDir (which may not exist).
func Write(w io.Writer, m Manifest, dbPath, uploadsDir string) error {
	m.Format = FormatVersion
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := writeEntry(tw, manifestName, int64(len(manifest)), m.CreatedAt, strings.NewReader(string(manifest))); err != nil {
		return err
	}
	if err := writeFile(tw, databaseName, dbPath); err != nil {
		return err
	}
	err = filepath.WalkDir(uploadsDir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == uploadsDir {
				return fs.SkipDir
			}
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(uploadsDir, p)
		if err != nil {
			return err
		}
		return writeFile(tw, uploadsPrefix+filepath.ToSlash(rel), p)
	})
	if err != nil {
		return fmt.Errorf("archive uploads: %w", err)
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeFile(tw *tar.Writer, name, src string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	return writeEntry(tw, name, info.Size(), info.ModTime(), file)
}

func writeEntry(tw *tar.Writer, name string, size int64, modTime time.Time, r io.Reader) error {
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o600,
		Size:     size,
		ModTime:  modTime,
		Typeflag: tar.TypeReg,
		Format:   tar.FormatPAX,
	}); err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}
	if _, err := io.CopyN(tw, r, size); err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}
	return nil
}

// Extract reads an archive into the empty directory dir and returns its
// manifest. The database lands at DatabasePath(dir) and uploads under
// UploadsPath(dir). Only the expected entries are accepted: the manifest
// first, the database once, and regular files with clean relative names
// under uploads/.
func Extract(r io.Reader, dir string, limits Limits) (Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return Manifest{}, fmt.Errorf("not a backup archive: %w", err)
	}
	tr := tar.NewReader(gz)

	header, err := tr.Next()
	if err != nil {
		return Manifest{}, fmt.Errorf("read archive: %w", err)
	}
	if header.Name != manifestName || header.Typeflag != tar.TypeReg || header.Size > 64*1024 {
		return Manifest{}, errors.New("archive does not start with a manifest")
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(tr, header.Size)).Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	if m.Format != FormatVersion {
		return Manifest{}, fmt.Errorf("archive format %d is not supported (want %d)", m.Format, FormatVersion)
	}

	var total int64
	files := 0
	haveDatabase := false
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, fmt.Errorf("read archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			return Manifest{}, fmt.Errorf("archive entry %q is not a regular file", header.Name)
		}
		files++
		total += header.Size
		if header.Size < 0 || files > limits.MaxFiles || total > limits.MaxBytes {
			return Manifest{}, errors.New("archive exceeds the size limits")
		}
		var dest string
		switch {
		case header.Name == databaseName && !haveDatabase:
			haveDatabase = true
			dest = DatabasePath(dir)
		case strings.HasPrefix(header.Name, uploadsPrefix):
			rel := strings.TrimPrefix(header.Name, uploadsPrefix)
			if !cleanRelative(rel) {
				return Manifest{}, fmt.Errorf("archive entry %q has an unsafe name", header.Name)
			}
			dest = filepath.Join(UploadsPath(dir), filepath.FromSlash(rel))
		default:
			return Manifest{}, fmt.Errorf("unexpected archive entry %q", header.Name)
		}
		if err := extractFile(tr, dest, header.Size); err != nil {
			return Manifest{}, err
		}
	}
	if !haveDatabase {
		return Manifest{}, errors.New("archive has no database")
	}
	return m, nil
}

// DatabasePath is where Extract puts the database snapshot.
func DatabasePath(dir string) string { return filepath.Join(dir, databaseName) }

// UploadsPath is where Extract puts uploads.
func UploadsPath(dir string) string { return filepath.Join(dir, "uploads") }

func extractFile(r io.Reader, dest string, size int64) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("extract %s: %w", filepath.Base(dest), err)
	}
	_, copyErr := io.CopyN(file, r, size)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("extract %s: %w", filepath.Base(dest), copyErr)
	}
	return closeErr
}

// cleanRelative accepts only plain relative slash paths that stay inside
// their directory on every platform.
func cleanRelative(name string) bool {
	if name == "" || path.Clean(name) != name || strings.ContainsAny(name, `\:`) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return filepath.IsLocal(filepath.FromSlash(name))
}
