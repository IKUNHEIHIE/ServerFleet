package main

import (
	"context"
	"encoding/hex"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseProcCPUStatAggregateLine(t *testing.T) {
	raw := []byte("cpu  1000 10 200 7000 30 0 40 20 5 7\ncpu0 1 2 3 4 5 6 7 8\nintr 12\n")
	sample, err := parseProcCPUStat(raw)
	if err != nil {
		t.Fatal(err)
	}
	if sample.total != 8300 || sample.idle != 7030 {
		t.Fatalf("sample = %+v", sample)
	}
	second := cpuTimes{total: sample.total + 200, idle: sample.idle + 150}
	if value := cpuPercent(sample, second); value != 25 {
		t.Fatalf("cpuPercent = %v, want 25", value)
	}
	if value := cpuPercent(second, sample); value != 0 {
		t.Fatalf("decreasing sample = %v", value)
	}
	for _, bad := range [][]byte{
		[]byte("intr 1\n"),
		[]byte("cpu 1 2\n"),
		[]byte("cpu a b c d e\n"),
		[]byte("cpu 0 0 0 0 0 0 0 0\n"),
		[]byte("cpu 1 2 3 4 5 6 7 8 9 10 11\n"),
	} {
		if _, err := parseProcCPUStat(bad); err == nil {
			t.Fatalf("invalid fixture accepted: %q", bad)
		}
	}
}

func TestParseProcMemInfo(t *testing.T) {
	raw := []byte("MemTotal:       1000 kB\nMemFree:         100 kB\nMemAvailable:    400 kB\n")
	value, err := parseProcMemInfo(raw)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(value-60) > 1e-9 {
		t.Fatalf("memoryPercent = %v, want 60", value)
	}
	for _, bad := range [][]byte{
		[]byte("MemTotal: 1000 kB\n"),
		[]byte("MemAvailable: 400 kB\n"),
		[]byte("MemTotal: 1000 kB\nMemAvailable: 2000 kB\n"),
		[]byte("MemTotal: 0 kB\nMemAvailable: 0 kB\n"),
	} {
		if _, err := parseProcMemInfo(bad); err == nil {
			t.Fatalf("invalid fixture accepted: %q", bad)
		}
	}
}

func TestSampleAgentMetricsFailsClosedOffLinux(t *testing.T) {
	var sampler agentMetricsSampler
	if runtime.GOOS == "linux" {
		sample, err := sampler.Sample(context.Background())
		if err != nil {
			t.Fatalf("linux sample: %v", err)
		}
		if (len(sample.GetPoints()) < 2 || len(sample.GetPoints()) > 8) || sample.GetPoints()[0].GetName() != "cpu" || sample.GetPoints()[1].GetName() != "memory" || sample.GetBatchId() == "" {
			t.Fatalf("sample = %v", sample)
		}
		for _, point := range sample.GetPoints()[:2] {
			if point.GetValue() < 0 || point.GetValue() > 100 || point.GetObservedAtUnixMs() <= 0 || point.GetUnit() != "percent" {
				t.Fatalf("point = %v", point)
			}
		}
		if len(sample.GetPoints()) >= 5 {
			points := sample.GetPoints()
			for index, name := range []string{"network.rx_bytes", "network.tx_bytes"} {
				point := points[index+2]
				if point.GetName() != name || point.GetUnit() != "bytes" || point.GetValue() < 0 || point.GetValue() > 1<<53-1 || math.Trunc(point.GetValue()) != point.GetValue() || point.GetObservedAtUnixMs() != points[0].GetObservedAtUnixMs() {
					t.Fatalf("network counter = %v", point)
				}
			}
			identity := points[4]
			digest, e := hex.DecodeString(strings.TrimPrefix(identity.GetName(), "network.interfaces."))
			if !strings.HasPrefix(identity.GetName(), "network.interfaces.") || e != nil || len(digest) != 32 || identity.GetUnit() != "set" || identity.GetValue() != 1 || identity.GetObservedAtUnixMs() != points[0].GetObservedAtUnixMs() {
				t.Fatalf("network interface identity = %v", identity)
			}
		}
		return
	}
	if _, err := sampler.Sample(context.Background()); err == nil {
		t.Fatal("non-Linux metrics sample succeeded")
	}
}

func TestAgentMetricsSamplerReusesPreviousCounters(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires real Linux counters")
	}
	var sampler agentMetricsSampler
	first, err := sampler.Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	ctx, stop := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer stop()
	second, err := sampler.Sample(ctx)
	if err != nil {
		t.Fatalf("second sample must finish within the reporting interval: %v", err)
	}
	if second.GetBatchId() == first.GetBatchId() || second.GetPoints()[0].GetObservedAtUnixMs() <= first.GetPoints()[0].GetObservedAtUnixMs() {
		t.Fatalf("sample did not advance: %v", second)
	}
}
