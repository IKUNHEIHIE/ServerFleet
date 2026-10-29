package testkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/audit/testkit"
)

var testNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func newValidator(t *testing.T) *audit.Validator {
	t.Helper()
	validator, err := audit.NewValidator([]string{"nodes.read", "tasks.submit"}, audit.DefaultLimits())
	if err != nil {
		t.Fatalf("NewValidator returned error: %v", err)
	}
	return validator
}

func newRecorder(t *testing.T) *testkit.MemoryRecorder {
	t.Helper()
	recorder, err := testkit.NewMemoryRecorder(newValidator(t), func() time.Time { return testNow })
	if err != nil {
		t.Fatalf("NewMemoryRecorder returned error: %v", err)
	}
	return recorder
}

func testEvent(requestID string) audit.Event {
	return audit.Event{
		SubjectID: "user-1",
		Action:    "nodes.read",
		Result:    "success",
		RequestID: requestID,
		At:        testNow.Add(-time.Minute),
	}
}

func TestAppendAndSnapshot(t *testing.T) {
	recorder := newRecorder(t)
	if err := recorder.Append(context.Background(), testEvent("req-1")); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	if err := recorder.Append(context.Background(), testEvent("req-2")); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	if recorder.Len() != 2 {
		t.Fatalf("Len = %d, want 2", recorder.Len())
	}
	snapshot := recorder.Snapshot()
	if len(snapshot) != 2 || snapshot[0].RequestID != "req-1" || snapshot[1].RequestID != "req-2" {
		t.Fatalf("snapshot = %+v, want append order req-1/req-2", snapshot)
	}

	snapshot[0].Action = "tampered"
	if recorder.Snapshot()[0].Action != "nodes.read" {
		t.Fatal("Snapshot returned an aliased record")
	}
}

func TestAppendRejectsInvalidEventWithoutWriting(t *testing.T) {
	recorder := newRecorder(t)
	event := testEvent("req-1")
	event.Action = "secret-action-token"
	err := recorder.Append(context.Background(), event)
	if !errors.Is(err, audit.ErrInvalidEvent) {
		t.Fatalf("error = %v, want ErrInvalidEvent", err)
	}
	if recorder.Len() != 0 {
		t.Fatalf("Len = %d after rejected append, want 0", recorder.Len())
	}
	if err != nil && strings.Contains(err.Error(), "secret-action-token") {
		t.Fatalf("error leaked rejected value: %v", err)
	}
}

func TestAppendUsesInjectedClock(t *testing.T) {
	recorder := newRecorder(t)
	event := testEvent("req-1")
	event.At = testNow.Add(2 * time.Hour)
	if err := recorder.Append(context.Background(), event); !errors.Is(err, audit.ErrInvalidEvent) {
		t.Fatalf("future event error = %v, want ErrInvalidEvent", err)
	}
	if recorder.Len() != 0 {
		t.Fatalf("Len = %d after future event, want 0", recorder.Len())
	}
}

func TestAppendCanceledContextDoesNotWrite(t *testing.T) {
	recorder := newRecorder(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := recorder.Append(ctx, testEvent("req-1")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Append error = %v, want context.Canceled", err)
	}

	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	if err := recorder.Append(expired, testEvent("req-2")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired Append error = %v, want context.DeadlineExceeded", err)
	}
	if recorder.Len() != 0 {
		t.Fatalf("Len = %d after canceled appends, want 0", recorder.Len())
	}
}

func TestDuplicateRequestIDAndIdenticalEventsAreRetained(t *testing.T) {
	recorder := newRecorder(t)
	first := testEvent("req-1")
	second := testEvent("req-1")
	second.At = testNow.Add(-2 * time.Minute)
	for _, event := range []audit.Event{first, second, first} {
		if err := recorder.Append(context.Background(), event); err != nil {
			t.Fatalf("Append returned error: %v", err)
		}
	}
	if recorder.Len() != 3 {
		t.Fatalf("Len = %d, want 3", recorder.Len())
	}
	page, err := recorder.View().Query(audit.Query{RequestID: "req-1"})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(page.Items) != 3 || page.Total != 3 {
		t.Fatalf("request page items/total = %d/%d, want 3/3", len(page.Items), page.Total)
	}
}

func TestConcurrentAppend(t *testing.T) {
	recorder := newRecorder(t)
	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- recorder.Append(context.Background(), testEvent(fmt.Sprintf("req-%d", i)))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Append returned error: %v", err)
		}
	}
	if recorder.Len() != workers {
		t.Fatalf("Len = %d, want %d", recorder.Len(), workers)
	}
	seen := make(map[string]struct{}, workers)
	for _, event := range recorder.Snapshot() {
		seen[event.RequestID] = struct{}{}
	}
	if len(seen) != workers {
		t.Fatalf("unique request ids = %d, want %d", len(seen), workers)
	}
}

func TestViewQueryDoesNotModifyRecorder(t *testing.T) {
	recorder := newRecorder(t)
	for i := 0; i < 3; i++ {
		if err := recorder.Append(context.Background(), testEvent(fmt.Sprintf("req-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	before := mustTestkitJSON(t, recorder.Snapshot())
	page, err := recorder.View().Query(audit.Query{})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(page.Items))
	}
	after := mustTestkitJSON(t, recorder.Snapshot())
	if string(before) != string(after) {
		t.Fatal("query mutated recorder events")
	}
	if recorder.Len() != 3 {
		t.Fatalf("Len = %d after query, want 3", recorder.Len())
	}
}

func TestAppendCancellationDuringValidationDoesNotWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder, err := testkit.NewMemoryRecorder(newValidator(t), func() time.Time {
		cancel()
		return testNow
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Append(ctx, testEvent("req-1")); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if recorder.Len() != 0 {
		t.Fatalf("Len = %d after mid-append cancellation, want 0", recorder.Len())
	}
}

func TestNewMemoryRecorderDefaultsClock(t *testing.T) {
	recorder, err := testkit.NewMemoryRecorder(newValidator(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	event := testEvent("req-1")
	event.At = time.Now().Add(-time.Minute)
	if err := recorder.Append(context.Background(), event); err != nil {
		t.Fatalf("Append with default clock returned error: %v", err)
	}
	if recorder.Len() != 1 {
		t.Fatalf("Len = %d, want 1", recorder.Len())
	}
}

func TestNewMemoryRecorderRequiresValidator(t *testing.T) {
	if _, err := testkit.NewMemoryRecorder(nil, nil); !errors.Is(err, testkit.ErrValidatorRequired) {
		t.Fatalf("error = %v, want ErrValidatorRequired", err)
	}
}

func mustTestkitJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
