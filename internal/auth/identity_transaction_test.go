package auth

import (
	"bytes"
	"context"
	"testing"
	"time"
)

type identityTransactionTestKey struct{}
type identityTransactionTestStore struct {
	*identityTestStore
	mode string
}

func (s identityTransactionTestStore) UpdateWithContext(ctx context.Context, fn func(context.Context, []byte) ([]byte, error)) error {
	if s.mode == "drop" {
		return nil
	}
	return s.identityTestStore.Update(ctx, func(raw []byte) ([]byte, error) {
		txCtx := context.WithValue(ctx, identityTransactionTestKey{}, "bound-reader")
		if s.mode == "nil" {
			txCtx = nil
		}
		if s.mode == "cancelled" {
			var cancel context.CancelFunc
			txCtx, cancel = context.WithCancel(txCtx)
			cancel()
		}
		next, err := fn(txCtx, raw)
		if err != nil {
			return nil, err
		}
		if s.mode == "duplicate" {
			return fn(txCtx, raw)
		}
		return next, nil
	})
}

func TestIdentityTransactionPathNeverSynthesizesReaderFromLegacyStore(t *testing.T) {
	service, store, _ := identityFixture(t)
	initial := bytes.Clone(store.data)
	if err := service.updateTransaction(context.Background(), func(context.Context, *identityState, time.Time) error {
		t.Error("legacy Update supplied a fake transaction context")
		return nil
	}); err != ErrIdentityUnavailable {
		t.Fatal(err)
	}
	if !bytes.Equal(store.data, initial) {
		t.Fatal("missing extension wrote state")
	}
	service.store = identityTransactionTestStore{store, ""}
	calls := 0
	if err := service.updateTransaction(context.Background(), func(ctx context.Context, st *identityState, now time.Time) error {
		calls++
		if ctx.Value(identityTransactionTestKey{}) != "bound-reader" {
			t.Error("bound reader lost")
		}
		return appendIdentityAudit(st, "system", "auth.transaction.probe", "prepared", now)
	}); err != nil || calls != 1 || len(store.data) == 0 {
		t.Fatal("actual callback not committed", calls, err)
	}
}

func TestIdentityTransactionRejectsMissingDuplicateNilAndCancelledCallbacks(t *testing.T) {
	for _, mode := range []string{"drop", "duplicate", "nil", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			service, store, _ := identityFixture(t)
			service.store = identityTransactionTestStore{store, mode}
			before := bytes.Clone(store.data)
			calls := 0
			err := service.updateTransaction(context.Background(), func(context.Context, *identityState, time.Time) error { calls++; return nil })
			if err != ErrIdentityUnavailable || !bytes.Equal(before, store.data) || calls > 1 {
				t.Fatal("invalid transaction committed", mode, calls, err)
			}
		})
	}
	service, store, _ := identityFixture(t)
	service.store = identityTransactionTestStore{store, ""}
	initial := bytes.Clone(store.data)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.updateTransaction(ctx, func(context.Context, *identityState, time.Time) error { cancel(); return nil }); err != context.Canceled || !bytes.Equal(store.data, initial) {
		t.Fatal("parent cancellation committed", err)
	}
}
