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
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
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

// Write streams an archive to w: the manifest, the database snapshot at
// dbPath, and every regular file under uploadsDir (which may not exist).
// A file that goes between the walk finding it and the archive taking it
// is left out, and counted in skipped: the service holds back deleting
// what the snapshot names, so such a file was never in it.
func Write(w io.Writer, m Manifest, dbPath, uploadsDir string) (skipped int, err error) {
	m.Format = FormatVersion
	// The uploads are sealed, so they don't compress, and gzip's default
	// level takes them at an eighth of the fastest's speed (M10): about 116
	// MiB/s against 950 with Go 1.26. Any level reads the same.
	gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if err != nil {
		return 0, err
	}
	tw := tar.NewWriter(gz)
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := writeEntry(tw, manifestName, int64(len(manifest)), m.CreatedAt, strings.NewReader(string(manifest))); err != nil {
		return 0, err
	}
	if err := writeFile(tw, databaseName, dbPath); err != nil {
		return 0, err
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
		err = writeFile(tw, uploadsPrefix+filepath.ToSlash(rel), p)
		if errors.Is(err, fs.ErrNotExist) {
			skipped++
			return nil
		}
		return err
	})
	if err != nil {
		return skipped, fmt.Errorf("archive uploads: %w", err)
	}
	if err := tw.Close(); err != nil {
		return skipped, err
	}
	return skipped, gz.Close()
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

// ExtractOptions guard an extraction.
type ExtractOptions struct {
	// Check runs once the manifest and the database are out, with the
	// database's path, before any upload, so an archive that won't
	// restore, under a wrong password or from another app, stops before
	// its files are written.
	Check func(m Manifest, database string) error
	// FreeSpace says how many bytes are free on the disk holding dir.
	// Extract stops before an entry would leave less than Floor there;
	// without FreeSpace, it doesn't look.
	FreeSpace func(dir string) (uint64, error)
	Floor     uint64
}

// NoRoomError is an extraction stopped before an entry would have left
// less than its floor free: Free is what the disk had, and Need the entry
// with the floor.
type NoRoomError struct{ Free, Need uint64 }

func (e *NoRoomError) Error() string {
	return "not enough room on the disk: " + SizeText(int64(e.Free)) + " free"
}

// Extract reads an archive into the empty directory dir and returns its
// manifest. The database lands at DatabasePath(dir) and uploads under
// UploadsPath(dir). Only the expected entries are accepted: the manifest
// first, then the database, which every archive has second, and then
// regular files with clean relative names under uploads/.
func Extract(r io.Reader, dir string, opts ExtractOptions) (Manifest, error) {
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

	room := &room{free: opts.FreeSpace, dir: dir, floor: opts.Floor}
	header, err = tr.Next()
	if errors.Is(err, io.EOF) {
		return m, errors.New("archive has no database")
	}
	if err != nil {
		return m, fmt.Errorf("read archive: %w", err)
	}
	if header.Name != databaseName || header.Typeflag != tar.TypeReg {
		return m, fmt.Errorf("archive has %q where its database belongs", header.Name)
	}
	if err := extractEntry(tr, header, DatabasePath(dir), room); err != nil {
		return m, err
	}
	if opts.Check != nil {
		if err := opts.Check(m, DatabasePath(dir)); err != nil {
			return m, err
		}
	}

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return m, nil
		}
		if err != nil {
			return m, fmt.Errorf("read archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			return m, fmt.Errorf("archive entry %q is not a regular file", header.Name)
		}
		rel, ok := strings.CutPrefix(header.Name, uploadsPrefix)
		if !ok {
			return m, fmt.Errorf("unexpected archive entry %q", header.Name)
		}
		if !cleanRelative(rel) {
			return m, fmt.Errorf("archive entry %q has an unsafe name", header.Name)
		}
		if err := extractEntry(tr, header, filepath.Join(UploadsPath(dir), filepath.FromSlash(rel)), room); err != nil {
			return m, err
		}
	}
}

func extractEntry(tr *tar.Reader, header *tar.Header, dest string, room *room) error {
	if header.Size < 0 {
		return fmt.Errorf("archive entry %q has a negative size", header.Name)
	}
	if err := room.take(uint64(header.Size)); err != nil {
		return err
	}
	return extractFile(tr, dest, header.Size)
}

// room keeps an extraction from filling its disk. It asks the system how
// much is free only now and then: after each gigabyte it writes, and when
// an entry would leave less than the floor by its own count.
type room struct {
	free  func(string) (uint64, error)
	dir   string
	floor uint64
	// known is what was free when last asked, less what's written since.
	known, since uint64
	asked        bool
}

func (r *room) take(size uint64) error {
	if r.free == nil {
		return nil
	}
	if !r.asked || r.since >= 1<<30 || r.known < size+r.floor {
		free, err := r.free(r.dir)
		if err != nil {
			return fmt.Errorf("check the room on the disk: %w", err)
		}
		r.known, r.since, r.asked = free, 0, true
	}
	if r.known < size+r.floor {
		return &NoRoomError{Free: r.known, Need: size + r.floor}
	}
	r.known -= size
	r.since += size
	return nil
}

// UploadsSize totals the files under dir, which may not exist, and counts
// them.
func UploadsSize(dir string) (size int64, files int, err error) {
	err = filepath.WalkDir(dir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == dir {
				return fs.SkipDir
			}
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		size += info.Size()
		files++
		return nil
	})
	return size, files, err
}

// Estimate is at least the size of an archive of a database and uploads of
// these sizes. The uploads are sealed, so gzip stores them, adding 5 bytes
// to each block of up to 64 KiB, and the database only shrinks; each entry
// adds a tar header and padding, under 1 KiB; and the manifest, gzip's
// header and the archive's end take less than 64 KiB.
func Estimate(database, uploads int64, files int) int64 {
	data := database + uploads
	return data + data/8192 + int64(files+1)*1024 + 64<<10
}

// SizeText writes a size as the page does: 186 GB.
func SizeText(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	units := []string{"KB", "MB", "GB"}
	v := float64(n) / 1024
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v < 10 {
		return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0") + " " + units[i]
	}
	return strconv.FormatFloat(math.Round(v), 'f', 0, 64) + " " + units[i]
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
