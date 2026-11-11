package main

import (
	"context"
	"encoding/json"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/netprobe"
	"serverfleet.local/serverfleet/internal/transport"
	"strings"
	"testing"
	"time"
)

type syntheticProbe struct {
	result netprobe.ProbeResult
	err    error
	kind   netprobe.Kind
}

func (p *syntheticProbe) HTTP(context.Context, netprobe.TargetSpec) (netprobe.ProbeResult, error) {
	p.kind = netprobe.KindHTTP
	return p.result, p.err
}
func (p *syntheticProbe) TCP(context.Context, netprobe.TargetSpec) (netprobe.ProbeResult, error) {
	p.kind = netprobe.KindTCP
	return p.result, p.err
}
func (p *syntheticProbe) TLS(context.Context, netprobe.TargetSpec) (netprobe.ProbeResult, error) {
	p.kind = netprobe.KindTLS
	return p.result, p.err
}
func TestNodeProbeReceiptSeparatesTargetFailureFromUnknown(t *testing.T) {
	for _, kind := range []string{"http", "tcp", "tls"} {
		target := "example.com:443"
		if kind == "http" {
			target = "https://example.com/"
		}
		raw, _ := json.Marshal(map[string]string{"requestKey": strings.Repeat("a", 64), "kind": kind, "target": target, "timeoutSeconds": "5", "profile": transport.ProbeProfile})
		latency := 3 * time.Millisecond
		p := &syntheticProbe{result: netprobe.ProbeResult{Healthy: true, Latency: &latency}}
		a := probeActionUsing(p)
		if e := a.Validate(raw); e != nil {
			t.Fatal(e)
		}
		result := a.Run(context.Background(), raw)
		receipt, e := transport.ParseProbeReceipt(result.Output)
		if e != nil || result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_SUCCEEDED || receipt.Outcome != "up" || p.kind != netprobe.Kind(kind) || *receipt.LatencyMs != 3 {
			t.Fatal(result, receipt, e)
		}
		p.result.Healthy = false
		result = a.Run(context.Background(), raw)
		receipt, e = transport.ParseProbeReceipt(result.Output)
		if e != nil || receipt.Outcome != "down" {
			t.Fatal("target failure lost", receipt, e)
		}
		p.err = &netprobe.ProbeError{Code: netprobe.ProbeCodeTargetForbidden, Cause: netprobe.ErrTargetForbidden}
		result = a.Run(context.Background(), raw)
		receipt, e = transport.ParseProbeReceipt(result.Output)
		if e != nil || receipt.Outcome != "unknown" {
			t.Fatal("policy refusal became business failure", receipt, e)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result = a.Run(ctx, raw)
		if result.Result != agentv1.ExecutionResult_EXECUTION_RESULT_UNKNOWN || len(result.Output) != 0 {
			t.Fatal("canceled probe published conclusive output")
		}
	}
}
