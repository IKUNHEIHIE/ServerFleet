package storage

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

func readPrivateDSN(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrInvalidConfig
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || before.Size() <= 0 || before.Size() > 16<<10 {
		return nil, errors.New("storage: private DSN file required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("storage: cannot read private DSN file")
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, ErrInvalidConfig
	}
	raw, err := io.ReadAll(io.LimitReader(f, (16<<10)+1))
	if err != nil || len(raw) > 16<<10 {
		return nil, ErrInvalidConfig
	}
	return raw, nil
}
