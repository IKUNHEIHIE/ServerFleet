package main

import (
	"context"
	"testing"
	"time"

	agentruntime "serverfleet.local/serverfleet/apps/agent/runtime"
)

func TestDaemonBoundedReconnectAndCancellation(t *testing.T) {
	calls, waits := 0, 0
	err := serveAgentLinks(context.Background(), agentruntime.ConnectRequest{}, func(_ context.Context, r agentruntime.ConnectRequest) (agentruntime.ConnectResult, error) {
		calls++
		if !r.Persistent || r.WaitTaskMillis != 0 {
			t.Error("not persistent")
		}
		return agentruntime.ConnectResult{}, errAgentConnect
	}, func(_ context.Context, d time.Duration) error {
		waits++
		if d < time.Second || d > 31*time.Second {
			t.Error("unbounded backoff")
		}
		return nil
	}, time.Now)
	if err == nil || calls != 8 || waits != 7 {
		t.Fatal(calls, waits, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err = serveAgentLinks(ctx, agentruntime.ConnectRequest{}, func(context.Context, agentruntime.ConnectRequest) (agentruntime.ConnectResult, error) {
		cancel()
		return agentruntime.ConnectResult{}, context.Canceled
	}, func(context.Context, time.Duration) error { t.Fatal("retry on shutdown"); return nil }, time.Now)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDaemonDoesNotReconnectWithUnstoppedWorker(t *testing.T) {
	err := serveAgentLinks(context.Background(), agentruntime.ConnectRequest{}, func(context.Context, agentruntime.ConnectRequest) (agentruntime.ConnectResult, error) {
		return agentruntime.ConnectResult{}, errAgentWorkersUnknown
	}, func(context.Context, time.Duration) error { t.Fatal("reconnected while worker may run"); return nil }, time.Now)
	if err != errAgentWorkersUnknown {
		t.Fatal(err)
	}
}
