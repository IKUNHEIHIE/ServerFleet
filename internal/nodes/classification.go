package nodes

import (
	"errors"
	"sort"
	"strings"
)

// Dimension is an automatically derived, read-only classification axis.
type Dimension string

const (
	DimensionRegion   Dimension = "region"
	DimensionProvider Dimension = "provider"
	DimensionTag      Dimension = "tag"
)

// ErrInvalidDimension rejects dimensions that are not modeled.
var ErrInvalidDimension = errors.New("nodes: invalid classification dimension")

// Valid reports whether d is a supported classification dimension.
func (d Dimension) Valid() bool {
	switch d {
	case DimensionRegion, DimensionProvider, DimensionTag:
		return true
	default:
		return false
	}
}

// Classification is a computed bucket. It is not a group resource and has no lifecycle.
type Classification struct {
	Dimension Dimension
	Value     string
	Count     int
}

// Classify computes read-only buckets from node attributes and never creates or mutates a
// group. Callers must pass only nodes visible to the current subject so counts cannot leak
// hidden inventory. Values are trimmed, empty values are skipped, and a tag repeated on one
// node counts once. Results are sorted by value.
func Classify(nodes []Node, d Dimension) ([]Classification, error) {
	if !d.Valid() {
		return nil, ErrInvalidDimension
	}
	counts := make(map[string]int)
	for _, n := range nodes {
		switch d {
		case DimensionRegion:
			countValue(counts, n.Region)
		case DimensionProvider:
			countValue(counts, n.Provider)
		case DimensionTag:
			seen := make(map[string]struct{}, len(n.Tags))
			for _, raw := range n.Tags {
				tag := strings.TrimSpace(raw)
				if tag == "" {
					continue
				}
				if _, ok := seen[tag]; ok {
					continue
				}
				seen[tag] = struct{}{}
				counts[tag]++
			}
		}
	}
	out := make([]Classification, 0, len(counts))
	for value, count := range counts {
		out = append(out, Classification{Dimension: d, Value: value, Count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out, nil
}

func countValue(counts map[string]int, raw string) {
	if value := strings.TrimSpace(raw); value != "" {
		counts[value]++
	}
}
