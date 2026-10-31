package main

import (
	"context"
	"math"
	"runtime"
	"testing"
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
	if runtime.GOOS == "linux" {
		sample, err := sampleAgentMetrics(context.Background())
		if err != nil {
			t.Fatalf("linux sample: %v", err)
		}
		if len(sample.GetPoints()) != 2 || sample.GetPoints()[0].GetName() != "cpu" || sample.GetPoints()[1].GetName() != "memory" || sample.GetBatchId() == "" {
			t.Fatalf("sample = %v", sample)
		}
		for _, point := range sample.GetPoints() {
			if point.GetValue() < 0 || point.GetValue() > 100 || point.GetObservedAtUnixMs() <= 0 || point.GetUnit() != "percent" {
				t.Fatalf("point = %v", point)
			}
		}
		return
	}
	if _, err := sampleAgentMetrics(context.Background()); err == nil {
		t.Fatal("non-Linux metrics sample succeeded")
	}
}
