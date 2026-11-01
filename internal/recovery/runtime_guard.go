package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// RuntimeGuard is called BEFORE opening/migrating the database. A restore
// directory always requires its exact external latch and explicit offline
// release. The returned live gate belongs on HTTP, scheduler and Agent frames.
// It proves local quarantine enforcement, not offline authority or remote fence.
func RuntimeGuard(ctx context.Context, database string, latch *QuarantineLatch) (func(context.Context) error, error) {
	absolute, err := filepath.Abs(database)
	if err != nil || database == "" || ctx == nil {
		return nil, ErrQuarantined
	}
	directory := filepath.Dir(absolute)
	marker := filepath.Join(directory, "hold.json")
	_, err = os.Lstat(marker)
	if errors.Is(err, os.ErrNotExist) && latch == nil {
		// Recheck the marker too: a running normal profile must not silently adopt
		// a restore hold created after startup.
		return func(ctx context.Context) error {
			if ctx == nil || ctx.Err() != nil {
				return ErrQuarantined
			}
			if _, e := os.Lstat(marker); !errors.Is(e, os.ErrNotExist) {
				return ErrQuarantined
			}
			return nil
		}, nil
	}
	if err != nil || latch == nil || filepath.Base(absolute) != "restored.db" {
		return nil, ErrQuarantined
	}
	check := func(ctx context.Context) error {
		if ctx == nil || ctx.Err() != nil {
			return ErrQuarantined
		}
		latch.mu.Lock()
		if latch.root == nil {
			latch.mu.Unlock()
			return ErrQuarantined
		}
		anchored, e := latch.root.Stat(".")
		actual, f := os.Lstat(directory)
		same := e == nil && f == nil && actual.IsDir() && os.SameFile(anchored, actual)
		latch.mu.Unlock()
		if !same || latch.Check(ctx) != nil {
			return ErrQuarantined
		}
		return nil
	}
	if check(ctx) != nil {
		return nil, ErrQuarantined
	}
	return check, nil
}
