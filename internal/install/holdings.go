package install

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/Data-Corruption/dens.chat/internal/denproto"
	"github.com/Data-Corruption/dens.chat/internal/platform/database"
)

// holdings is what an instance's data holds that it would lose to an
// uninstall, or to a den's export restored over it (M10): the den it
// hosts, if any, and how many dens it has joined.
type holdings struct {
	den    string // the den's name, "" for none
	denID  []byte
	joined int
}

// describe says what the instance holds, as "hosts the den Foo and has
// accounts in 3 dens", or "" for nothing.
func (h holdings) describe() string {
	var joined string
	switch h.joined {
	case 0:
	case 1:
		joined = "has an account in 1 den"
	default:
		joined = fmt.Sprintf("has accounts in %d dens", h.joined)
	}
	switch {
	case h.den != "" && joined != "":
		return fmt.Sprintf("hosts the den %s and %s", h.den, joined)
	case h.den != "":
		return "hosts the den " + h.den
	}
	return joined
}

// errCrashed is a database a crash left with a WAL and no index for it.
var errCrashed = errors.New("its database was left mid-write when the service last stopped; start the instance once, then try again")

// readHoldings reads what the database file at path holds; one that
// doesn't exist holds nothing. A maintenance command runs as root, so it
// must leave nothing beside the file that the service account couldn't
// open. While the service runs, its connections keep the WAL and its
// index, which a read-only connection uses as they are. Once it stops,
// closing folds the WAL into the database and deletes both, so the file
// opens as immutable, which makes neither. A WAL a crash left without its
// index can't be read without making one, so it isn't.
func readHoldings(ctx context.Context, path string) (holdings, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return holdings{}, nil
	}
	if err != nil {
		return holdings{}, err
	}
	if !info.Mode().IsRegular() {
		return holdings{}, fmt.Errorf("%s is not a regular file", path)
	}
	wal, err := os.Lstat(path + "-wal")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return holdings{}, err
	}
	_, err = os.Lstat(path + "-shm")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return holdings{}, err
	}
	index := err == nil
	var db *sql.DB
	switch {
	case wal != nil && index:
		db, err = database.OpenReadOnly(path)
	case wal == nil || wal.Size() == 0:
		db, err = database.OpenImmutable(path)
	default:
		return holdings{}, errCrashed
	}
	if err != nil {
		return holdings{}, err
	}
	defer db.Close()
	var h holdings
	var public []byte
	err = db.QueryRowContext(ctx, `SELECT name, public_key FROM den WHERE id = 1`).Scan(&h.den, &public)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return holdings{}, fmt.Errorf("read the den: %w", err)
	default:
		h.denID = denproto.ID(public)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM joined_dens`).Scan(&h.joined); err != nil {
		return holdings{}, fmt.Errorf("read the joined dens: %w", err)
	}
	return h, nil
}
