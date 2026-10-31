package storage

import (
	"database/sql"
	"errors"
	"time"

	"modernc.org/sqlite"
)

var errNoRows = sql.ErrNoRows

// SQLite primary result codes used for classification. Extended constraint codes
// are matched numerically because the driver returns them from Code().
const (
	sqliteBusy                 = 5
	sqliteLocked               = 6
	sqliteReadOnly             = 8
	sqliteConstraint           = 19
	sqliteCorrupt              = 11
	sqliteNotADB               = 26
	sqliteConstraintUnique     = 2067
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintForeignKey = 787
	sqliteConstraintNotNull    = 1299
	sqliteConstraintCheck      = 275
)

// IsBusy reports SQLITE_BUSY/SQLITE_LOCKED, including from another process.
func IsBusy(err error) bool {
	code, ok := sqliteCode(err)
	return ok && (code&0xff == sqliteBusy || code&0xff == sqliteLocked)
}

// IsUniqueViolation reports a UNIQUE or PRIMARY KEY constraint failure. Repositories
// translate it into their own conflict error.
func IsUniqueViolation(err error) bool {
	code, ok := sqliteCode(err)
	return ok && (code == sqliteConstraintUnique || code == sqliteConstraintPrimaryKey)
}

// IsConstraintViolation reports any SQLITE_CONSTRAINT failure, including foreign
// keys, NOT NULL and CHECK.
func IsConstraintViolation(err error) bool {
	code, ok := sqliteCode(err)
	return ok && code&0xff == sqliteConstraint
}

// IsReadOnly reports a write attempted on a read-only handle.
func IsReadOnly(err error) bool {
	code, ok := sqliteCode(err)
	return ok && code&0xff == sqliteReadOnly
}

// IsCorrupt reports a malformed database image detected during a query.
func IsCorrupt(err error) bool {
	code, ok := sqliteCode(err)
	return ok && (code&0xff == sqliteCorrupt || code&0xff == sqliteNotADB)
}

func sqliteCode(err error) (int, bool) {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code(), true
	}
	return 0, false
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
