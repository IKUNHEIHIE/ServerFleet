package platform_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/ai"
	"serverfleet.local/serverfleet/internal/storage"
)

func newSQLBudget(t *testing.T, db *storage.DB, failAudit, uncertain *atomic.Bool) *ai.SQLGenerationBudgetStore {
	t.Helper()
	store, err := ai.NewSQLGenerationBudgetStore(ai.GenerationBudgetSQLBackend{InTx: func(ctx context.Context, fn func(ai.GenerationBudgetSQLStatements) error) error {
		err := db.InTx(ctx, func(txctx context.Context, tx storage.Tx) error {
			exec := func(ctx context.Context, q string, args ...any) (int64, error) {
				r, e := tx.Exec(ctx, q, args...)
				if e != nil {
					return 0, e
				}
				return r.RowsAffected()
			}
			return fn(ai.GenerationBudgetSQLStatements{Read: func(ctx context.Context, q string, args ...any) ([]byte, error) {
				var raw []byte
				e := tx.QueryRow(ctx, q, args...).Scan(&raw)
				return raw, e
			}, Exec: exec, AppendAudit: func(ctx context.Context, e []ai.GenerationBudgetAudit) error {
				if err := ai.AppendGenerationBudgetAudit(ctx, exec, e); err != nil {
					return err
				}
				if failAudit.Load() {
					return errors.New("synthetic audit rollback")
				}
				return nil
			}})
		})
		if err == nil && uncertain.Load() {
			return errors.New("synthetic lost commit acknowledgement")
		}
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func generationBudgetRequest(n int) ai.GenerationRequest {
	id := fmt.Sprintf("message-%d", n)
	return ai.GenerationRequest{SubjectID: "owner", SessionID: "session", MessageID: id, OperationID: "ai-generation-" + id, ScopeRevision: 1, Profile: ai.ModelProfile{ID: "profile", Model: "fixture-model", CredentialRef: "reference", Revision: 1}, Messages: []ai.Message{{MessageID: id, SessionID: "session", Role: "user", RedactedContent: "synthetic content must not be persisted in budget", CreatedAt: time.Now().UTC(), TaskIDs: []string{}}}}
}
func TestPersistentGenerationBudgetSQLiteConcurrencyRollbackRestartAndUnknown(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "budget.db")
	db, err := storage.Open(ctx, storage.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	for _, q := range ai.GenerationBudgetSchemaStatements() {
		if _, err = db.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	failAudit, uncertain := new(atomic.Bool), new(atomic.Bool)
	newBudget := func() *ai.PersistentGenerationBudget {
		b, e := ai.NewPersistentGenerationBudget(newSQLBudget(t, db, failAudit, uncertain), func(context.Context, string, ai.GenerationBudgetPolicy) error { return nil }, func(context.Context, ai.GenerationRequest, string) error { return nil })
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	b := newBudget()
	now := time.Now().UTC()
	policy := ai.GenerationBudgetPolicy{OwnerID: "owner", ProfileID: "profile", WindowID: "explicit-window", Revision: 1, StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Hour), RequestLimit: 1, InputByteLimit: 256000, OutputTokenLimit: 8192}
	if err = b.Configure(ctx, "budget-admin", 0, policy); err != nil {
		t.Fatal(err)
	}
	failAudit.Store(true)
	if b.Admit(ctx, generationBudgetRequest(0)) == nil {
		t.Fatal("failed audit admitted provider use")
	}
	failAudit.Store(false)
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM cx_ai_budget_audit WHERE action='ai.budget.reserve'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("audit not rolled back", count, err)
	}
	var wg sync.WaitGroup
	var admitted atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if b.Admit(ctx, generationBudgetRequest(n)) == nil {
				admitted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if admitted.Load() != 1 {
		t.Fatal("quota overspend", admitted.Load())
	}
	db.Close()
	db, err = storage.Open(ctx, storage.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	b = newBudget()
	if b.Admit(ctx, generationBudgetRequest(100)) == nil {
		t.Fatal("restart reset budget")
	}
	var payload []byte
	if err = db.QueryRow(ctx, `SELECT payload FROM cx_ai_generation_budget`).Scan(&payload); err != nil || strings.Contains(string(payload), "synthetic content") {
		t.Fatal("prompt persisted in quota", err)
	}
	// New explicit window, same operation fences retained. Simulate an actually
	// committed reservation whose acknowledgement is lost, then reopen the DB.
	policy.Revision = 2
	policy.WindowID = "next-window"
	if err = b.Configure(ctx, "budget-admin", 1, policy); err != nil {
		t.Fatal(err)
	}
	uncertain.Store(true)
	request := generationBudgetRequest(200)
	if b.Admit(ctx, request) == nil {
		t.Fatal("lost acknowledgement reported success")
	}
	uncertain.Store(false)
	db.Close()
	db, err = storage.Open(ctx, storage.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	b = newBudget()
	if b.Admit(ctx, request) == nil {
		t.Fatal("unknown reservation replayed")
	}
}
