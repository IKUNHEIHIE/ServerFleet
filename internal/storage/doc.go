// Package storage will own SQLite connection setup, WAL, transactions and
// migration infrastructure. Business schemas belong to their owning modules.
// No database is opened and no public schema is frozen in M0-P1.
package storage
