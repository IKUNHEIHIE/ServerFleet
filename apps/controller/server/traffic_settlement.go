package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/quota"
)

const (
	trafficSettlementInterval = 10 * time.Second
	trafficSamplesPerNode     = 120
	// Agent transport permits 100 concurrent links. Allow another full fleet
	// of disconnected nodes to await settlement during reconnects.
	trafficPendingNodes = 200
)

type trafficCounterBuffer struct {
	mu        sync.Mutex
	pending   map[string][]quota.CounterSample
	discarded int
}

func (b *trafficCounterBuffer) enqueue(sample quota.CounterSample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.appendLocked(sample.NodeID, []quota.CounterSample{sample})
}

func (b *trafficCounterBuffer) appendLocked(node string, samples []quota.CounterSample) {
	current, exists := b.pending[node]
	if !exists && len(b.pending) >= trafficPendingNodes {
		b.discarded += len(samples)
		return
	}
	for _, sample := range samples {
		if n := len(current); n != 0 && !sample.ObservedAt.After(current[n-1].ObservedAt) {
			continue
		}
		current = append(current, sample)
	}
	if excess := len(current) - trafficSamplesPerNode; excess > 0 {
		b.discarded += excess
		current = append(current[:0], current[excess:]...)
		current[0].GapBefore = true
	}
	b.pending[node] = current
}

func (b *trafficCounterBuffer) drain() (map[string][]quota.CounterSample, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	pending, discarded := b.pending, b.discarded
	b.pending, b.discarded = make(map[string][]quota.CounterSample), 0
	return pending, discarded
}

func (b *trafficCounterBuffer) retry(node string, samples []quota.CounterSample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	newer := b.pending[node]
	delete(b.pending, node)
	b.appendLocked(node, samples)
	b.appendLocked(node, newer)
}

func (s *Server) runTrafficSettlement(ctx context.Context) {
	ticker := time.NewTicker(trafficSettlementInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cycle, stop := context.WithTimeout(ctx, 8*time.Second)
			s.settleTraffic(cycle)
			stop()
		case <-ctx.Done():
			// Server.Close closes Agent links before stopping workers.
			final, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			s.settleTraffic(final)
			stop()
			return
		}
	}
}

// The link validates each frame before calling recordTraffic. Recheck the
// Controller's current authority before each node's settlement transaction.
func (s *Server) settleTraffic(ctx context.Context) {
	pending, discarded := s.traffic.drain()
	failed := 0
	for node, samples := range pending {
		cutoff := s.now().Add(-quota.CounterSampleMaxAge)
		expired := 0
		for expired < len(samples) && samples[expired].ObservedAt.Before(cutoff) {
			expired++
		}
		discarded += expired
		samples = samples[expired:]
		if len(samples) == 0 {
			continue
		}
		if expired > 0 {
			samples[0].GapBefore = true
		}
		err := s.runtimeGate(ctx)
		if err == nil {
			err = s.Quota.RecordCounterBatch(ctx, samples)
		}
		if err != nil {
			failed++
			s.traffic.retry(node, samples)
		}
	}
	if failed != 0 || discarded != 0 {
		slog.Warn("traffic settlement incomplete", "nodes_to_retry", failed, "discarded_samples", discarded)
	}
}
