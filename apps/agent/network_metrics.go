package main

import (
	"crypto/sha256"
	"encoding/hex"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"sort"
	"strconv"
	"strings"
)

// These are host interface counters, not provider billing. Interface changes and
// resets are reported to the Controller rather than presented as monthly usage.
func networkMetricPoints(observed int64) []*agentv1.MetricPoint {
	raw, e := readBoundedProcFile("/proc/net/dev")
	if e != nil {
		return nil
	}
	rx, tx, interfaces, e := parseProcNetDev(raw)
	if e != nil {
		return nil
	}
	return []*agentv1.MetricPoint{
		{Name: "network.rx_bytes", Value: float64(rx), Unit: "bytes", ObservedAtUnixMs: observed},
		{Name: "network.tx_bytes", Value: float64(tx), Unit: "bytes", ObservedAtUnixMs: observed},
		{Name: "network.interfaces." + interfaces, Value: 1, Unit: "set", ObservedAtUnixMs: observed},
	}
}
func parseProcNetDev(raw []byte) (uint64, uint64, string, error) {
	var rx, tx uint64
	names := []string{}
	seen := map[string]bool{}
	if len(raw) > 1<<20 {
		return 0, 0, "", errMetricsUnavailable
	}
	for _, line := range strings.Split(string(raw), "\n") {
		a, b, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name := strings.TrimSpace(a)
		if name == "lo" {
			continue
		}
		if name == "" || len(name) > 64 || strings.ContainsAny(name, " \t\r\n/\\") || seen[name] {
			return 0, 0, "", errMetricsUnavailable
		}
		seen[name] = true
		fields := strings.Fields(b)
		if len(fields) != 16 {
			return 0, 0, "", errMetricsUnavailable
		}
		in, e := strconv.ParseUint(fields[0], 10, 64)
		if e != nil {
			return 0, 0, "", errMetricsUnavailable
		}
		out, e := strconv.ParseUint(fields[8], 10, 64)
		if e != nil {
			return 0, 0, "", errMetricsUnavailable
		}
		const max = uint64(1<<53 - 1)
		if in > max || out > max || rx > max-in || tx > max-out {
			return 0, 0, "", errMetricsUnavailable
		}
		rx += in
		tx += out
		names = append(names, name)
		if len(names) > 128 {
			return 0, 0, "", errMetricsUnavailable
		}
	}
	if len(names) == 0 {
		return 0, 0, "", errMetricsUnavailable
	}
	sort.Strings(names)
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	return rx, tx, hex.EncodeToString(sum[:]), nil
}
