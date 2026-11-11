package transport_test

import (
	"encoding/json"
	"serverfleet.local/serverfleet/internal/transport"
	"strings"
	"testing"
)

func TestPublicProbeProtocolRejectsScopeAndNetworkOverrides(t *testing.T) {
	valid := map[string]string{"requestKey": strings.Repeat("a", 64), "kind": "http", "target": "https://example.com/health", "timeoutSeconds": "5", "profile": transport.ProbeProfile}
	for name, change := range map[string]func(map[string]string){"private": func(m map[string]string) { m["target"] = "http://10.0.0.1/" }, "metadata": func(m map[string]string) { m["target"] = "http://169.254.169.254/" }, "metadata-alias": func(m map[string]string) { m["target"] = "http://metadata.google.internal/" }, "userinfo": func(m map[string]string) { m["target"] = "https://u:p@example.com/" }, "method": func(m map[string]string) { m["method"] = "POST" }, "headers": func(m map[string]string) { m["headers"] = "{}" }, "long": func(m map[string]string) { m["timeoutSeconds"] = "31" }, "policy": func(m map[string]string) { m["profile"] = "private-network" }} {
		t.Run(name, func(t *testing.T) {
			m := map[string]string{}
			for k, v := range valid {
				m[k] = v
			}
			change(m)
			raw, _ := json.Marshal(m)
			if _, e := transport.ParseProbeParameters(raw); e == nil {
				t.Fatal("unsafe probe accepted")
			}
		})
	}
	raw, _ := json.Marshal(valid)
	if _, e := transport.ParseProbeParameters(raw); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"version":1,"requestKey":"` + strings.Repeat("a", 64) + `","outcome":"success"}`, `{"version":1,"requestKey":"` + strings.Repeat("a", 64) + `","outcome":"up","reason":"timeout"}`, `{"version":1,"requestKey":"` + strings.Repeat("a", 64) + `","outcome":"down","latencyMs":35001}`} {
		if _, e := transport.ParseProbeReceipt([]byte(raw)); e == nil {
			t.Fatal("invalid receipt accepted")
		}
	}
}
