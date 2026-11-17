package bootstrap

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestParseOptionsPrivatePostgresDSN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(path, []byte("host=localhost dbname=isolated"), 0600); err != nil {
		t.Fatal(err)
	}
	opts, err := ParseOptions([]string{"-db-driver", "postgres", "-db-dsn-file", path}, io.Discard)
	if err != nil || opts.DBDSN != "host=localhost dbname=isolated" {
		t.Fatal("private connection config rejected", err)
	}
	for _, args := range [][]string{
		{"-db-driver", "sqlite", "-db-dsn-file", path},
		{"-db-driver", "postgres", "-db-dsn-file", path, "-db-dsn", "conflicting"},
	} {
		if _, err := ParseOptions(args, io.Discard); err == nil {
			t.Fatal("conflicting database flags accepted")
		}
	}
}
