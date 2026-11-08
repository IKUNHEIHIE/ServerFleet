package certificates_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/storage"
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
	mu      sync.Mutex
	calls   []cleanupCall
	err     error
	started chan struct{}
	release chan struct{}
}

func (f *fakeCleaner) DeleteChallengeRecord(_ context.Context, subjectID, requestID, fqdn, recordID, expectedValue string) error {
	f.mu.Lock()
	f.calls = append(f.calls, cleanupCall{subjectID, requestID, fqdn, recordID, expectedValue})
	started, release := f.started, f.release
	f.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	f.mu.Lock()
	defer f.mu.Unlock()
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
	db         *storage.DB
}

func newCleanupFixture(t *testing.T, maxAttempts int) cleanupFixture {
	t.Helper()
	db := testkit.NewDB(t, certificates.CleanupMigrations(), certificates.CleanupClaimMigrations(), audit.Migrations())
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
	return cleanupFixture{journal: journal, cleaner: cleaner, authorizer: authorizer, recorder: recorder, db: db}
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
	if list[0].PlanID != "plan-1" || list[0].Domain != "app.example.com" || list[0].FQDN != "_acme-challenge.app.example.com" {
		t.Fatalf("record identity = %+v", list[0])
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
	f.cleaner.err = certificates.ErrCleanupRetryable
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
	f.cleaner.err = certificates.ErrCleanupRetryable
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

func TestCleanupJournalRetryDueUsesCurrentOriginalOwnerAuthorization(t *testing.T) {
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
	calls := f.cleaner.snapshot()
	if len(calls) != 1 || calls[0].subject != "admin-1" {
		t.Fatalf("invented cleanup execution subject: %+v", calls)
	}
	page, err := f.recorder.Query(context.Background(), audit.Query{Action: certificates.ActionCleanupRetry})
	if err != nil || len(page.Items) == 0 {
		t.Fatal("missing automatic audit", err)
	}
	for _, e := range page.Items {
		if e.SubjectID != "admin-1" || e.Source != audit.SourceSystem {
			t.Fatalf("wrong delegation provenance: %+v", e)
		}
	}
}

func TestCleanupJournalConcurrentRetryDeletesOnce(t *testing.T) {
	f := newCleanupFixture(t, 3)
	records, err := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords()[:1])
	if err != nil {
		t.Fatal(err)
	}
	id := records[0].ID
	f.cleaner.started = make(chan struct{}, 1)
	release := make(chan struct{})
	f.cleaner.release = release
	first := make(chan error, 1)
	go func() {
		_, err := f.journal.Retry(context.Background(), "admin-1", "req-first", id)
		first <- err
	}()
	<-f.cleaner.started
	if _, err := f.journal.Retry(context.Background(), "admin-1", "req-second", id); !errors.Is(err, certificates.ErrCleanupBusy) {
		t.Fatalf("concurrent retry err = %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if calls := f.cleaner.snapshot(); len(calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(calls))
	}
	list, _ := f.journal.List(context.Background(), "admin-1")
	if list[0].State != certificates.CleanupResolved || list[0].Attempts != 1 {
		t.Fatalf("final state = %+v", list[0])
	}
	page, err := f.recorder.Query(context.Background(), audit.Query{Action: certificates.ActionCleanupRetry, Result: "denied"})
	if err != nil || page.Total < 1 {
		t.Fatalf("busy denial audit = %+v, %v", page, err)
	}
}

func TestCleanupJournalCancelWinsOverInFlightSuccess(t *testing.T) {
	f := newCleanupFixture(t, 3)
	records, err := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords()[:1])
	if err != nil {
		t.Fatal(err)
	}
	id := records[0].ID
	f.cleaner.started = make(chan struct{}, 1)
	release := make(chan struct{})
	f.cleaner.release = release
	first := make(chan error, 1)
	go func() {
		_, err := f.journal.Retry(context.Background(), "admin-1", "req-first", id)
		first <- err
	}()
	<-f.cleaner.started
	canceled, err := f.journal.Cancel(context.Background(), "admin-1", "req-cancel", id)
	if err != nil || canceled.State != certificates.CleanupCanceled {
		t.Fatalf("cancel = %+v, err = %v", canceled, err)
	}
	close(release)
	if err := <-first; !errors.Is(err, certificates.ErrCleanupState) {
		t.Fatalf("late retry err = %v", err)
	}
	list, _ := f.journal.List(context.Background(), "admin-1")
	if list[0].State != certificates.CleanupCanceled {
		t.Fatalf("canceled state was overwritten: %+v", list[0])
	}
	if calls := f.cleaner.snapshot(); len(calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(calls))
	}
	if page, err := f.recorder.Query(context.Background(), audit.Query{RequestID: "req-first", Result: "denied"}); err != nil || page.Total < 1 {
		t.Fatalf("superseded audit = %+v, %v", page, err)
	}
}

