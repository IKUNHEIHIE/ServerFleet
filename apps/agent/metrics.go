package main

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/transport"
)

var errMetricsUnavailable = errors.New("agent: local metrics sample unavailable")

type cpuTimes struct {
	total, idle uint64
}

type agentCounterSample struct {
	cpu        cpuTimes
	at         time.Time
	rx, tx     uint64
	interfaces string
}

type agentMetricsSampler struct {
	previous agentCounterSample
}

func readAgentCounters() (agentCounterSample, error) {
	cpu, err := readProcCPU("/proc/stat")
	if err != nil {
		return agentCounterSample{}, err
	}
	sample := agentCounterSample{cpu: cpu, at: time.Now()}
	if raw, err := readBoundedProcFile("/proc/net/dev"); err == nil {
		rx, tx, interfaces, err := parseProcNetDev(raw)
		if err == nil {
			sample.rx, sample.tx, sample.interfaces = rx, tx, interfaces
		}
	}
	return sample, nil
}

// Sample warms up the CPU measurement on connection. Subsequent ticks reuse the
// previous real counter snapshot so sampling never blocks the reporting ticker.
func (s *agentMetricsSampler) Sample(ctx context.Context) (*agentv1.MetricsBatch, error) {
	if ctx == nil || ctx.Err() != nil || runtime.GOOS != "linux" {
		return nil, errMetricsUnavailable
	}
	previous := s.previous
	if previous.at.IsZero() {
		first, err := readAgentCounters()
		if err != nil {
			return nil, errMetricsUnavailable
		}
		previous = first
		timer := time.NewTimer(900 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, errMetricsUnavailable
		}
	}
	current, err := readAgentCounters()
	if err != nil || current.cpu.total <= previous.cpu.total {
		return nil, errMetricsUnavailable
	}
	memoryPercent, err := readProcMemory("/proc/meminfo")
	if err != nil {
		return nil, errMetricsUnavailable
	}
	batchID, err := transport.NewConnectionID()
	if err != nil {
		return nil, errMetricsUnavailable
	}
	now := current.at.UnixMilli()
	points := []*agentv1.MetricPoint{
		{Name: "cpu", Value: cpuPercent(previous.cpu, current.cpu), Unit: "percent", ObservedAtUnixMs: now},
		{Name: "memory", Value: memoryPercent, Unit: "percent", ObservedAtUnixMs: now},
	}
	if current.interfaces != "" {
		points = append(points, networkMetricPoints(now, current.rx, current.tx, current.interfaces)...)
		elapsed := current.at.Sub(previous.at).Seconds()
		if current.interfaces == previous.interfaces && current.rx >= previous.rx && current.tx >= previous.tx && elapsed > 0 {
			points = append(points,
				&agentv1.MetricPoint{Name: "network.download_bps", Value: float64(current.rx-previous.rx) / elapsed, Unit: "bytes/second", ObservedAtUnixMs: now},
				&agentv1.MetricPoint{Name: "network.upload_bps", Value: float64(current.tx-previous.tx) / elapsed, Unit: "bytes/second", ObservedAtUnixMs: now})
		}
	}
	if disk, ok := sampleDiskPercent(); ok {
		points = append(points, &agentv1.MetricPoint{Name: "disk.used_percent", Value: disk, Unit: "percent", ObservedAtUnixMs: now})
	}
	s.previous = current
	return &agentv1.MetricsBatch{Points: points, BatchId: batchID}, nil
}

func readProcCPU(path string) (cpuTimes, error) {
	raw, err := readBoundedProcFile(path)
	if err != nil {
		return cpuTimes{}, err
	}
	return parseProcCPUStat(raw)
}

func readProcMemory(path string) (float64, error) {
	raw, err := readBoundedProcFile(path)
	if err != nil {
		return 0, err
	}
	return parseProcMemInfo(raw)
}

func readBoundedProcFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errMetricsUnavailable
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil || len(raw) == 0 {
		return nil, errMetricsUnavailable
	}
	return raw, nil
}

// parseProcCPUStat reads the aggregate "cpu " line. Guest time is already
// included in user/nice and must not be added again.
func parseProcCPUStat(raw []byte) (cpuTimes, error) {
	lines := strings.Split(string(raw), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "cpu" {
			continue
		}
		if len(fields) < 5 || len(fields) > 11 {
			return cpuTimes{}, errMetricsUnavailable
		}
		var values []uint64
		for _, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuTimes{}, errMetricsUnavailable
			}
			values = append(values, value)
		}
		if len(values) > 8 {
			values = values[:8]
		}
		var total, idle uint64
		for i, value := range values {
			if math.MaxUint64-total < value {
				return cpuTimes{}, errMetricsUnavailable
			}
			total += value
			if i == 3 || i == 4 {
				idle += value
			}
		}
		if total == 0 || idle > total {
			return cpuTimes{}, errMetricsUnavailable
		}
		return cpuTimes{total: total, idle: idle}, nil
	}
	return cpuTimes{}, errMetricsUnavailable
}

func cpuPercent(first, second cpuTimes) float64 {
	if second.total <= first.total || second.idle < first.idle {
		return 0
	}
	total := second.total - first.total
	idle := second.idle - first.idle
	if idle > total {
		return 0
	}
	value := 100 * float64(total-idle) / float64(total)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Min(100, math.Max(0, value))
}

func parseProcMemInfo(raw []byte) (float64, error) {
	var total, available uint64
	var haveTotal, haveAvailable bool
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, haveTotal = value, true
		case "MemAvailable:":
			available, haveAvailable = value, true
		}
	}
	if !haveTotal || !haveAvailable || total == 0 || available > total {
		return 0, errMetricsUnavailable
	}
	return 100 * float64(total-available) / float64(total), nil
}
