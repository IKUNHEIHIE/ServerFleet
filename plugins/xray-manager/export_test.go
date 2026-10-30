package xraymanager

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

func clientExportFixture() (ClientExportRequest, ClientExportSnapshot) {
	req := ClientExportRequest{RequestID: "export-1", SubjectID: "human-a", RecipientID: "human-a", Source: "web", Format: "vless-uri", NodeID: "node-a", InstanceID: "instance-a", UserID: "user-a", ExpiresAt: time.Now().Add(time.Minute)}
	facts := ClientExportSnapshot{NodeID: req.NodeID, InstanceID: req.InstanceID, UserID: req.UserID, InboundID: "inbound-a", IdentityRef: "credential-a", TemplateID: "vless-tls-vision", TargetVersion: TestedXrayVersion, Host: "2001:db8::1", ServerName: "fleet.test", Label: "客户端 / user & one", Port: 443, ConfigRevision: 3, UserRevision: 2, PluginRevision: 4, CredentialRevision: 5, PluginEnabled: true, UserEnabled: true, ObservedAt: time.Now()}
	return req, facts
}

func TestClientExportURITLSRealityAndJSONOnlyDeliverAfterCredentialFence(t *testing.T) {
	for _, format := range []string{"vless-uri", "json"} {
		for _, template := range []string{"vless-tls-vision", "vless-reality-vision"} {
			t.Run(format+"-"+template, func(t *testing.T) {
				req, facts := clientExportFixture()
				req.Format = format
				facts.TemplateID = template
				if template == "vless-reality-vision" {
					facts.RealityPublicKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{37}, 32))
					facts.ShortID = "aabbccdd"
				}
				secret := []byte("3b37d23e-e729-4f43-9d02-8a4249125551")
				read := func(context.Context, ClientExportRequest) (ClientExportSnapshot, error) {
					out := facts
					out.ObservedAt = time.Now()
					return out, nil
				} // authoritative metadata double
				admit := func(_ context.Context, b ClientExportBinding) error {
					if b.Request != req || b.Snapshot.IdentityRef != facts.IdentityRef || len(b.DigestSHA256) != 64 {
						t.Error("export binding lost subject/config/user")
					}
					return nil
				}
				complete := false
				deliveries := 0
				var retained []byte
				var late func([]byte) error
				borrow := func(_ context.Context, b ClientExportBinding, use func([]byte) error) error {
					late = use
					if complete {
						t.Error("loan replayed")
					}
					if err := use(secret); err != nil {
						return err
					}
					complete = true
					return nil
				} // explicit central-fence double
				err := WithClientExport(context.Background(), req, read, admit, borrow, func(_ context.Context, kind string, raw []byte, expires time.Time) error {
					deliveries++
					retained = raw
					if !complete || kind != format || !expires.Equal(req.ExpiresAt) || !bytes.Contains(raw, secret) {
						t.Error("secret delivered before fence completion")
					}
					if format == "vless-uri" {
						u, err := url.Parse(string(raw))
						if err != nil || u.Scheme != "vless" || u.User.Username() != string(secret) || u.Hostname() != facts.Host || u.Port() != "443" || u.Fragment != facts.Label || u.Query().Get("flow") != "xtls-rprx-vision" || u.Query().Get("type") != "tcp" {
							t.Error("bad scoped export URI")
						}
						if _, password := u.User.Password(); password {
							t.Error("URI contains password authority")
						}
						if template == "vless-reality-vision" && (u.Query().Get("pbk") != facts.RealityPublicKey || u.Query().Get("sid") != facts.ShortID || u.Query().Get("fp") != "chrome") {
							t.Error("REALITY client fields changed")
						}
					}
					if format == "json" {
						var doc map[string]any
						if json.Unmarshal(raw, &doc) != nil || len(doc) != 1 || doc["outbounds"] == nil {
							t.Error("invalid outbound-only JSON")
						}
					}
					for _, unsafe := range []string{"privateKey", "keyFile", "certificateFile", "allowInsecure", "sf-api"} {
						if bytes.Contains(raw, []byte(unsafe)) {
							t.Error("server secret/unsafe field in client export", unsafe)
						}
					}
					return nil
				})
				if err != nil || deliveries != 1 || !bytes.Equal(retained, make([]byte, len(retained))) {
					t.Fatal("export delivery/cleanup failed", err)
				}
				if err = late(secret); err != ErrClientExport || deliveries != 1 {
					t.Fatal("retained callback remained usable", err)
				}
				if strings.Contains(fmt.Sprintf("%v %#v", facts, facts), facts.ShortID) && facts.ShortID != "" {
					t.Fatal("client metadata printed")
				}
			})
		}
	}
}

