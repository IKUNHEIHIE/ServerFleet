package xraymanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func basicCustom() []byte {
	return []byte(` {"log":{"loglevel":"none"},"inbounds":[{"tag":"local","listen":"127.0.0.1","port":18384,"protocol":"socks","settings":{"auth":"noauth","udp":false}}],"outbounds":[{"tag":"direct","protocol":"freedom"}]} `)
}
func customPolicy() CustomPolicy {
	return CustomPolicy{InstanceID: "instance-a", TargetVersion: TestedXrayVersion}
}
func directAPICustom() []byte {
	return []byte(`{"log":{"loglevel":"none"},"api":{"tag":"private-api","listen":"127.0.0.1:18500","services":["StatsService"]},"stats":{},"inbounds":[{"tag":"local","listen":"127.0.0.1","port":18384,"protocol":"socks","settings":{"auth":"noauth"}}],"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}],"routing":{"rules":[{"type":"field","port":"18500","outboundTag":"block"}]}}`)
}
func TestCustomReviewPreservesBytesAndReturnsNoConfigValues(t *testing.T) {
	raw := basicCustom()
	before := bytes.Clone(raw)
	review, err := ReviewCustomJSON(raw, customPolicy())
	hash := sha256.Sum256(raw)
	if err != nil || review.SHA256 != hex.EncodeToString(hash[:]) || !bytes.Equal(raw, before) || review.InboundCount != 1 {
		t.Fatal(review, err)
	}
	if _, err = ReviewCustomJSON(directAPICustom(), customPolicy()); err != nil {
		t.Fatal("safe direct loopback API rejected", err)
	}
	spec, facts, resolve := templateFixture(t)
	for _, id := range []string{"vless-tls-vision", "vless-reality-vision"} {
		spec.TemplateID = id
		if err := WithTemplate(context.Background(), spec, facts, allowTemplate, resolve, func(_ context.Context, raw []byte) error {
			policy := customPolicy()
			policy.AllowedReadFiles = []string{facts.CertificateFile, facts.PrivateKeyFile}
			review, err := ReviewCustomJSON(raw, policy)
			if err != nil {
				return err
			}
			value, _ := json.Marshal(review)
			if bytes.Contains(value, []byte("3b37d23e")) || bytes.Contains(value, []byte(facts.PrivateKeyFile)) {
				t.Fatal("config material escaped in review")
			}
			if id == "vless-reality-vision" && !slices.Contains(review.WarningCodes, "REALITY_FALLBACK_REQUIRES_REVIEW") {
				t.Fatal("missing REALITY consent warning")
			}
			return nil
		}); err != nil {
			t.Fatal(id, err)
		}
	}
}

func TestCustomReviewRejectsPublicOrIndirectManagementAPI(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"public-listen":   func(m map[string]any) { object(m["api"])["listen"] = "0.0.0.0:18500" },
		"hostname-listen": func(m map[string]any) { object(m["api"])["listen"] = "localhost:18500" },
		"empty-listen":    func(m map[string]any) { object(m["api"])["listen"] = "" },
		"routing-service": func(m map[string]any) { object(m["api"])["services"] = []any{"RoutingService"} },
		"missing-block":   func(m map[string]any) { object(m["routing"])["rules"] = []any{} },
		"permit-before-block": func(m map[string]any) {
			r := object(m["routing"])
			r["rules"] = append([]any{map[string]any{"type": "field", "network": "tcp", "outboundTag": "direct"}}, array(r["rules"])...)
		},
		"fake-block":     func(m map[string]any) { array(m["outbounds"])[1].(map[string]any)["protocol"] = "freedom" },
		"loopback-route": func(m map[string]any) { array(m["outbounds"])[0].(map[string]any)["protocol"] = "loopback" },
		"api-via-outbound": func(m map[string]any) {
			array(m["outbounds"])[0].(map[string]any)["proxySettings"] = map[string]any{"tag": "private-api"}
		},
		"api-via-rule": func(m map[string]any) {
			r := object(m["routing"])
			r["rules"] = append(array(r["rules"]), map[string]any{"type": "field", "network": "tcp", "outboundTag": "private-api"})
		},
		"api-via-balancer-prefix": func(m map[string]any) {
			object(m["routing"])["balancers"] = []any{map[string]any{"tag": "any", "selector": []string{"private"}}}
		},
		"extra-metrics-listener": func(m map[string]any) { m["metrics"] = map[string]any{"listen": "127.0.0.1:19000"} },
		"redirect-after-routing": func(m map[string]any) {
			array(m["outbounds"])[0].(map[string]any)["settings"] = map[string]any{"redirect": "127.0.0.1:18500"}
		},
		"redirect-via-dns-alias": func(m map[string]any) {
			array(m["outbounds"])[0].(map[string]any)["settings"] = map[string]any{"redirect": "alias.invalid:018500"}
		},
		"proxy-endpoint-after-routing": func(m map[string]any) {
			array(m["outbounds"])[0].(map[string]any)["settings"] = map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": json.Number("18500")}}}
		},
		"endpoint-url-after-routing": func(m map[string]any) {
			array(m["outbounds"])[0].(map[string]any)["settings"] = map[string]any{"address": "https://localhost:18500/path"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var config map[string]any
			if json.Unmarshal(directAPICustom(), &config) != nil {
				t.Fatal("bad fixture")
			}
			change(config)
			raw, _ := json.Marshal(config)
			if _, err := ReviewCustomJSON(raw, customPolicy()); err != ErrConfigurationRejected {
				t.Fatal(name, err)
			}
		})
	}
}

func TestCustomReviewRejectsAmbiguityBoundsAndFiles(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"inbounds":[],"inbounds":[]}`), []byte(`{"API":{},"api":{}}`),
		append(basicCustom(), []byte(`{}`)...), []byte(`null`),
		[]byte(strings.Repeat("[", 34) + strings.Repeat("]", 34)),
		bytes.Repeat([]byte(" "), 1<<20+1),
	} {
		if _, err := ReviewCustomJSON(raw, customPolicy()); err != ErrConfigurationRejected {
			t.Fatal("ambiguous/unbounded JSON accepted", err)
		}
	}
	for _, field := range []string{`"certificateFile":"/etc/shadow"`, `"keyFile":"/var/lib/serverfleet/xray/other/tls/key.pem"`, `"keyFile":"none"`, `"access":"/etc/cron.d/xray"`, `"include":"another-config.json"`, `"masterKeyLog":"/tmp/session.keys"`, `"masquerade":{"dir":"/etc"}`} {
		raw := bytes.Replace(basicCustom(), []byte(`"loglevel":"none"`), []byte(`"loglevel":"none",`+field), 1)
		if _, err := ReviewCustomJSON(raw, customPolicy()); err != ErrConfigurationRejected {
			t.Fatal("ungranted file/directive accepted", err)
		}
	}
	policy := customPolicy()
	file := "/var/lib/serverfleet/xray/instance-a/tls/key.pem"
	raw := bytes.Replace(basicCustom(), []byte(`"loglevel":"none"`), []byte(`"loglevel":"none","keyFile":"`+file+`"`), 1)
	policy.AllowedReadFiles = []string{file}
	if _, err := ReviewCustomJSON(raw, policy); err != nil {
		t.Fatal("exact current controlled file grant rejected", err)
	}
	policy.TargetVersion = "latest"
	if _, err := ReviewCustomJSON(basicCustom(), policy); err != ErrConfigurationRejected {
		t.Fatal("floating target version", err)
	}
}