func TestCleanupJournalRetryDueDoesNotDuplicate(t *testing.T) {
	f := newCleanupFixture(t, 3)
	if _, err := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords()[:1]); err != nil {
		t.Fatal(err)
	}
	f.cleaner.started = make(chan struct{}, 1)
	release := make(chan struct{})
	f.cleaner.release = release
	first := make(chan struct {
		count int
		err   error
	}, 1)
	go func() {
		count, err := f.journal.RetryDue(context.Background(), 10)
		first <- struct {
			count int
			err   error
		}{count, err}
	}()
	<-f.cleaner.started
	if count, err := f.journal.RetryDue(context.Background(), 10); err != nil || count != 0 {
		t.Fatalf("second due run = %d, %v", count, err)
	}
	close(release)
	result := <-first
	if result.err != nil || result.count != 1 {
		t.Fatalf("first due run = %d, %v", result.count, result.err)
	}
	if calls := f.cleaner.snapshot(); len(calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(calls))
	}
}

func TestCleanupJournalStaleClaimIsUnknownNotReplayed(t *testing.T) {
	f := newCleanupFixture(t, 3)
	records, err := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords()[:1])
	if err != nil {
		t.Fatal(err)
	}
	id := records[0].ID
	stale := time.Now().UTC().Add(-time.Minute)
	if _, err := f.db.Exec(context.Background(), `UPDATE certificate_dns_cleanup
		SET state = 'retrying', attempts = 1, claim_token = 'stale-token', lease_until = ? WHERE id = ?`,
		stale.Format(time.RFC3339Nano), id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.journal.Retry(context.Background(), "admin-1", "req-retry", id); !errors.Is(err, certificates.ErrCleanupUnknown) {
		t.Fatalf("stale retry err = %v", err)
	}
	list, _ := f.journal.List(context.Background(), "admin-1")
	if list[0].State != certificates.CleanupUnknown || list[0].LastErrorCode != "provider_outcome_unknown" {
		t.Fatalf("stale state = %+v", list[0])
	}
	if calls := f.cleaner.snapshot(); len(calls) != 0 {
		t.Fatalf("stale claim was replayed: %+v", calls)
	}
	if _, err := f.journal.Retry(context.Background(), "admin-1", "req-retry-2", id); !errors.Is(err, certificates.ErrCleanupUnknown) {
		t.Fatalf("unknown retry err = %v", err)
	}
	if _, err := f.journal.Cancel(context.Background(), "admin-1", "req-cancel", id); err != nil {
		t.Fatal(err)
	}
	list, _ = f.journal.List(context.Background(), "admin-1")
	if list[0].State != certificates.CleanupCanceled {
		t.Fatalf("unknown cancel state = %+v", list[0])
	}
}

func TestCleanupJournalRecoverStaleClaimsMarksUnknown(t *testing.T) {
	f := newCleanupFixture(t, 3)
	records, err := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords()[:1])
	if err != nil {
		t.Fatal(err)
	}
	stale := time.Now().UTC().Add(-time.Minute)
	if _, err := f.db.Exec(context.Background(), `UPDATE certificate_dns_cleanup
		SET state = 'retrying', attempts = 2, claim_token = 'dead-claim', lease_until = ? WHERE id = ?`,
		stale.Format(time.RFC3339Nano), records[0].ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := f.journal.RecoverStaleClaims(context.Background(), 10)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered = %d, err = %v", recovered, err)
	}
	list, _ := f.journal.List(context.Background(), "admin-1")
	if list[0].State != certificates.CleanupUnknown || list[0].Attempts != 2 || list[0].LastErrorCode != "provider_outcome_unknown" {
		t.Fatalf("recovered = %+v", list[0])
	}
	if calls := f.cleaner.snapshot(); len(calls) != 0 {
		t.Fatalf("recovery called provider: %+v", calls)
	}
	if recovered, err := f.journal.RecoverStaleClaims(context.Background(), 10); err != nil || recovered != 0 {
		t.Fatalf("second recovery = %d, %v", recovered, err)
	}
}

