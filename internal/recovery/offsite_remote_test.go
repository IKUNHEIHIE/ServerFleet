package recovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOffsiteConditionalTransferAndDelete(t *testing.T) {
	for _, kind := range []string{"s3", "webdav"} {
		t.Run(kind, func(t *testing.T) {
			var mu sync.Mutex
			var data []byte
			var puts, deletes int
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if kind == "s3" {
					if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=synthetic-key/") || r.Header.Get("X-Amz-Date") == "" {
						t.Error("missing SigV4")
					}
				} else {
					u, p, ok := r.BasicAuth()
					if !ok || u != "synthetic" || p != "synthetic-password" {
						t.Error("missing WebDAV authentication")
					}
				}
				switch r.Method {
				case "PUT":
					puts++
					if r.Header.Get("If-None-Match") != "*" {
						t.Error("unconditional overwrite")
					}
					if data != nil {
						w.WriteHeader(412)
						return
					}
					data, _ = io.ReadAll(r.Body)
					w.Header().Set("ETag", `"synthetic-etag"`)
					if kind == "s3" {
						w.Header().Set("X-Amz-Version-Id", "synthetic-version")
					}
					w.WriteHeader(201)
				case "GET":
					if data == nil {
						w.WriteHeader(404)
						return
					}
					w.Header().Set("ETag", `"synthetic-etag"`)
					if kind == "s3" {
						w.Header().Set("X-Amz-Version-Id", "synthetic-version")
					}
					w.Write(data)
				case "DELETE":
					deletes++
					if r.Header.Get("If-Match") != `"synthetic-etag"` {
						t.Error("missing conditional delete")
						w.WriteHeader(412)
						return
					}
					if kind == "s3" && r.URL.Query().Get("versionId") != "synthetic-version" {
						t.Error("version binding lost")
					}
					data = nil
					w.WriteHeader(204)
				default:
					t.Error(r.Method)
					w.WriteHeader(405)
				}
			}))
			defer server.Close()
			d := BackupDestination{Kind: kind, Endpoint: server.URL, CredentialRef: "synthetic-ref", CredentialRevision: 1, Prefix: "fleet"}
			secret := `{"username":"synthetic","password":"synthetic-password"}`
			if kind == "s3" {
				d.Bucket = "synthetic-bucket"
				d.Region = "us-east-1"
				secret = `{"accessKeyId":"synthetic-key","secretAccessKey":"synthetic-secret"}`
			}
			store, e := newRemoteStore(d, []byte(secret), func() error { return nil })
			if e != nil {
				t.Fatal(e)
			}
			// Only this test-owned store uses the local TLS fixture's explicit trust.
			store.client = server.Client()
			store.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			content := []byte("age-encryption.org/v1\nsynthetic-encrypted-fixture")
			sum := sha256.Sum256(content)
			o := RemoteObject{Key: "fleet/synthetic.age", Bytes: int64(len(content)), SHA256: hex.EncodeToString(sum[:])}
			observed, e := store.put(context.Background(), o, bytes.NewReader(content))
			if e != nil || observed.ETag == "" {
				t.Fatal(observed, e)
			}
			if _, e = store.put(context.Background(), o, bytes.NewReader(content)); e == nil {
				t.Fatal("overwrote existing object")
			}
			// Corruption and changed ETags fail closed before a DELETE can happen.
			bad := observed
			bad.SHA256 = strings.Repeat("0", 64)
			if store.remove(context.Background(), bad) == nil {
				t.Fatal("deleted mismatched bytes")
			}
			bad = observed
			bad.ETag = `"changed"`
			if store.remove(context.Background(), bad) == nil {
				t.Fatal("deleted changed etag")
			}
			if e = store.remove(context.Background(), observed); e != nil {
				t.Fatal(e)
			}
			mu.Lock()
			defer mu.Unlock()
			if puts != 2 || deletes != 1 || data != nil {
				t.Fatal(puts, deletes, len(data))
			}
		})
	}
}
func TestOffsiteRejectsPrivateDestinationsAndAmbiguousSecrets(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "::1", "::ffff:127.0.0.1", "2001:db8::1", "64:ff9b::a00:1"} {
		if publicRemoteIP(netip.MustParseAddr(ip)) {
			t.Fatal("private destination accepted", ip)
		}
	}
	if !publicRemoteIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public destination rejected")
	}
	c := remoteHTTPClient()
	defer c.CloseIdleConnections()
	if _, e := c.Get("https://127.0.0.1:1/metadata"); e == nil {
		t.Fatal("production transport admitted localhost")
	}
	d := BackupDestination{Kind: "webdav", Endpoint: "https://backup.example.com/path", CredentialRef: "synthetic-ref", CredentialRevision: 1}
	for _, raw := range []string{`{"username":"a","username":"b","password":"synthetic"}`, `{"Username":"a","password":"synthetic"}`, `{"username":"a","password":"synthetic","unexpected":true}`, `{"username":"a","password":{"nested":"secret"}}`} {
		if _, e := newRemoteStore(d, []byte(raw), func() error { return nil }); e == nil {
			t.Fatal("ambiguous secret accepted")
		}
	}
	for _, tag := range []string{`W/"weak"`, `"one","two"`, `"one",*"`, `"bad` + "\n" + `tag"`} {
		if strongRemoteETag(tag) {
			t.Fatal("unsafe etag accepted", tag)
		}
	}
	revoked := false
	store, e := newRemoteStore(d, []byte(`{"username":"a","password":"synthetic"}`), func() error {
		if revoked {
			return ErrBackupUnavailable
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	revoked = true
	if _, e = store.verify(context.Background(), RemoteObject{Key: "test.age", Bytes: 1, SHA256: strings.Repeat("0", 64)}); e == nil {
		t.Fatal("revoked transfer admitted")
	}
}

// Public AWS documentation vector, not live credentials. Exact signature comes
// from the GET Bucket Lifecycle example at the SigV4 reference in BACKUP-OPERATIONS.
func TestOffsiteAWSOfficialSignatureVector(t *testing.T) {
	request, e := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/?lifecycle=", nil)
	if e != nil {
		t.Fatal(e)
	}
	secret := remoteSecret{AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
	signRemoteRequest(request, "us-east-1", secret, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	if !strings.HasSuffix(request.Header.Get("Authorization"), "Signature=fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543") {
		t.Fatal("signature differs from published AWS vector")
	}
}
