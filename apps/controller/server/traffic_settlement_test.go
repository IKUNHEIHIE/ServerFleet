package server

import (
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/quota"
)

func TestTrafficBufferRetriesBeforeNewSamplesAndMarksOverflowGap(t *testing.T) {
	buffer := &trafficCounterBuffer{pending: make(map[string][]quota.CounterSample)}
	start := time.Now()
	sample := func(i int) quota.CounterSample {
		return quota.CounterSample{NodeID: "node-one", ObservedAt: start.Add(time.Duration(i) * time.Second), BytesIn: uint64(i)}
	}
	for i := range trafficSamplesPerNode {
		buffer.enqueue(sample(i))
	}
	failed, _ := buffer.drain()
	buffer.enqueue(sample(trafficSamplesPerNode))
	buffer.retry("node-one", failed["node-one"])
	batch, discarded := buffer.drain()
	samples := batch["node-one"]
	if discarded != 1 || len(samples) != trafficSamplesPerNode || samples[0].BytesIn != 1 || !samples[0].GapBefore || samples[len(samples)-1].BytesIn != trafficSamplesPerNode {
		t.Fatalf("retry did not preserve bounded chronological samples, len=%d discarded=%d", len(samples), discarded)
	}
}
