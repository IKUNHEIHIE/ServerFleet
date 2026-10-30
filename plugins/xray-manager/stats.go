package xraymanager

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// StatsBinding is frozen from current managed config metadata, never selected
// by arbitrary browser-provided email/tag prefixes. Instance totals use only
// the registered traffic inbounds, not user+inbound+outbound sums.
type StatsBinding struct {
	Key   CounterKey
	Bases []string
}
type StatsObservation struct {
	InstanceID, SegmentID string
	At                    time.Time
}

func validStatsBase(base, prefix string) bool {
	return len(base) <= 512 && strings.HasPrefix(base, prefix+">>>") && strings.HasSuffix(base, ">>>traffic") && strings.Count(base, ">>>") == 2 && !strings.ContainsAny(base, "\r\n\x00*?[]")
}
func statsValue(value any) (uint64, bool) {
	var text string
	switch v := value.(type) {
	case nil:
		return 0, true
	case string:
		text = v
	case json.Number:
		text = string(v)
	default:
		return 0, false
	}
	// StatsService uses signed int64. Negative/wrapped and inexact float values
	// are rejected, not cast into huge unsigned usage or silently rounded.
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 0 || strconv.FormatInt(n, 10) != text {
		return 0, false
	}
	return uint64(n), true
}

// DecodeStatsJSON decodes a SUCCESSFUL non-resetting StatsService query from
// the fixed target CLI/adapter. Call failures must remain unavailable outside
// this pure decoder. Metadata must come from current controlled process/config
// observations; neither JSON nor a reset counter proves a process generation.
func DecodeStatsJSON(raw []byte, source StatsObservation, bindings []StatsBinding) ([]TrafficSample, error) {
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) || !safeConfigID(source.InstanceID) || !safeConfigID(source.SegmentID) || source.At.IsZero() || len(bindings) == 0 || len(bindings) > 256 {
		return nil, ErrTrafficSample
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	remaining := 100000
	value, err := readCustomValue(d, 0, &remaining)
	root := object(value)
	if err != nil || root == nil {
		return nil, ErrTrafficSample
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, ErrTrafficSample
	}
	for key := range root {
		if key != "stat" {
			return nil, ErrTrafficSample
		}
	}
	list, exists := root["stat"]
	if exists {
		if _, ok := list.([]any); !ok {
			return nil, ErrTrafficSample
		}
	}
	stats := map[string]uint64{}
	for _, item := range array(list) {
		entry := object(item)
		name := textValue(entry["name"])
		if entry == nil || name == "" || len(name) > 1024 || strings.ContainsAny(name, "\r\n\x00") {
			return nil, ErrTrafficSample
		}
		for key := range entry {
			if key != "name" && key != "value" {
				return nil, ErrTrafficSample
			}
		}
		if _, duplicate := stats[name]; duplicate {
			return nil, ErrTrafficSample
		}
		if value, exists := entry["value"]; exists && value == nil {
			return nil, ErrTrafficSample
		}
		n, ok := statsValue(entry["value"])
		if !ok {
			return nil, ErrTrafficSample
		}
		stats[name] = n
	}
	seen := map[CounterKey]bool{}
	out := make([]TrafficSample, 0, len(bindings))
	for _, binding := range bindings {
		key := binding.Key
		if key.InstanceID != source.InstanceID || !safeConfigID(key.ResourceID) || seen[key] || len(binding.Bases) == 0 || len(binding.Bases) > 128 {
			return nil, ErrTrafficSample
		}
		seen[key] = true
		prefix := "inbound"
		switch key.Dimension {
		case "user":
			prefix = "user"
			if len(binding.Bases) != 1 {
				return nil, ErrTrafficSample
			}
		case "inbound":
			if len(binding.Bases) != 1 {
				return nil, ErrTrafficSample
			}
		case "instance":
			if key.ResourceID != key.InstanceID {
				return nil, ErrTrafficSample
			}
		default:
			return nil, ErrTrafficSample
		}
		sample := TrafficSample{Key: key, SegmentID: source.SegmentID, At: source.At, Available: true}
		seenBases := map[string]bool{}
		for _, base := range binding.Bases {
			if !validStatsBase(base, prefix) || seenBases[base] {
				return nil, ErrTrafficSample
			}
			seenBases[base] = true
			up, upExists := stats[base+">>>uplink"]
			down, downExists := stats[base+">>>downlink"]
			if !upExists || !downExists {
				sample.Available = false
				continue
			}
			if math.MaxUint64-sample.Uplink < up || math.MaxUint64-sample.Downlink < down {
				return nil, ErrTrafficSample
			}
			sample.Uplink += up
			sample.Downlink += down
		}
		if !sample.Available {
			sample.Uplink, sample.Downlink = 0, 0
		} // Unavailable, never an observed zero.
		out = append(out, sample)
	}
	return out, nil
}
