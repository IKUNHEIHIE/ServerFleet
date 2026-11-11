package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"serverfleet.local/serverfleet/internal/netprobe"
	"strconv"
)

const ProbeCapability = "monitoring.probe"
const ProbeExecutionGraceSeconds = 5

const ProbeProfile = "public-http-tcp-tls-v1"

type ProbeParameters struct {
	RequestKey     string
	Kind           netprobe.Kind
	Target         string
	TimeoutSeconds int
}

func ParseProbeParameters(raw []byte) (ProbeParameters, error) {
	var m map[string]string
	var p ProbeParameters
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 8192 || d.Decode(&m) != nil || d.Decode(&struct{}{}) != io.EOF || len(m) != 5 || !FileDigest(m["requestKey"]) || m["profile"] != ProbeProfile {
		return p, ErrLinkRejected
	}
	n, e := strconv.Atoi(m["timeoutSeconds"])
	if e != nil || n < 1 || n > 30 || strconv.Itoa(n) != m["timeoutSeconds"] {
		return p, ErrLinkRejected
	}
	policy, _ := netprobe.NewTargetPolicy(nil)
	p = ProbeParameters{m["requestKey"], netprobe.Kind(m["kind"]), m["target"], n}
	if len(p.Target) > 1024 || policy.Validate(p.Kind, p.Target) != nil {
		return p, ErrLinkRejected
	}
	return p, nil
}

type ProbeReceipt struct {
	Version    int    `json:"version"`
	RequestKey string `json:"requestKey"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
	LatencyMs  *int64 `json:"latencyMs,omitempty"`
	StatusCode *int   `json:"statusCode,omitempty"`
}

func ParseProbeReceipt(raw []byte) (ProbeReceipt, error) {
	var r ProbeReceipt
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if len(raw) > 4096 || d.Decode(&r) != nil || d.Decode(&struct{}{}) != io.EOF || r.Version != 1 || !FileDigest(r.RequestKey) {
		return r, ErrLinkRejected
	}
	switch r.Outcome {
	case "up", "down", "unknown":
	default:
		return r, ErrLinkRejected
	}
	switch r.Reason {
	case "", "http_status", "invalid_target", "target_forbidden", "dns_failure", "dial_failed", "tls_failed", "timeout", "request_failed", "probe_interrupted":
	default:
		return r, ErrLinkRejected
	}
	if r.Outcome == "up" && r.Reason != "" || r.LatencyMs != nil && (*r.LatencyMs < 0 || *r.LatencyMs > 35_000) || r.StatusCode != nil && (*r.StatusCode < 100 || *r.StatusCode > 599) {
		return r, ErrLinkRejected
	}
	return r, nil
}
