package main

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	agentruntime "serverfleet.local/serverfleet/apps/agent/runtime"
)

// No enrollment, identity replacement or task replay in this loop. Every link
// reopens the durable ledger and participates in the existing execution sync.
// Eight consecutive failures stop the process; a healthy 60s link resets that
// budget. Supervisors must not turn this into an unbounded restart policy.
func agentServe(ctx context.Context, r agentruntime.ConnectRequest) error {
	return serveAgentLinks(ctx, r, agentConnectOnce, func(ctx context.Context, d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	}, time.Now)
}

func serveAgentLinks(ctx context.Context, r agentruntime.ConnectRequest, connect func(context.Context, agentruntime.ConnectRequest) (agentruntime.ConnectResult, error), wait func(context.Context, time.Duration) error, now func() time.Time) error {
	if ctx == nil || connect == nil || wait == nil || now == nil {
		return errAgentConnect
	}
	r.Persistent = true
	r.WaitTaskMillis = 0
	for failures := 0; failures < 8; {
		if ctx.Err() != nil {
			return nil
		}
		started := now()
		_, err := connect(ctx, r)
		if errors.Is(err, errAgentIdentityRotated) {
			failures = 0
			continue
		}
		if errors.Is(err, errAgentWorkersUnknown) {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		if now().Sub(started) >= time.Minute {
			failures = 0
		}
		failures++
		if failures == 8 {
			return errAgentConnect
		}
		delay := min(time.Second<<(failures-1), 30*time.Second)
		var jitter [1]byte
		if _, err := rand.Read(jitter[:]); err != nil {
			return errAgentConnect
		}
		delay += time.Duration(jitter[0]) * time.Millisecond
		if wait(ctx, delay) != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errAgentConnect
		}
	}
	return errAgentConnect
}
