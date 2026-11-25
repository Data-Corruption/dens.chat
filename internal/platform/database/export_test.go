package database

import (
	"context"
	"database/sql"

	"github.com/Data-Corruption/dens.chat/internal/build"
	"github.com/Data-Corruption/dens.chat/pkg/xlog"
)

// MigrateTo runs the steps up to version alone, which makes the schema a
// release left, for the tests outside the package.
func MigrateTo(ctx context.Context, db *sql.DB, log *xlog.Logger, version int) (int, error) {
	return newMigrator(build.BuildInfo{DefaultLogLevel: "warn"}).Upto(version).Run(ctx, db, log)
}
