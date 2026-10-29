package tasks_test

import (
	"errors"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/tasks"
)

var waitNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func mustWaitWindow(t *testing.T, createdAt time.Time, wait time.Duration) tasks.WaitWindow {
	t.Helper()
	window, err := tasks.NewWaitWindow(createdAt, wait)
	if err != nil {
		t.Fatalf("NewWaitWindow returned error: %v", err)
	}
	return window
}

func TestDefaultWaitWindowIsFifteenMinutes(t *testing.T) {
	window, err := tasks.NewDefaultWaitWindow(waitNow)
	if err != nil {
		t.Fatalf("NewDefaultWaitWindow returned error: %v", err)
	}
	if !window.CreatedAt().Equal(waitNow) {
		t.Fatalf("createdAt = %v, want %v", window.CreatedAt(), waitNow)
	}
	wantDeadline := waitNow.Add(15 * time.Minute)
	if !window.Deadline().Equal(wantDeadline) {
		t.Fatalf("deadline = %v, want %v", window.Deadline(), wantDeadline)
	}
	if tasks.DefaultUnstartedOfflineWait != 15*time.Minute {
		t.Fatalf("default wait = %v, want 15m", tasks.DefaultUnstartedOfflineWait)
	}
}

func TestUnstartedWaitOutcomeBoundaries(t *testing.T) {
	window, err := tasks.NewDefaultWaitWindow(waitNow)
	if err != nil {
		t.Fatal(err)
	}
	deadline := window.Deadline()

	before := mustEvaluate(t, window, deadline.Add(-time.Nanosecond), tasks.StartStateUnstarted)
	if before != tasks.WaitPending {
		t.Fatalf("before deadline outcome = %v, want pending", before)
	}

	exactly := mustEvaluate(t, window, deadline, tasks.StartStateUnstarted)
	if exactly != tasks.WaitExpired {
		t.Fatalf("exactly at deadline outcome = %v, want expired", exactly)
	}

	after := mustEvaluate(t, window, deadline.Add(time.Nanosecond), tasks.StartStateUnstarted)
	if after != tasks.WaitExpired {
		t.Fatalf("after deadline outcome = %v, want expired", after)
	}

	start := mustEvaluate(t, window, waitNow, tasks.StartStateUnstarted)
	if start != tasks.WaitPending {
		t.Fatalf("at creation outcome = %v, want pending", start)
	}
}

func TestStartedTasksAreNotApplicable(t *testing.T) {
	window, err := tasks.NewDefaultWaitWindow(waitNow)
	if err != nil {
		t.Fatal(err)
	}
	deadline := window.Deadline()
	for _, now := range []time.Time{
		waitNow,
		deadline.Add(-time.Nanosecond),
		deadline,
		deadline.Add(time.Hour),
	} {
		outcome := mustEvaluate(t, window, now, tasks.StartStateStarted)
		if outcome != tasks.WaitNotApplicable {
			t.Fatalf("started outcome at %v = %v, want not_applicable", now, outcome)
		}
	}
}

func TestNewWaitWindowRejectsBadTimesAndDurations(t *testing.T) {
	if _, err := tasks.NewWaitWindow(time.Time{}, time.Minute); !errors.Is(err, tasks.ErrInvalidCreatedAt) {
		t.Fatalf("zero creation error = %v, want ErrInvalidCreatedAt", err)
	}
	if _, err := tasks.NewDefaultWaitWindow(time.Time{}); !errors.Is(err, tasks.ErrInvalidCreatedAt) {
		t.Fatalf("default zero creation error = %v, want ErrInvalidCreatedAt", err)
	}
	for _, wait := range []time.Duration{0, -time.Nanosecond} {
		if _, err := tasks.NewWaitWindow(waitNow, wait); !errors.Is(err, tasks.ErrInvalidWaitDuration) {
			t.Fatalf("wait %v error = %v, want ErrInvalidWaitDuration", wait, err)
		}
	}
}

func TestEvaluateRejectsBadReferenceTimeWindowAndState(t *testing.T) {
	window, err := tasks.NewDefaultWaitWindow(waitNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := window.Evaluate(time.Time{}, tasks.StartStateUnstarted); !errors.Is(err, tasks.ErrInvalidReferenceTime) {
		t.Fatalf("zero now error = %v, want ErrInvalidReferenceTime", err)
	}
	if _, err := window.Evaluate(time.Time{}, tasks.StartStateStarted); !errors.Is(err, tasks.ErrInvalidReferenceTime) {
		t.Fatalf("zero now started error = %v, want ErrInvalidReferenceTime", err)
	}
	if _, err := (tasks.WaitWindow{}).Evaluate(waitNow, tasks.StartStateUnstarted); !errors.Is(err, tasks.ErrInvalidWindow) {
		t.Fatalf("zero window error = %v, want ErrInvalidWindow", err)
	}
	if _, err := (tasks.WaitWindow{}).Evaluate(waitNow, tasks.StartStateStarted); !errors.Is(err, tasks.ErrInvalidWindow) {
		t.Fatalf("zero window started error = %v, want ErrInvalidWindow", err)
	}
	if _, err := window.Evaluate(waitNow, tasks.StartState("unknown")); !errors.Is(err, tasks.ErrUnknownStartState) {
		t.Fatalf("unknown state error = %v, want ErrUnknownStartState", err)
	}
}

func TestCustomWaitDuration(t *testing.T) {
	window := mustWaitWindow(t, waitNow, 5*time.Minute)
	if want := waitNow.Add(5 * time.Minute); !window.Deadline().Equal(want) {
		t.Fatalf("deadline = %v, want %v", window.Deadline(), want)
	}
	if outcome := mustEvaluate(t, window, window.Deadline().Add(-time.Second), tasks.StartStateUnstarted); outcome != tasks.WaitPending {
		t.Fatalf("custom window outcome = %v, want pending", outcome)
	}
	if outcome := mustEvaluate(t, window, window.Deadline(), tasks.StartStateUnstarted); outcome != tasks.WaitExpired {
		t.Fatalf("custom deadline outcome = %v, want expired", outcome)
	}
}

func mustEvaluate(t *testing.T, window tasks.WaitWindow, now time.Time, state tasks.StartState) tasks.WaitOutcome {
	t.Helper()
	outcome, err := window.Evaluate(now, state)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	return outcome
}