func TestCleanupClaimMigrationPreservesExistingRecords(t *testing.T) {
	db := testkit.NewDB(t, certificates.CleanupMigrations(), audit.Migrations())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(context.Background(), `INSERT INTO certificate_dns_cleanup
		(subject_id, request_id, plan_id, domain, fqdn, record_id, record_value, state,
		 attempts, last_error_code, next_attempt_at, created_at, updated_at)
		VALUES ('admin-1', 'req-1', 'plan-1', 'app.example.com', '_acme-challenge.app.example.com',
			'record-1', 'txt-1', 'retrying', 2, 'provider_delete_failed', NULL, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	migrator, err := storage.NewMigrator(certificates.CleanupMigrations(), certificates.CleanupClaimMigrations(), audit.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	report, err := migrator.Apply(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Applied) != 1 || report.Applied[0] != "ds/0063_cleanup_claim" {
		t.Fatalf("applied = %+v", report.Applied)
	}
	var (
		state    string
		attempts int
		claim    string
		lease    *string
	)
	if err := db.QueryRow(context.Background(), `SELECT state, attempts, claim_token, lease_until
		FROM certificate_dns_cleanup WHERE fqdn = ?`, "_acme-challenge.app.example.com").
		Scan(&state, &attempts, &claim, &lease); err != nil {
		t.Fatal(err)
	}
	if state != "retrying" || attempts != 2 || claim != "" || lease != nil {
		t.Fatalf("migrated row = %s/%d/%q/%v", state, attempts, claim, lease)
	}
	if _, err := db.Exec(context.Background(), `UPDATE certificate_dns_cleanup SET state = 'unknown' WHERE fqdn = ?`,
		"_acme-challenge.app.example.com"); err != nil {
		t.Fatalf("unknown state rejected: %v", err)
	}
}

func TestCleanupHTTPRoutes(t *testing.T) {
	f := newCleanupFixture(t, 3)
	records, err := f.journal.RecordFailure(context.Background(), "admin-1", "req-1", "plan-1", "app.example.com", testCleanupRecords()[:1])
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := certificates.RegisterCleanupHTTP(mux, f.journal, func(*http.Request) (certificates.CleanupPrincipal, error) {
		return certificates.CleanupPrincipal{SubjectID: "admin-1", RequestID: "http-request"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	response, err := http.Get(server.URL + "/api/v1/certificates/cleanup")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("list response = %v, err = %v", response, err)
	}
	var list []struct {
		RecordID string `json:"recordId"`
		State    string `json:"state"`
	}
	if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if len(list) != 1 || list[0].RecordID != "record-1" || list[0].State != "pending" {
		t.Fatalf("list = %+v", list)
	}
	retry, err := http.Post(server.URL+"/api/v1/certificates/cleanup/"+strconv.FormatInt(records[0].ID, 10)+"/retry", "application/json", nil)
	if err != nil || retry.StatusCode != http.StatusOK {
		t.Fatalf("retry response = %v, err = %v", retry, err)
	}
	var retried struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(retry.Body).Decode(&retried); err != nil {
		t.Fatal(err)
	}
	_ = retry.Body.Close()
	if retried.State != "resolved" {
		t.Fatalf("retried = %+v", retried)
	}
	cancel, err := http.Post(server.URL+"/api/v1/certificates/cleanup/"+strconv.FormatInt(records[0].ID, 10)+"/cancel", "application/json", nil)
	if err != nil || cancel.StatusCode != http.StatusConflict {
		t.Fatalf("cancel resolved response = %v, err = %v", cancel, err)
	}
	_ = cancel.Body.Close()
}

func TestCleanupBackgroundRevokedOwnerNeverCallsProvider(t *testing.T) {
	f := newCleanupFixture(t, 3)
	records, err := f.journal.RecordFailure(context.Background(), "admin-1", "create-due", "plan-1", "app.example.com", testCleanupRecords()[:1])
	if err != nil {
		t.Fatal(err)
	}
	f.authorizer.mu.Lock()
	f.authorizer.deny = true
	f.authorizer.mu.Unlock()
	count, err := f.journal.RetryDue(context.Background(), 10)
	if err != nil || count != 0 || len(f.cleaner.snapshot()) != 0 {
		t.Fatalf("revoked owner executed: %d %v", count, err)
	}
	var state string
	if err = f.db.QueryRow(context.Background(), `SELECT state FROM certificate_dns_cleanup WHERE id=?`, records[0].ID).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("denial changed state: %s %v", state, err)
	}
}

func TestCleanupUnclassifiedProviderFailureBecomesUnknownAndNeverReplays(t *testing.T) {
	f := newCleanupFixture(t, 3)
	ctx := context.Background()
	records, err := f.journal.RecordFailure(ctx, "admin-1", "create", "plan-1", "app.example.com", testCleanupRecords()[:1])
	if err != nil {
		t.Fatal(err)
	}
	f.cleaner.err = errors.New("connection reset after request transmission")
	record, err := f.journal.Retry(ctx, "admin-1", "delete", records[0].ID)
	if err != nil || record.State != certificates.CleanupUnknown || !record.NextAttemptAt.IsZero() {
		t.Fatalf("unsafe retry classified: %+v %v", record, err)
	}
	f.cleaner.err = nil
	count, err := f.journal.RetryDue(ctx, 10)
	if err != nil || count != 0 || len(f.cleaner.snapshot()) != 1 {
		t.Fatalf("unknown effect replayed: %d %v", count, err)
	}
	if _, err = f.journal.Retry(ctx, "admin-1", "retry-unknown", record.ID); !errors.Is(err, certificates.ErrCleanupUnknown) {
		t.Fatal(err)
	}
}
