// Package xraymanager is an optional business package. Core releases must not
// import it. These counters consume authenticated StatsService observations;
// they never represent VPS billing or combine user/inbound/instance totals.
package xraymanager

import (
	"errors"
	"math"
	"time"
)

var ErrTrafficSample = errors.New("xray-manager: invalid traffic sample")

// CounterKey uses stable internal IDs, never display names or mutable labels.
type CounterKey struct {
	InstanceID string
	Dimension  string
	ResourceID string
}
type TrafficSample struct {
	Key       CounterKey
	SegmentID string
	At        time.Time
	Available bool
	Uplink    uint64
	Downlink  uint64
}
type CounterState struct {
	Key              CounterKey
	SegmentID        string
	At               time.Time
	LastUplink       uint64
	LastDownlink     uint64
	TotalUplink      uint64
	TotalDownlink    uint64
	Gap              bool
	IntervalComplete bool
	HistoricalGap    bool
	Initialized      bool
}
type TrafficDelta struct {
	Uplink, Downlink uint64
	Known            bool
	Reset            bool
	Replay           bool
	Gap              bool
}

// Accumulate is pure so DS persistence can compare-and-store each key atomically.
// One collector uses non-resetting reads; QueryStats(reset=true) is not supported
// by this candidate. First observations establish a baseline, not invented usage.
// Missing/reset intervals remain incomplete and don't become zero usage.
func Accumulate(prior CounterState, sample TrafficSample) (CounterState, TrafficDelta, error) {
	if sample.Key.InstanceID == "" || sample.Key.ResourceID == "" || sample.At.IsZero() {
		return prior, TrafficDelta{}, ErrTrafficSample
	}
	switch sample.Key.Dimension {
	case "user", "inbound", "instance":
	default:
		return prior, TrafficDelta{}, ErrTrafficSample
	}
	if prior.Initialized && sample.At.Before(prior.At) {
		return prior, TrafficDelta{}, ErrTrafficSample
	}
	if prior.Initialized && prior.Key != sample.Key {
		return prior, TrafficDelta{}, ErrTrafficSample
	}
	if prior.Initialized && sample.At.Equal(prior.At) {
		if sample.Available && !prior.Gap && sample.SegmentID == prior.SegmentID && sample.Uplink == prior.LastUplink && sample.Downlink == prior.LastDownlink {
			return prior, TrafficDelta{Replay: true}, nil
		}
		return prior, TrafficDelta{}, ErrTrafficSample
	}
	next := prior
	next.Key = sample.Key
	next.At = sample.At
	next.Initialized = true
	if !sample.Available {
		next.Gap = true
		next.IntervalComplete = false
		next.HistoricalGap = true
		return next, TrafficDelta{Gap: true}, nil
	}
	if sample.SegmentID == "" {
		return prior, TrafficDelta{}, ErrTrafficSample
	}
	next.SegmentID = sample.SegmentID
	next.LastUplink = sample.Uplink
	next.LastDownlink = sample.Downlink
	next.Gap = false
	if !prior.Initialized {
		return next, TrafficDelta{Gap: true}, nil
	}
	reset := sample.SegmentID != prior.SegmentID || sample.Uplink < prior.LastUplink || sample.Downlink < prior.LastDownlink
	if reset || prior.Gap {
		next.IntervalComplete = false
		next.HistoricalGap = true
		return next, TrafficDelta{Reset: reset, Gap: true}, nil
	}
	up := sample.Uplink - prior.LastUplink
	down := sample.Downlink - prior.LastDownlink
	if math.MaxUint64-prior.TotalUplink < up || math.MaxUint64-prior.TotalDownlink < down {
		return prior, TrafficDelta{}, ErrTrafficSample
	}
	next.TotalUplink += up
	next.TotalDownlink += down
	// Complete refers to this consecutive interval; historical incompleteness
	// remains represented by the returned Gap/first baseline in persisted samples.
	next.IntervalComplete = true
	return next, TrafficDelta{Uplink: up, Downlink: down, Known: true}, nil
}
