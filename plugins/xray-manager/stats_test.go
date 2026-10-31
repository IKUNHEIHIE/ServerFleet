package xraymanager

import (
	"strings"
	"testing"
	"time"
)

func statsBindings() []StatsBinding {
	return []StatsBinding{
		{CounterKey{"instance-a", "user", "user-one"}, []string{"user>>>sf-user-user-one>>>traffic"}},
		{CounterKey{"instance-a", "inbound", "inbound-one"}, []string{"inbound>>>sf-vless-instance-a>>>traffic"}},
		{CounterKey{"instance-a", "instance", "instance-a"}, []string{"inbound>>>sf-vless-instance-a>>>traffic"}},
	}
}
func TestStatsExactInt64MissingIsUnavailableAndDimensionsDoNotDoubleCount(t *testing.T) {
	source := StatsObservation{"instance-a", "process-one", time.Now()}
	raw := []byte(`{"stat":[{"name":"user>>>sf-user-user-one>>>traffic>>>uplink","value":"9007199254740993"},{"name":"user>>>sf-user-user-one>>>traffic>>>downlink","value":0},{"name":"inbound>>>sf-vless-instance-a>>>traffic>>>uplink","value":"100"},{"name":"inbound>>>sf-vless-instance-a>>>traffic>>>downlink","value":"200"},{"name":"inbound>>>sf-api-in>>>traffic>>>uplink","value":"99999"}]}`)
	samples, err := DecodeStatsJSON(raw, source, statsBindings())
	if err != nil || len(samples) != 3 || !samples[0].Available || samples[0].Uplink != 9007199254740993 || samples[1].Uplink != 100 || samples[2].Uplink != 100 || samples[2].Downlink != 200 {
		t.Fatal(samples, err)
	}
	missing, err := DecodeStatsJSON([]byte(`{"stat":[{"name":"user>>>sf-user-user-one>>>traffic>>>uplink","value":"10"}]}`), source, statsBindings())
	if err != nil || missing[0].Available || missing[1].Available {
		t.Fatal("missing counter became observed zero", missing, err)
	}
	zero, err := DecodeStatsJSON([]byte(`{"stat":[{"name":"user>>>sf-user-user-one>>>traffic>>>uplink"},{"name":"user>>>sf-user-user-one>>>traffic>>>downlink"}]}`), source, statsBindings())
	if err != nil || !zero[0].Available || zero[0].Uplink != 0 {
		t.Fatal("present protobuf default was not a real zero", zero, err)
	}
}
func TestStatsRejectsAmbiguousOverflowAndInvalidBindings(t *testing.T) {
	source := StatsObservation{"instance-a", "process-one", time.Now()}
	for _, raw := range []string{`{"stat":[],"Stat":[]}`, `{"stat":null}`, `{"stat":[{"name":"a","value":null}]}`, `{"stat":[{"name":"a","value":"-1"}]}`, `{"stat":[{"name":"a","value":"9223372036854775808"}]}`, `{"stat":[{"name":"a","value":1.5}]}`, `{"stat":[{"name":"a"},{"name":"a"}]}`, `{} {}`, strings.Repeat(" ", 1<<20+1)} {
		if _, err := DecodeStatsJSON([]byte(raw), source, statsBindings()); err != ErrTrafficSample {
			t.Fatal("invalid Stats data accepted", err)
		}
	}
	for _, modify := range []func([]StatsBinding){func(b []StatsBinding) { b[0].Key.InstanceID = "other" }, func(b []StatsBinding) { b[0].Bases = []string{"user>>>*>>>traffic"} }, func(b []StatsBinding) { b[2].Bases = []string{"user>>>sf-user-user-one>>>traffic"} }, func(b []StatsBinding) { b[2].Bases = append(b[2].Bases, b[2].Bases[0]) }} {
		bindings := statsBindings()
		modify(bindings)
		if _, err := DecodeStatsJSON([]byte(`{}`), source, bindings); err != ErrTrafficSample {
			t.Fatal("untrusted binding accepted", err)
		}
	}
}
