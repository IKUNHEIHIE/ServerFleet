package xraymanager

import (
	"testing"
	"time"
)

func TestTrafficBaselineReplayRestartMissingAndReset(t *testing.T) {
	now := time.Now()
	sample := TrafficSample{CounterKey{"instance-1", "user", "stable-user-id"}, "boot-1", now, true, 100, 200}
	state, delta, e := Accumulate(CounterState{}, sample)
	if e != nil || delta.Known || state.TotalUplink != 0 {
		t.Fatal(state, delta, e)
	}
	sample.At = now.Add(time.Minute)
	sample.Uplink = 160
	sample.Downlink = 280
	state, delta, e = Accumulate(state, sample)
	if e != nil || !delta.Known || delta.Uplink != 60 || delta.Downlink != 80 {
		t.Fatal(delta, e)
	}
	replayed, d, e := Accumulate(state, sample)
	if e != nil || !d.Replay || replayed.TotalUplink != 60 {
		t.Fatal(d, e)
	}
	sample.At = sample.At.Add(time.Minute)
	sample.SegmentID = "boot-2"
	sample.Uplink = 5
	sample.Downlink = 10
	state, delta, e = Accumulate(state, sample)
	if e != nil || !delta.Reset || delta.Known || state.TotalUplink != 60 {
		t.Fatal(state, delta, e)
	}
	sample.At = sample.At.Add(time.Minute)
	sample.Available = false
	state, delta, e = Accumulate(state, sample)
	if e != nil || !delta.Gap || delta.Known {
		t.Fatal(delta, e)
	}
	sample.At = sample.At.Add(time.Minute)
	sample.Available = true
	sample.Uplink = 50
	sample.Downlink = 70
	state, delta, e = Accumulate(state, sample)
	if e != nil || delta.Known || state.TotalUplink != 60 {
		t.Fatal(state, delta, e)
	}
	sample.At = sample.At.Add(time.Minute)
	sample.Uplink = 80
	sample.Downlink = 100
	state, delta, e = Accumulate(state, sample)
	if e != nil || !delta.Known || state.TotalUplink != 90 {
		t.Fatal(state, delta, e)
	}
	sample.At = now
	if _, _, e = Accumulate(state, sample); e != ErrTrafficSample {
		t.Fatal("out of order sample accepted", e)
	}
}
