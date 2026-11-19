package server

import (
	"context"
	"math"
	agentv1 "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1"
	"serverfleet.local/serverfleet/internal/quota"
	"serverfleet.local/serverfleet/internal/transport"
	"strings"
	"time"
)

func (s *Server) recordTraffic(ctx context.Context, obs transport.LinkObservation, boot string, batch *agentv1.MetricsBatch) error {
	if s.traffic == nil || ctx.Err() != nil {
		return nil
	}
	var rx, tx *uint64
	var interfaces string
	var observed int64
	found := false
	for _, p := range batch.GetPoints() {
		name := p.GetName()
		isCounter := name == "network.rx_bytes" || name == "network.tx_bytes"
		isSet := strings.HasPrefix(name, "network.interfaces.")
		if !isCounter && !isSet {
			continue
		}
		found = true
		if p.ObservedAtUnixMs <= 0 || observed != 0 && observed != p.ObservedAtUnixMs {
			return quota.ErrInvalidUsage
		}
		observed = p.ObservedAtUnixMs
		if isSet {
			digest := strings.TrimPrefix(name, "network.interfaces.")
			if digest == "" || interfaces != "" || p.Unit != "set" || p.Value != 1 {
				return quota.ErrInvalidUsage
			}
			interfaces = digest
			continue
		}
		if p.Unit != "bytes" || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) || p.Value < 0 || p.Value > 1<<53-1 || math.Trunc(p.Value) != p.Value {
			return quota.ErrInvalidUsage
		}
		v := uint64(p.Value)
		if name == "network.rx_bytes" {
			if rx != nil {
				return quota.ErrInvalidUsage
			}
			rx = &v
		} else {
			if tx != nil {
				return quota.ErrInvalidUsage
			}
			tx = &v
		}
	}
	if !found {
		return nil
	}
	if rx == nil || tx == nil || interfaces == "" || boot == "" {
		return quota.ErrInvalidUsage
	}
	sample := quota.CounterSample{NodeID: obs.Identity.NodeID, BootID: boot, Interfaces: interfaces, BytesIn: *rx, BytesOut: *tx, ObservedAt: time.UnixMilli(observed)}
	if err := quota.ValidateCounterSample(sample, s.now()); err != nil {
		return err
	}
	s.traffic.enqueue(sample)
	return nil
}
