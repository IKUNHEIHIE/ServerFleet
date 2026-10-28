package nodes

import (
	"math"
	"time"
)

// Status is the provisionally modeled connection state of a node.
type Status string

const (
	StatusOnline    Status = "online"
	StatusOffline   Status = "offline"
	StatusSuspended Status = "suspended"
	StatusRevoked   Status = "revoked"
	StatusUnknown   Status = "unknown"
)

// Valid reports whether s is one of the modeled states.
func (s Status) Valid() bool {
	switch s {
	case StatusOnline, StatusOffline, StatusSuspended, StatusRevoked, StatusUnknown:
		return true
	default:
		return false
	}
}

// Metrics is a single raw resource sample. A zero ObservedAt is not a real sample.
type Metrics struct {
	CPUPercent    float64
	MemoryPercent float64
	ObservedAt    time.Time
}

// Node is the internal inventory record, not the public G2 DTO. Metrics is raw source data and
// may remain populated after a node goes offline; display code must call Usage and never read
// Metrics directly. Metrics is nil when no sample was ever received.
type Node struct {
	ID         string
	Name       string
	Address    string
	Region     string
	Provider   string
	Tags       []string
	Status     Status
	LastSeenAt time.Time
	Metrics    *Metrics
}

// Usage returns a sample only for an online node with a present, fresh observation.
// When ok is false the caller must render missing data instead of zero percentages.
func (n Node) Usage(now time.Time, maxAge time.Duration) (Metrics, bool) {
	if n.Status != StatusOnline || n.Metrics == nil || maxAge <= 0 {
		return Metrics{}, false
	}
	sample := *n.Metrics
	if !finite(sample.CPUPercent) || !finite(sample.MemoryPercent) {
		return Metrics{}, false
	}
	if sample.ObservedAt.IsZero() || sample.ObservedAt.After(now) {
		return Metrics{}, false
	}
	if now.Sub(sample.ObservedAt) > maxAge {
		return Metrics{}, false
	}
	return sample, true
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
