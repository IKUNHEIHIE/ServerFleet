package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
)

func testOffer() *agentv1.TaskOffer {
	return &agentv1.TaskOffer{TaskId: "task-1", ExecutionId: "exec-1", PlanDigestSha256: strings.Repeat("a", 64), ActionId: "test.write-marker", CanonicalParametersJson: []byte(`{}`), Privilege: agentv1.Privilege_PRIVILEGE_NORMAL, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli(), TimeoutSeconds: 10, ScopeRevision: 1, ActionSchemaVersion: "1"}
}
func testActions(run func(context.Context, []byte) executionResult) map[string]agentAction {
	return map[string]agentAction{"test.write-marker": {Validate: func(b []byte) error {
		if string(b) != "{}" {
			return errExecutionRejected
		}
		return nil
	}, Run: run, FilterOutput: func(_ context.Context, b []byte) ([]byte, error) { return b, nil }}}
}

func TestAgentExecutorActualFileEffectDeduplicatedAfterReopen(t *testing.T) {
	ledger := t.TempDir()
	effect := filepath.Join(t.TempDir(), "effect")
	actions := testActions(func(context.Context, []byte) executionResult {
		if err := os.WriteFile(effect, []byte("one execution"), 0600); err != nil {
			t.Fatal(err)
		}
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
	})
	e, err := newAgentExecutor(ledger, actions)
	if err != nil {
		t.Fatal(err)
	}
	offer := testOffer()
	r, duplicate, err := e.execute(context.Background(), offer)
	if err != nil || duplicate || r.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED {
		t.Fatal(r, duplicate, err)
	}
	_ = e.Close()
	if err = os.WriteFile(effect, []byte("preserve this"), 0600); err != nil {
		t.Fatal(err)
	}
	e, err = newAgentExecutor(ledger, actions)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	_, duplicate, err = e.execute(context.Background(), offer)
	if err != nil || !duplicate {
		t.Fatal(duplicate, err)
	}
	data, err := os.ReadFile(effect)
	if err != nil || string(data) != "preserve this" {
		t.Fatal("side effect repeated", err)
	}
	offer.ScopeRevision++
	if _, _, err = e.execute(context.Background(), offer); err == nil {
		t.Fatal("different offer reused ID")
	}
}

func TestAgentExecutorConcurrentDuplicateAndCrashReservation(t *testing.T) {
	var count atomic.Int32
	e, err := newAgentExecutor(t.TempDir(), testActions(func(context.Context, []byte) executionResult {
		count.Add(1)
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	offer := testOffer()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() { _, _, _ = e.execute(context.Background(), offer) })
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal(count.Load())
	}
	crashOffer := testOffer()
	crashOffer.ExecutionId = "exec-crash"
	if err = e.ledger.Mkdir("exec-crash", 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(executionClaim{crashOffer.TaskId, crashOffer.ExecutionId, offerDigest(crashOffer)})
	if err = e.ledger.WriteFile("exec-crash/claim.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	r, duplicate, err := e.execute(context.Background(), crashOffer)
	if err != nil || !duplicate || r.Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN || count.Load() != 1 {
		t.Fatal(r, duplicate, err, count.Load())
	}
}

func TestAgentExecutorRejectsUnsafeOffersBeforeSideEffect(t *testing.T) {
	var count int
	e, _ := newAgentExecutor(t.TempDir(), testActions(func(context.Context, []byte) executionResult { count++; return executionResult{} }))
	defer e.Close()
	for _, change := range []func(*agentv1.TaskOffer){func(o *agentv1.TaskOffer) { o.Privilege = agentv1.Privilege_PRIVILEGE_ROOT }, func(o *agentv1.TaskOffer) { o.ActionId = "shell" }, func(o *agentv1.TaskOffer) { o.ExecutionId = "../escape" }, func(o *agentv1.TaskOffer) { o.ExpiresAtUnixMs = time.Now().UnixMilli() }, func(o *agentv1.TaskOffer) { o.ScopeRevision = 0 }, func(o *agentv1.TaskOffer) { o.PluginId = "xray-manager" }, func(o *agentv1.TaskOffer) { o.CanonicalParametersJson = []byte(`{"command":"root"}`) }} {
		offer := testOffer()
		change(offer)
		if _, _, err := e.execute(context.Background(), offer); err == nil {
			t.Fatal("unsafe offer accepted")
		}
	}
	if count != 0 {
		t.Fatal(count)
	}
}

func TestAgentExecutorLostCompletionRemainsUnknown(t *testing.T) {
	dir := t.TempDir()
	e, _ := newAgentExecutor(dir, testActions(func(context.Context, []byte) executionResult {
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED}
	}))
	defer e.Close()
	offer := testOffer()
	if err := e.ledger.Mkdir(offer.ExecutionId, 0700); err != nil {
		t.Fatal(err)
	}
	// A partial/torn claim fails closed; it never reruns the capability.
	if err := e.ledger.WriteFile(offer.ExecutionId+"/claim.json", []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, duplicate, err := e.execute(context.Background(), offer); err == nil || !duplicate {
		t.Fatal(duplicate, err)
	}
}

func TestAgentExecutorRechecksExpiryBeforeActionAndBoundsRecords(t *testing.T) {
	var calls int
	e, err := newAgentExecutor(t.TempDir(), testActions(func(context.Context, []byte) executionResult { calls++; return executionResult{} }))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	offer := testOffer()
	now := time.Now()
	checks := 0
	e.now = func() time.Time {
		checks++
		if checks == 1 {
			return now
		}
		return now.Add(2 * time.Minute)
	}
	if _, _, err = e.execute(context.Background(), offer); err == nil || calls != 0 {
		t.Fatal("expired while claiming then executed", err, calls)
	}
	if err = e.ledger.WriteFile("too-large", make([]byte, 2048), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = e.readRecord("too-large", 1024); err == nil {
		t.Fatal("oversized ledger record accepted")
	}
}
