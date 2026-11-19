package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

func TestManagedWebsiteIssuesAndRenewsWithoutNodes(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("ACME runtime uses a non-root Linux service account")
	}
	ctx := t.Context()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	account, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := x509.MarshalPKCS8PrivateKey(account)
	if err != nil {
		t.Fatal(err)
	}
	keyfile := filepath.Join(dir, "acme-account.pem")
	if err = os.WriteFile(keyfile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), 0600); err != nil {
		t.Fatal(err)
	}

	const domain = "service.example.com"
	ca, roots, orderCount := managedTestCA(t, domain)
	caFile := filepath.Join(dir, "acme-trust.pem")
	if err = os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, Config{DBPath: filepath.Join(dir, "test.db"), Origin: "https://fleet.test", ControllerAuthorityID: "controller-test",
		ACMEDeployment: &ACMEDeployment{AccountKeyFile: keyfile, DirectoryURL: ca.URL + "/directory", ACMERootFile: caFile,
			CloudflareURL: ca.URL, DNSRootFile: caFile, DNSCredentialID: "fixture", DNSCredentialRevision: 1,
			ForbiddenKeySPKI: []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	s.runCancel()
	<-s.runDone
	m := s.PublicCertificates
	m.acmeRoots = roots
	setup, err := s.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const password = "synthetic-website-test-password"
	if err = s.Identity.Bootstrap(ctx, setup, "admin", password); err != nil {
		t.Fatal(err)
	}
	admin, err := s.Identity.Login(ctx, "admin", password, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := s.Credentials.Create(ctx, admin.Secret, admin.CSRF, auth.CredentialInput{Name: "Fixture", Kind: "cloudflare_token", Secret: strings.Repeat("A", 40), UsageScope: []string{"dns"}})
	if err != nil {
		t.Fatal(err)
	}
	m.o.DNSCredentialID, m.o.DNSCredentialRevision = credential.ID, credential.Revision
	owner := admin.Identity.SubjectID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = s.DB.Exec(ctx, `INSERT INTO dns_accounts(id,display_name,enabled,credential_ref,credential_status,credential_revision,created_at,updated_at) VALUES('test-account','Fixture',1,?,'valid',?,?,?)`, credential.ID, credential.Revision, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO dns_zones(id,account_id,provider_zone_id,name,status,synced_at) VALUES('test-zone','test-account','fixture-zone','example.com','active',?)`, now); err != nil {
		t.Fatal(err)
	}
	if zoneID, err := m.findManagedZone(ctx, owner, domain); err != nil || zoneID != "test-zone" {
		t.Fatalf("zone=%s err=%v", zoneID, err)
	}
	if _, err = s.DB.Exec(ctx, `INSERT INTO cx_managed_websites(domain,owner_id,zone_id,profile_sha256,auto_renew,issue_key,issue_source) VALUES(?,?,'test-zone',?,1,'test-website-issue','web')`, domain, owner, m.profileSHA); err != nil {
		t.Fatal(err)
	}
	site, err := m.managedWebsite(ctx, domain)
	if err != nil {
		t.Fatal(err)
	}
	issue, err := m.submitManagedIssue(ctx, site)
	if err != nil || issue.Status != tasks.StatusAwaitingApproval || issue.TargetType != tasks.TargetTypeController {
		t.Fatalf("issue opts: %s/%s, %v", issue.Status, issue.TargetType, err)
	}
	if _, err = s.Tasks.Decide(ctx, issue.ID, issue.Revision, tasks.Decision{ApproverID: owner, RequestID: "approve-website", Approve: true}); err != nil {
		t.Fatal(err)
	}
	assertIssued := func(t *testing.T, want uint64) managedWebsite {
		t.Helper()
		for end := time.Now().Add(15 * time.Second); time.Now().Before(end); {
			if err := s.Tasks.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			site, err := m.managedWebsite(ctx, domain)
			if err != nil {
				t.Fatal(err)
			}
			if site.Generation == want {
				task, err := s.Tasks.Get(ctx, site.OrderID)
				if err != nil {
					t.Fatal(err)
				}
				if task.Status == tasks.StatusCompleted {
					return site
				}
			}
			time.Sleep(30 * time.Millisecond)
		}
		site, _ := m.managedWebsite(ctx, domain)
		task, _, _ := m.managedTask(ctx, owner, site.IssueKey)
		order, _ := certificates.ReadPublicOrder(ctx, s.DB, task.ID)
		t.Fatalf("issue did not complete: generation=%d task=%s order=%s error=%s", site.Generation, task.Status, order.Status, order.ErrorCode)
		return managedWebsite{}
	}
	first := assertIssued(t, 1)
	if _, err = s.DB.Exec(ctx, `UPDATE cx_managed_websites SET renew_at_ms=1 WHERE domain=?`, domain); err != nil {
		t.Fatal(err)
	}
	m.scheduleManagedWebsites(ctx, time.Now())
	m.scheduleManagedWebsites(ctx, time.Now())
	second := assertIssued(t, 2)
	if first.OrderID == second.OrderID || orderCount.Load() != 2 {
		t.Fatal("expected a single renewal order")
	}
	current, _, err := m.managedTask(ctx, owner, second.IssueKey)
	if err != nil || current.ApprovalRequired || current.Source != "system" {
		t.Fatalf("renew task=%+v err=%v", current, err)
	}
	order, err := certificates.ReadPublicOrder(ctx, s.DB, second.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	var keyID string
	var sealed []byte
	if err := s.DB.QueryRow(ctx, `SELECT key_id,sealed_key FROM cx_managed_website_keys WHERE order_id=?`, second.OrderID).Scan(&keyID, &sealed); err != nil {
		t.Fatal(err)
	}
	secret, err := s.Credentials.OpenWebsiteKey(second.OrderID, owner, keyID, sealed, order.NotAfter, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer clear(secret)
	if bytes.Contains(sealed, secret) || strings.Contains(current.Params, "PRIVATE KEY") {
		t.Fatal("unsealed key reached storage or plan")
	}
	if err := transport.VerifyWebsiteCertificate(transport.WebsiteCertificatePlan{Domain: domain, ChainSHA256: order.ChainSHA256}, transport.WebsiteCertificateBundle{KeyDER: secret, ChainPEM: order.ChainPEM}, roots, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func managedTestCA(t *testing.T, domain string) (*httptest.Server, *x509.CertPool, *atomic.Int64) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "In-process ACME fixture"}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	ca.Raw, err = x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	parsedRoot, err := x509.ParseCertificate(ca.Raw)
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(parsedRoot)
	orders := new(atomic.Int64)
	var nonce atomic.Int64
	var chain []byte
	var chainMu sync.Mutex
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "https://" + r.Host
		w.Header().Set("Replay-Nonce", base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("synthetic-nonce-%08d", nonce.Add(1)))))
		reply := func(value any) { _ = json.NewEncoder(w).Encode(value) }
		switch r.URL.Path {
		case "/directory":
			reply(map[string]string{"newNonce": base + "/nonce", "newAccount": base + "/account", "newOrder": base + "/new-order"})
		case "/nonce":
			w.WriteHeader(204)
		case "/account":
			w.Header().Set("Location", base+"/account/fixture")
			reply(map[string]string{"status": "valid"})
		case "/new-order":
			orders.Add(1)
			w.Header().Set("Location", base+"/order")
			reply(map[string]any{"status": "pending", "authorizations": []string{base + "/authorization"}, "finalize": base + "/finalize"})
		case "/authorization":
			reply(map[string]any{"status": "valid", "identifier": map[string]string{"type": "dns", "value": domain}})
		case "/finalize":
			var jws struct{ Payload string }
			if json.NewDecoder(r.Body).Decode(&jws) != nil {
				w.WriteHeader(400)
				return
			}
			payload, _ := base64.RawURLEncoding.DecodeString(jws.Payload)
			var fields struct{ CSR string }
			_ = json.Unmarshal(payload, &fields)
			der, _ := base64.RawURLEncoding.DecodeString(fields.CSR)
			csr, err := x509.ParseCertificateRequest(der)
			if err != nil || csr.CheckSignature() != nil {
				w.WriteHeader(400)
				return
			}
			number := orders.Load()
			leaf := &x509.Certificate{SerialNumber: big.NewInt(number + 1), Subject: csr.Subject, DNSNames: csr.DNSNames, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(0, 0, 80+int(number)), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			signed, err := x509.CreateCertificate(rand.Reader, leaf, ca, csr.PublicKey, caKey)
			if err != nil {
				w.WriteHeader(500)
				return
			}
			chainMu.Lock()
			chain = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: signed})
			chainMu.Unlock()
			reply(map[string]string{"status": "valid", "certificate": base + "/certificate"})
		case "/certificate":
			w.Header().Set("Content-Type", "application/pem-certificate-chain")
			chainMu.Lock()
			_, _ = w.Write(chain)
			chainMu.Unlock()
		default:
			w.WriteHeader(404)
		}
	})
	caServer := httptest.NewUnstartedServer(h)
	caServer.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	caServer.StartTLS()
	t.Cleanup(caServer.Close)
	return caServer, roots, orders
}
