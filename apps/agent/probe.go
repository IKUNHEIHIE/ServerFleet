package main

import (
	"context"
	"encoding/json"
	"errors"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/netprobe"
	"serverfleet.local/serverfleet/internal/transport"
	"time"
)

// A fixed in-process read-only capability. No shell, arbitrary method, headers,
// request body, custom CA or user-provided network allowlist is accepted.
func publicProbeAction() agentAction {
	return agentAction{Validate: func(raw []byte) error { _, e := transport.ParseProbeParameters(raw); return e }, Run: func(ctx context.Context, raw []byte) executionResult {
		p, e := transport.ParseProbeParameters(raw)
		if e != nil {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_FAILED}
		}
		probe, _ := netprobe.NewNetProber(netprobe.NetProberConfig{})
		bounded, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutSeconds)*time.Second)
		defer cancel()
		spec := netprobe.TargetSpec{Kind: p.Kind, Target: p.Target, Timeout: time.Duration(p.TimeoutSeconds) * time.Second}
		var result netprobe.ProbeResult
		switch p.Kind {
		case netprobe.KindHTTP:
			result, e = probe.HTTP(bounded, spec)
		case netprobe.KindTCP:
			result, e = probe.TCP(bounded, spec)
		case netprobe.KindTLS:
			result, e = probe.TLS(bounded, spec)
		}
		if ctx.Err() != nil {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
		receipt := transport.ProbeReceipt{Version: 1, RequestKey: p.RequestKey, Outcome: "down", Reason: "request_failed", StatusCode: result.StatusCode}
		if result.Latency != nil {
			ms := result.Latency.Milliseconds()
			receipt.LatencyMs = &ms
		}
		if result.Healthy && e == nil {
			receipt.Outcome = "up"
			receipt.Reason = ""
		} else if e == nil {
			receipt.Reason = "http_status"
		} else {
			var known *netprobe.ProbeError
			if errors.As(e, &known) {
				receipt.Reason = known.Code
			}
			if errors.Is(e, netprobe.ErrTargetForbidden) || receipt.Reason == netprobe.ProbeCodeTargetForbidden || errors.Is(e, netprobe.ErrInvalidTarget) {
				receipt.Outcome = "unknown"
				receipt.Reason = "target_forbidden"
			}
		}
		output, _ := json.Marshal(receipt)
		if _, e = transport.ParseProbeReceipt(output); e != nil {
			return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN}
		}
		return executionResult{Result: agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED, Output: output}
	}, FilterOutput: func(_ context.Context, raw []byte) ([]byte, error) {
		_, e := transport.ParseProbeReceipt(raw)
		return raw, e
	}}
}
