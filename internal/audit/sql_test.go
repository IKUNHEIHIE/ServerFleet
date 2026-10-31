package audit_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

func newRecorder(t *testing.T, now time.Time) (*audit.SQLRecorder, *storage.DB) {
	t.Helper()
	db := testkit.NewDB(t, audit.Migrations())
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator([]string{"nodes.read", "nodes.update", audit.ActionRetention}, limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return recorder, db
}

func baseEvent(at time.Time) audit.Event {
	return audit.Event{
		SubjectID: "user-1",
		Action:    "nodes.update",
		Result:    "success",
		RequestID: "req-1",
		Source:    audit.SourceWeb,
		At:        at,
	}
}

func TestSQLRecorderAppendQueryAndRestart(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	recorder, db := newRecorder(t, now)
	ctx := context.Background()

	first := baseEvent(now.Add(-2 * time.Minute))
	first.TaskID = "task-1"
	first.NodeIDs = []string{"n1", "n2"}
	first.ReasonCode = "policy"
	if err := recorder.Append(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := baseEvent(now.Add(-time.Minute))
	second.Action = "nodes.read"
	second.Result = "denied"
	second.SubjectID = "ai-1"
	second.Source = audit.SourceAI
	if err := recorder.Append(ctx, second); err != nil {
		t.Fatal(err)
	}

	page, err := recorder.Query(ctx, audit.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 || page.Items[0].SubjectID != "ai-1" {
		t.Fatalf("page = %+v", page)
	}
	if got := page.Items[1].NodeIDs; len(got) != 2 || got[0] != "n1" {
		t.Fatalf("node ids = %+v", got)
	}
	if page.Items[1].TaskID != "task-1" || page.Items[1].ReasonCode != "policy" {
		t.Fatalf("fields lost: %+v", page.Items[1])
	}

	byTask, err := recorder.Query(ctx, audit.Query{TaskID: "task-1"})
	if err != nil || byTask.Total != 1 {
		t.Fatalf("byTask = %+v, %v", byTask, err)
	}
	bySource, err := recorder.Query(ctx, audit.Query{Source: audit.SourceAI})
	if err != nil || bySource.Total != 1 || bySource.Items[0].Action != "nodes.read" {
		t.Fatalf("bySource = %+v, %v", bySource, err)
	}
	denied, err := recorder.Query(ctx, audit.Query{Result: "denied"})
	if err != nil || denied.Total != 1 {
		t.Fatalf("denied = %+v, %v", denied, err)
	}
	page1, err := recorder.Query(ctx, audit.Query{Limit: 1})
	if err != nil || !page1.HasMore || len(page1.Items) != 1 {
		t.Fatalf("page1 = %+v, %v", page1, err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(ctx, storage.Config{Path: db.Path()})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	validator, _ := audit.NewValidator([]string{"nodes.read", "nodes.update", audit.ActionRetention}, audit.DefaultLimits())
	reopenedRecorder, err := audit.NewSQLRecorder(reopened, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	count, err := reopenedRecorder.Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("count after restart = %d, %v", count, err)
	}
}

func TestSQLRecorderRejectsInvalidEvent(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	recorder, _ := newRecorder(t, now)
	ctx := context.Background()
	missingSource := baseEvent(now)
	missingSource.Source = ""
	if err := recorder.Append(ctx, missingSource); err == nil {
		t.Fatal("accepted event without source")
	}
	duplicate := baseEvent(now)
	duplicate.NodeIDs = []string{"n1", "n1"}
	if err := recorder.Append(ctx, duplicate); err == nil {
		t.Fatal("accepted duplicate node ids")
	}
	badReason := baseEvent(now)
	badReason.ReasonCode = "has spaces"
	if err := recorder.Append(ctx, badReason); err == nil {
		t.Fatal("accepted reason code with spaces")
	}
	count, err := recorder.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("invalid events were persisted: count=%d err=%v", count, err)
	}
}

func TestSQLRecorderAppendTxRollsBackWithMutation(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	recorder, db := newRecorder(t, now)
	ctx := context.Background()
	boom := errors.New("mutation failed")
	err := db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(txCtx, "CREATE TABLE IF NOT EXISTS demo (id TEXT)"); err != nil {
			return err
		}
		if err := recorder.AppendTx(txCtx, tx, baseEvent(now)); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	count, err := recorder.Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("audit survived rollback: %d, %v", count, err)
	}
}

func TestSQLRecorderUpdateTriggerBlocksTampering(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	recorder, db := newRecorder(t, now)
	ctx := context.Background()
	if err := recorder.Append(ctx, baseEvent(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "UPDATE audit_events SET result = 'allowed'"); err == nil {
		t.Fatal("update trigger did not block tampering")
	}
}

func TestSQLRecorderConcurrentAppends(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	recorder, _ := newRecorder(t, now)
	ctx := context.Background()
	const writers = 20
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- recorder.Append(ctx, baseEvent(now))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	count, err := recorder.Count(ctx)
	if err != nil || count != writers {
		t.Fatalf("count = %d, %v", count, err)
	}
}

func TestSQLRecorderRetention(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	recorder, _ := newRecorder(t, now)
	ctx := context.Background()
	old := baseEvent(now.Add(-48 * time.Hour))
	if err := recorder.Append(ctx, old); err != nil {
		t.Fatal(err)
	}
	recent := baseEvent(now.Add(-time.Minute))
	if err := recorder.Append(ctx, recent); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.PurgeBefore(ctx, now.Add(-30*time.Minute), "admin-1", "retain-1", 10); !errors.Is(err, audit.ErrRetentionTooRecent) {
		t.Fatalf("err = %v, want ErrRetentionTooRecent", err)
	}
	if _, err := recorder.PurgeBefore(ctx, now.Add(-24*time.Hour), "admin-1", "retain-1", 0); !errors.Is(err, audit.ErrInvalidBatch) {
		t.Fatalf("err = %v, want ErrInvalidBatch", err)
	}
	removed, err := recorder.PurgeBefore(ctx, now.Add(-24*time.Hour), "admin-1", "retain-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	page, err := recorder.Query(ctx, audit.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("total = %d, want recent + retention marker", page.Total)
	}
	markers, err := recorder.Query(ctx, audit.Query{Action: audit.ActionRetention})
	if err != nil || markers.Total != 1 || markers.Items[0].SubjectID != "admin-1" {
		t.Fatalf("markers = %+v, %v", markers, err)
	}
}

func TestSQLRecorderInvalidQuery(t *testing.T) {
	t.Parallel()
	recorder, _ := newRecorder(t, time.Now().UTC())
	if _, err := recorder.Query(context.Background(), audit.Query{Limit: audit.MaxPageLimit + 1}); err == nil {
		t.Fatal("accepted oversized limit")
	}
	if _, err := recorder.Query(context.Background(), audit.Query{Source: "unknown"}); err == nil {
		t.Fatal("accepted unknown source filter")
	}
}