func TestClientExportRevocationBadLoanAndDeliveryFailureNeverProduceFalseSuccess(t *testing.T) {
	for _, failure := range []string{"credential-audit", "metadata-revoked", "metadata-rotated", "double-callback", "missing-callback", "bad-uuid", "loan-panic", "delivery-error", "delivery-revocation"} {
		t.Run(failure, func(t *testing.T) {
			req, facts := clientExportFixture()
			deliveries := 0
			secret := []byte("3b37d23e-e729-4f43-9d02-8a4249125551")
			read := func(context.Context, ClientExportRequest) (ClientExportSnapshot, error) {
				out := facts
				out.ObservedAt = time.Now()
				return out, nil
			}
			admit := func(context.Context, ClientExportBinding) error { return nil }
			borrow := func(_ context.Context, _ ClientExportBinding, use func([]byte) error) error {
				if failure == "missing-callback" {
					return nil
				}
				if failure == "loan-panic" {
					panic("loan source failed")
				}
				if failure == "bad-uuid" {
					secret = []byte("not-a-canonical-v4-uuid")
				}
				if err := use(secret); err != nil {
					return err
				}
				switch failure {
				case "credential-audit":
					return errors.New("audit did not commit")
				case "metadata-revoked":
					facts.UserEnabled = false
				case "metadata-rotated":
					facts.CredentialRevision++
				case "double-callback":
					_ = use(secret)
				}
				return nil
			}
			err := WithClientExport(context.Background(), req, read, admit, borrow, func(context.Context, string, []byte, time.Time) error {
				deliveries++
				if failure == "delivery-error" {
					return errors.New("write result unknown")
				}
				if failure == "delivery-revocation" {
					facts.PluginEnabled = false
				}
				return nil
			})
			if err != ErrClientExportUnknown {
				t.Fatal("failed export reported success", err)
			}
			want := 0
			if failure == "delivery-error" || failure == "delivery-revocation" {
				want = 1
			}
			if deliveries != want {
				t.Fatal("secret delivered before valid credential/current fence", deliveries)
			}
		})
	}
}

func TestClientExportRejectsAmbiguousSubjectsDisabledUsersUnsafeEndpointsAndStaleMetadata(t *testing.T) {
	for _, failure := range []string{"ai", "recipient", "expired", "long-expiry", "qrcode-unimplemented", "wrong-user", "disabled-user", "disabled-plugin", "stale", "future", "invalid-host", "zone", "multicast", "unsafe-sni", "invalid-version", "missing-revision", "short-id", "empty-public-key", "label-control"} {
		t.Run(failure, func(t *testing.T) {
			req, facts := clientExportFixture()
			switch failure {
			case "ai":
				req.Source = "ai"
			case "recipient":
				req.RecipientID = "human-b"
			case "expired":
				req.ExpiresAt = time.Now().Add(-time.Second)
			case "long-expiry":
				req.ExpiresAt = time.Now().Add(3 * time.Minute)
			case "qrcode-unimplemented":
				req.Format = "qrcode"
			case "wrong-user":
				facts.UserID = "another-user"
			case "disabled-user":
				facts.UserEnabled = false
			case "disabled-plugin":
				facts.PluginEnabled = false
			case "stale":
				facts.ObservedAt = time.Now().Add(-time.Minute)
			case "future":
				facts.ObservedAt = time.Now().Add(time.Minute)
			case "invalid-host":
				facts.Host = "fleet.test?exfil=yes"
			case "zone":
				facts.Host = "fe80::1%eth0"
			case "multicast":
				facts.Host = "ff02::1"
			case "unsafe-sni":
				facts.ServerName = "fleet.test\r\n"
			case "invalid-version":
				facts.TargetVersion = "latest"
			case "missing-revision":
				facts.UserRevision = 0
			case "short-id":
				facts.TemplateID = "vless-reality-vision"
				facts.RealityPublicKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
				facts.ShortID = "ABC"
			case "empty-public-key":
				facts.TemplateID = "vless-reality-vision"
				facts.ShortID = "aa"
			case "label-control":
				facts.Label = "name\nheader"
			}
			err := WithClientExport(context.Background(), req, func(context.Context, ClientExportRequest) (ClientExportSnapshot, error) { return facts, nil }, func(context.Context, ClientExportBinding) error {
				t.Error("invalid export reached authorization")
				return nil
			}, func(context.Context, ClientExportBinding, func([]byte) error) error {
				t.Error("invalid export borrowed credential")
				return nil
			}, func(context.Context, string, []byte, time.Time) error {
				t.Error("invalid export delivered")
				return nil
			})
			if err != ErrClientExport {
				t.Fatal("invalid export accepted", err)
			}
		})
	}
}
