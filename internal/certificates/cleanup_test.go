package certificates_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type cleanupAuthorizer struct {
	mu   sync.Mutex
	deny bool
}

func (a *cleanupAuthorizer) Authorize(context.Context, auth.Check) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deny {
		return auth.ErrDenied
	}
	return nil
}

type cleanupCall struct {
	subject, request, fqdn, recordID, value string
}

type fakeCleaner struct {
	mu    sync.Mutex
	calls []cleanupCall
	err   error
}

func (f *fakeCleaner) DeleteChallengeRecord(_ context.Context, subjectID, requestID, fqdn, recordID, expectedValue string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, cleanupCall{subjectID, requestID, fqdn, recordID, expectedValue})
	return f.err
}

func (f *fakeCleaner) snapshot() []cleanupCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]cleanupCall(nil), f.calls...)
}

type cleanupFixture struct {
	journal    *certificates.CleanupJournal
	cleaner    *fakeCleaner
	authorizer *cleanupAuthorizer
	recorder   *audit.SQLRecorder
}

func newCleanupFixture(t *testing.T, maxAttempts int) cleanupFixture {
	t.Helper()
	db := testkit.NewDB(t, certificates.CleanupMigrations(), audit.Migrations())
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(certificates.ServiceActions(), limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &cleanupAuthorizer{}
	cleaner := &fakeCleaner{}
	journal, err := certificates.NewCleanupJournal(certificates.CleanupJournalOptions{
		DB: db, Audit: recorder, Authorizer: authorizer, Cleaner: cleaner,
		MaxAttempts: maxAttempts, Backoff: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cleanupFixture{journal: journal, cleaner: cleaner, authorizer: authorizer, recorder: recorder}
}

func testCleanupRecords() []certificates.ACMEDNS01Record {
	return []certificates.ACMEDNS01Record{
		{FQDN: "_acme-challenge.app.example.com", Value: "txt-1", RecordID: "record-1"},
		{FQDN: "_acme-challenge.app.example.com", Value: "txt-1", RecordID: "record-1"},
	}
}

func TestCleanupJournalRecordsFailuresIdempotently(t *testing.T) {
	f := newCleanupFixture(t, 3)
	records, err := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].ID != records[1].ID || records[0].State != certificates.CleanupPending {
		t.Fatalf("records = %+v", records)
	}
	list, err := f.journal.List(context.Background(), "admin-1")
	if err != nil || len(list) != 1 || list[0].RecordValue != "txt-1" || list[0].RecordID != "record-1" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	page, err := f.recorder.Query(context.Background(), audit.Query{Action: certificates.ActionCleanupRecord})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
}

func TestCleanupJournalRetryBoundedAndResolved(t *testing.T) {
	f := newCleanupFixture(t, 2)
	records, err := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords()[:1])
	if err != nil {
		t.Fatal(err)
	}
	id := records[0].ID
	f.cleaner.err = errors.New("provider delete failed")
	if _, err := f.journal.Retry(context.Background(), "admin-1", "req-retry-1", id); err != nil {
		t.Fatal(err)
	}
	list, _ := f.journal.List(context.Background(), "admin-1")
	if list[0].State != certificates.CleanupRetrying || list[0].Attempts != 1 || list[0].NextAttemptAt.IsZero() {
		t.Fatalf("after failure = %+v", list[0])
	}
	f.cleaner.err = nil
	if _, err := f.journal.Retry(context.Background(), "admin-1", "req-retry-2", id); err != nil {
		t.Fatal(err)
	}
	list, _ = f.journal.List(context.Background(), "admin-1")
	if list[0].State != certificates.CleanupResolved || list[0].Attempts != 2 {
		t.Fatalf("after success = %+v", list[0])
	}
	calls := f.cleaner.snapshot()
	if len(calls) != 2 || calls[0].recordID != "record-1" || calls[0].value != "txt-1" || calls[0].fqdn != "_acme-challenge.app.example.com" {
		t.Fatalf("cleaner calls = %+v", calls)
	}
	if _, err := f.journal.Retry(context.Background(), "admin-1", "req-retry-3", id); !errors.Is(err, certificates.ErrCleanupState) {
		t.Fatalf("resolved retry err = %v", err)
	}
}

func TestCleanupJournalExhaustsRetries(t *testing.T) {
	f := newCleanupFixture(t, 2)
	records, _ := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords()[:1])
	id := records[0].ID
	f.cleaner.err = errors.New("provider delete failed")
	if _, err := f.journal.Retry(context.Background(), "admin-1", "req-retry-1", id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.journal.Retry(context.Background(), "admin-1", "req-retry-2", id); err != nil {
		t.Fatal(err)
	}
	list, _ := f.journal.List(context.Background(), "admin-1")
	if list[0].State != certificates.CleanupFailed || list[0].Attempts != 2 {
		t.Fatalf("after exhaustion = %+v", list[0])
	}
	if _, err := f.journal.Retry(context.Background(), "admin-1", "req-retry-3", id); !errors.Is(err, certificates.ErrCleanupExhausted) {
		t.Fatalf("exhausted retry err = %v", err)
	}
}

func TestCleanupJournalCancelAndAuthorization(t *testing.T) {
	f := newCleanupFixture(t, 3)
	f.authorizer.deny = true
	if _, err := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords()[:1]); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied record err = %v", err)
	}
	f.authorizer.deny = false
	records, err := f.journal.RecordFailure(context.Background(), "admin-1", "req-2", "plan-1", "app.example.com", testCleanupRecords()[:1])
	if err != nil {
		t.Fatal(err)
	}
	id := records[0].ID
	if _, err := f.journal.Cancel(context.Background(), "admin-1", "req-cancel", id); err != nil {
		t.Fatal(err)
	}
	list, _ := f.journal.List(context.Background(), "admin-1")
	if list[0].State != certificates.CleanupCanceled {
		t.Fatalf("cancel state = %+v", list[0])
	}
	if _, err := f.journal.Retry(context.Background(), "admin-1", "req-retry", id); !errors.Is(err, certificates.ErrCleanupState) {
		t.Fatalf("canceled retry err = %v", err)
	}
	if _, err := f.journal.List(context.Background(), "other"); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupJournalRetryDueUsesServiceAuthorization(t *testing.T) {
	f := newCleanupFixture(t, 3)
	records, _ := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords()[:1])
	if records[0].ID == 0 {
		t.Fatal("missing record")
	}
	attempted, err := f.journal.RetryDue(context.Background(), 10)
	if err != nil || attempted != 1 {
		t.Fatalf("attempted = %d, err = %v", attempted, err)
	}
	list, _ := f.journal.List(context.Background(), "admin-1")
	if list[0].State != certificates.CleanupResolved {
		t.Fatalf("due state = %+v", list[0])
	}
}
