package xraymanager

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"
)

func templateFixture(t *testing.T) (TemplateSpec, PreflightFacts, MaterialResolver) {
	t.Helper()
	now := time.Now()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"fleet.test"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(2 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	spec := TemplateSpec{InstanceID: "instance-a", NodeID: "node-a", TemplateID: "vless-tls-vision", TargetVersion: TestedXrayVersion, Listen: "127.0.0.1", Port: 14443, APIPort: 15000, Domain: "fleet.test", CertificateRef: "cert-1", ExpectedRevision: 1, Users: []TemplateUser{{"stable-user-1", "credential-1", true}}, RealityTarget: "example.invalid:443", RealityKeyRef: "reality-key-1", ServerNames: []string{"example.invalid"}, ShortIDs: []string{"aabbccdd"}, RealityFallbackAcknowledged: true}
	facts := PreflightFacts{NodeID: spec.NodeID, InstanceID: spec.InstanceID, OS: "linux", Architecture: "amd64", Version: TestedXrayVersion, Revision: 1, PluginRevision: 1, PluginEnabled: true, PortAvailable: true, APIPortAvailable: true, Managed: true, ObservedAt: now, CertificateDER: der, CertificateFile: "/var/lib/serverfleet/xray/instance-a/tls/cert.pem", PrivateKeyFile: "/var/lib/serverfleet/xray/instance-a/tls/key.pem", RealityTLS13Reachable: true}
	reality, _ := ecdh.X25519().GenerateKey(rand.Reader)
	resolver := func(ctx context.Context, instance, purpose, ref string, use func([]byte) error) error {
		if instance != spec.InstanceID {
			return ErrConfigurationRejected
		}
		var b []byte
		switch {
		case purpose == "xray-user" && ref == "credential-1":
			b = []byte("3b37d23e-e729-4f43-9d02-8a4249125551")
		case purpose == "xray-reality" && ref == "reality-key-1":
			b = []byte(base64.RawURLEncoding.EncodeToString(reality.Bytes()))
		default:
			return ErrConfigurationRejected
		}
		defer clear(b)
		return use(b)
	}
	return spec, facts, resolver
}
func allowTemplate(context.Context, TemplateSpec, PreflightFacts) error { return nil }
func TestTemplateBothVersionsAndNoUncontrolledSecretRead(t *testing.T) {
	spec, facts, resolve := templateFixture(t)
	ctx := context.Background()
	for _, template := range []string{"vless-tls-vision", "vless-reality-vision"} {
		spec.TemplateID = template
		var borrowed []byte
		if err := WithTemplate(ctx, spec, facts, allowTemplate, resolve, func(_ context.Context, b []byte) error {
			borrowed = b
			var cfg map[string]any
			if json.Unmarshal(b, &cfg) != nil {
				t.Fatal("invalid config")
			}
			inbounds := cfg["inbounds"].([]any)
			api := inbounds[1].(map[string]any)
			if api["listen"] != "127.0.0.1" {
				t.Fatal("public API")
			}
			settings := inbounds[0].(map[string]any)["settings"].(map[string]any)
			if _, ok := settings["users"]; !ok {
				t.Fatal("wrong version schema")
			}
			return nil
		}); err != nil {
			t.Fatal(template, err)
		}
		if !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
			t.Fatal("render buffer retained")
		}
	}
	calls := 0
	tracked := func(ctx context.Context, instance, purpose, ref string, use func([]byte) error) error {
		calls++
		return resolve(ctx, instance, purpose, ref, use)
	}
	if err := WithTemplate(ctx, spec, facts, nil, tracked, func(context.Context, []byte) error { return nil }); err != ErrConfigurationRejected || calls != 0 {
		t.Fatal("no gate read secrets", err, calls)
	}
	gateCalls := 0
	if err := WithTemplate(ctx, spec, facts, func(context.Context, TemplateSpec, PreflightFacts) error {
		gateCalls++
		if gateCalls > 1 {
			return errors.New("revoked")
		}
		return nil
	}, resolve, func(context.Context, []byte) error { t.Fatal("revoked config escaped"); return nil }); err != ErrConfigurationRejected {
		t.Fatal(err)
	}
}
func TestTemplateRejectsUnsafeOrStaleFacts(t *testing.T) {
	for name, change := range map[string]func(*TemplateSpec, *PreflightFacts){
		"unmanaged": func(_ *TemplateSpec, f *PreflightFacts) { f.Managed = false }, "plugin-disabled": func(_ *TemplateSpec, f *PreflightFacts) { f.PluginEnabled = false }, "port-conflict": func(_ *TemplateSpec, f *PreflightFacts) { f.PortAvailable = false }, "stale-revision": func(_ *TemplateSpec, f *PreflightFacts) { f.Revision++ }, "stale-facts": func(_ *TemplateSpec, f *PreflightFacts) { f.ObservedAt = f.ObservedAt.Add(-time.Minute) }, "floating-version": func(s *TemplateSpec, _ *PreflightFacts) { s.TargetVersion = "latest" }, "certificate-host": func(s *TemplateSpec, _ *PreflightFacts) { s.Domain = "other.test" }, "private-path": func(_ *TemplateSpec, f *PreflightFacts) { f.PrivateKeyFile = "/etc/shadow" }, "no-ack": func(s *TemplateSpec, _ *PreflightFacts) {
			s.TemplateID = "vless-reality-vision"
			s.RealityFallbackAcknowledged = false
		}, "empty-short-id": func(s *TemplateSpec, _ *PreflightFacts) {
			s.TemplateID = "vless-reality-vision"
			s.ShortIDs = []string{""}
		}, "bad-target": func(s *TemplateSpec, _ *PreflightFacts) {
			s.TemplateID = "vless-reality-vision"
			s.RealityTarget = "https://example.invalid"
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, f, r := templateFixture(t)
			change(&s, &f)
			calls := 0
			err := WithTemplate(context.Background(), s, f, allowTemplate, r, func(context.Context, []byte) error { calls++; return nil })
			if err != ErrConfigurationRejected || calls != 0 {
				t.Fatal(err, calls)
			}
		})
	}
}
