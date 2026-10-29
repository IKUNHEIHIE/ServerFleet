package storage_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

func TestGenerationAdvanceAndConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testkit.NewDB(t)
	generations := storage.NewGenerations(db)
	if err := generations.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := generations.Advance(ctx, "auth/user/u1", 0)
	if err != nil || first.Value != 1 {
		t.Fatalf("first advance = %+v, %v", first, err)
	}
	if _, err := generations.Advance(ctx, "auth/user/u1", 0); !errors.Is(err, storage.ErrGenerationConflict) {
		t.Fatalf("re-init err = %v, want conflict", err)
	}
	if _, err := generations.Advance(ctx, "auth/user/u1", 9); !errors.Is(err, storage.ErrGenerationConflict) {
		t.Fatalf("stale advance err = %v, want conflict", err)
	}
	second, err := generations.Advance(ctx, "auth/user/u1", 1)
	if err != nil || second.Value != 2 {
		t.Fatalf("second advance = %+v, %v", second, err)
	}
	current, err := generations.Current(ctx, "auth/user/u1")
	if err != nil || current.Value != 2 {
		t.Fatalf("current = %+v, %v", current, err)
	}
	absent, err := generations.Current(ctx, "auth/user/unknown")
	if err != nil || absent.Value != 0 {
		t.Fatalf("absent = %+v, %v", absent, err)
	}
}

func TestGenerationConcurrentCAS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testkit.NewDB(t)
	generations := storage.NewGenerations(db)
	if err := generations.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := generations.Advance(ctx, "tasks/queue", 0); err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	var success, conflicts int32
	var mu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := generations.Advance(ctx, "tasks/queue", 1)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				success++
			case errors.Is(err, storage.ErrGenerationConflict):
				conflicts++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	if success != 1 || conflicts != workers-1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	current, _ := generations.Current(ctx, "tasks/queue")
	if current.Value != 2 {
		t.Fatalf("value = %d, want 2", current.Value)
	}
}
