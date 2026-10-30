package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/transport"
)

func identityStoreFixture(t *testing.T) (*agentIdentityStore, *x509.Certificate, *ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic test root"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(2 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(root.RawSubjectPublicKeyInfo)
	pin := hex.EncodeToString(h[:])
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := openAgentIdentity(dir, pin, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, root, key, dir
}

// This local test issuer proves CSR/key/certificate behavior only. It is NOT a
// Controller admin approval, production CA or a Linux installation test.
func issuedIdentityFixture(t *testing.T, p agentPreparedIdentity, ca *x509.Certificate, key *ecdsa.PrivateKey, alter func(*x509.Certificate)) transport.AgentEnrollmentResult {
	t.Helper()
	block, _ := pem.Decode([]byte(p.CSRPEM))
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		t.Fatal(err)
	}
	now := time.Now()
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "node-a"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * time.Minute), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	if alter != nil {
		alter(leaf)
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, csr.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	chain := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})...)
	return transport.AgentEnrollmentResult{State: "approved", NodeID: "node-a", CertificatePEM: string(chain), TrustGeneration: 3, ExpiresAt: parsed.NotAfter}
}

func TestAgentIdentityLocalKeyCSRRequestAndRestart(t *testing.T) {
	s, root, key, dir := identityStoreFixture(t)
	ctx := context.Background()
	p, err := s.Prepare(ctx, "0.9.0-test")
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	r, err := s.Request(ctx, token)
	if err != nil || r.RequestID != p.RequestID || r.Platform != runtime.GOOS || strings.Contains(r.CSRPEM, "PRIVATE KEY") {
		t.Fatal("wrong CSR-only request", err)
	}
	if strings.Contains(fmt.Sprintf("%v %#v", r, r), token) {
		t.Fatal("enrollment token printed")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "enrollment.json"))
	if bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, []byte("PRIVATE KEY")) {
		t.Fatal("secret in public intent")
	}
	issued := issuedIdentityFixture(t, p, root, key, nil)
	if err = s.Accept(ctx, issued); err != nil {
		t.Fatal(err)
	}
	if err = s.Accept(ctx, issued); err != nil {
		t.Fatal("same signed response not idempotent", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openAgentIdentity(dir, p.RootSPKI, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Prepare(ctx, "0.9.0-test")
	if err != nil || got.RequestID != p.RequestID || got.CSRPEM != p.CSRPEM {
		t.Fatal("restart changed private identity", err)
	}
	called := 0
	if err = reopened.WithCertificate(ctx, func(_ context.Context, f agentIdentityFacts) error {
		if f.NodeID != "node-a" || f.TrustGeneration != 3 || f.RootSPKI != p.RootSPKI {
			t.Error("wrong public facts")
		}
		return nil
	}, func(_ context.Context, c tls.Certificate) error {
		called++
		if c.PrivateKey == nil || c.Leaf.Subject.CommonName != "node-a" {
			t.Error("wrong local pair")
		}
		return nil
	}); err != nil || called != 1 {
		t.Fatal(err, called)
	}
}

func TestAgentIdentityRejectsWrongCertificateAuthorityAndPersistenceCorruption(t *testing.T) {
	for _, failure := range []string{"pending", "node", "generation", "expiry", "server-auth", "ca-leaf", "dns", "junk", "private-pem", "wrong-root", "wrong-key", "replacement", "missing-key", "damaged-intent", "nil-gate"} {
		t.Run(failure, func(t *testing.T) {
			s, root, key, dir := identityStoreFixture(t)
			ctx := context.Background()
			p, err := s.Prepare(ctx, "0.9.0")
			if err != nil {
				t.Fatal(err)
			}
			var alter func(*x509.Certificate)
			switch failure {
			case "server-auth":
				alter = func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth} }
			case "ca-leaf":
				alter = func(c *x509.Certificate) { c.IsCA = true; c.KeyUsage = x509.KeyUsageCertSign }
			case "dns":
				alter = func(c *x509.Certificate) { c.DNSNames = []string{"unexpected.test"} }
			}
			issued := issuedIdentityFixture(t, p, root, key, alter)
			switch failure {
			case "pending":
				issued.State = "pending"
			case "node":
				issued.NodeID = "other-node"
			case "generation":
				issued.TrustGeneration = 2
			case "expiry":
				issued.ExpiresAt = issued.ExpiresAt.Add(-time.Second)
			case "junk":
				issued.CertificatePEM = "unexpected text\n" + issued.CertificatePEM
			case "private-pem":
				issued.CertificatePEM += string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("unexpected secret")}))
			case "wrong-root":
				_, otherRoot, otherKey, _ := identityStoreFixture(t)
				issued = issuedIdentityFixture(t, p, otherRoot, otherKey, nil)
			case "wrong-key":
				other, _, _, _ := identityStoreFixture(t)
				otherPrepared, err := other.Prepare(ctx, "0.9.0")
				if err != nil {
					t.Fatal(err)
				}
				issued = issuedIdentityFixture(t, otherPrepared, root, key, nil)
			case "replacement":
				if err = s.Accept(ctx, issued); err != nil {
					t.Fatal(err)
				}
				issued = issuedIdentityFixture(t, p, root, key, func(c *x509.Certificate) { c.SerialNumber = big.NewInt(3) })
			case "missing-key":
				if err = os.Remove(filepath.Join(dir, "identity-key.pem")); err != nil {
					t.Fatal(err)
				}
				if _, err = s.Prepare(ctx, "0.9.0"); err != errAgentIdentity {
					t.Fatal("missing key silently replaced", err)
				}
			case "damaged-intent":
				if err = os.WriteFile(filepath.Join(dir, "enrollment.json"), []byte(`{"Format":1}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "nil-gate":
				if err = s.Accept(ctx, issued); err != nil {
					t.Fatal(err)
				}
				if err = s.WithCertificate(ctx, nil, func(context.Context, tls.Certificate) error { t.Error("missing gate borrowed key"); return nil }); err != errAgentIdentity {
					t.Fatal(err)
				}
				return
			}
			if err = s.Accept(ctx, issued); err != errAgentIdentity {
				t.Fatal("invalid identity accepted", err)
			}
		})
	}
}

func TestAgentStoredIdentityActualMTLSAndCurrentControllerRevocation(t *testing.T) {
	s, root, key, _ := identityStoreFixture(t)
	ctx := context.Background()
	p, err := s.Prepare(ctx, "0.9.0")
	if err != nil {
		t.Fatal(err)
	}
	issued := issuedIdentityFixture(t, p, root, key, nil)
	if err = s.Accept(ctx, issued); err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(issued.CertificatePEM))
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	fp := sha256.Sum256(leaf.Raw)
	var revoked atomic.Bool
	verifier, err := transport.NewPeerVerifier(func(context.Context, string) (transport.AgentIdentity, error) {
		return transport.AgentIdentity{NodeID: "node-a", FingerprintSHA256: hex.EncodeToString(fp[:]), Serial: leaf.SerialNumber.String(), Enabled: true, Revoked: revoked.Load(), TrustGeneration: 3}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverLeaf := &x509.Certificate{SerialNumber: big.NewInt(5), DNSNames: []string{"fleet.test"}, NotBefore: root.NotBefore, NotAfter: root.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, serverLeaf, root, &serverKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := verifier.Verify(r.Context(), *r.TLS, "node-a"); err != nil {
			w.WriteHeader(403)
			return
		}
		w.WriteHeader(204)
	}))
	server.TLS, err = verifier.ControllerTLSConfig(tls.Certificate{Certificate: [][]byte{der, root.Raw}, PrivateKey: serverKey}, pool)
	if err != nil {
		t.Fatal(err)
	}
	server.StartTLS()
	defer server.Close()
	if err = s.WithCertificate(ctx, func(context.Context, agentIdentityFacts) error { return nil }, func(_ context.Context, c tls.Certificate) error {
		cfg, err := transport.AgentTLSConfig(c, pool, "fleet.test")
		if err != nil {
			return err
		}
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}, Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		response, err := client.Get(server.URL)
		if err != nil {
			return err
		}
		_ = response.Body.Close()
		if response.StatusCode != 204 {
			return fmt.Errorf("unexpected status")
		}
		revoked.Store(true)
		client.CloseIdleConnections()
		if response, err = client.Get(server.URL); err == nil {
			_ = response.Body.Close()
			t.Error("revoked identity completed new TLS handshake")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
