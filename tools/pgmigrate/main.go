// Command pgmigrate imports a stopped, verified portable ServerFleet snapshot
// into an empty PostgreSQL database. It never overwrites existing tables.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/storage"
	"time"
)

func main() {
	snapshot := flag.String("sqlite", "", "stopped portable SQLite snapshot")
	dsnFile := flag.String("pg-dsn-file", "", "private PostgreSQL DSN file; target must be empty")
	flag.Parse()
	if flag.NArg() != 0 || *snapshot == "" || *dsnFile == "" {
		fmt.Fprintln(os.Stderr, "pgmigrate: -sqlite and -pg-dsn-file are required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	dsn, err := storage.ReadDSNFile(*dsnFile)
	if err == nil {
		var m *storage.Migrator
		m, err = storage.NewMigrator(server.Migrations()...)
		if err == nil {
			err = storage.RestorePostgres(ctx, *snapshot, storage.Config{Path: *snapshot, Driver: "postgres", DSN: dsn}, m)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgmigrate: import rejected; target unchanged")
		os.Exit(1)
	}
	fmt.Println("PostgreSQL import committed; recovery authority remains unchanged")
}
