package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/localpki"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/transport"
)

func TestEnrollmentExplicitReadGrantDoesNotGrantExecutionOrReviveRevocation(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	material, e := localpki.Ensure(filepath.Join(directory, "pki"))
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(ctx, Config{DBPath: filepath.Join(directory, "fleet.db"), Origin: "https://fleet.test", AgentAuthority: "fleet.test", AgentServerName: "fleet.test", EnrollmentIssuer: material.Issuer})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	setup, e := s.Identity.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Identity.Bootstrap(ctx, setup, "admin", "synthetic-enrollment-password"); e != nil {
		t.Fatal(e)
	}
	login, e := s.Identity.Login(ctx, "admin", "synthetic-enrollment-password", "127.0.0.1")
	if e != nil {
		t.Fatal(e)
	}
	owner := login.Identity.SubjectID
	principal := transport.EnrollmentPrincipal{SubjectID: owner, RequestID: "enrollment-test", Administrator: true}
	trusted := transport.WithEnrollmentPrincipal(ctx, principal)
	token, e := s.Enrollment.IssueToken(trusted, transport.EnrollmentOperation{ActorID: owner, Action: "enrollment.issue"}, transport.EnrollmentTokenInput{Name: "test node", TTLSeconds: 600})
	if e != nil {
		t.Fatal(e)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	csr, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	input := transport.AgentEnrollmentInput{EnrollmentToken: token.Token, RequestID: "explicit-enrollment", CSRPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr})), PinnedRootSPKI: material.RootSPKI, AgentVersion: "synthetic", Platform: "linux"}
	if _, e = s.Enrollment.Submit(ctx, input); e != nil {
		t.Fatal(e)
	}
	views, e := s.Enrollment.List(trusted, transport.EnrollmentOperation{ActorID: owner, Action: "enrollment.list"})
	if e != nil || len(views) != 1 {
		t.Fatal(e)
	}
	approve := func() int {
		body, _ := json.Marshal(map[string]any{"decision": "approve", "grantRead": true})
		r := httptest.NewRequest("POST", "https://fleet.test/api/v1/node-enrollments/explicit-enrollment/decision", bytes.NewReader(body))
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13, HandshakeComplete: true}
		r.Header.Set("Origin", "https://fleet.test")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Cookie", auth.SessionCookieName+"="+login.Secret)
		r.Header.Set("X-CSRF-Token", login.CSRF)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Log(w.Body.String())
		}
		return w.Code
	}
	if code := approve(); code != 200 {
		t.Fatal("approval", code)
	}
	node := views[0].NodeID
	if e = s.Identity.Authorize(ctx, auth.Check{SubjectID: owner, Action: "nodes.read", NodeIDs: []string{node}}); e != nil {
		t.Fatal("node invisible after explicit grant", e)
	}
	if e = s.Identity.Authorize(ctx, auth.Check{SubjectID: owner, Action: "tasks.submit", NodeIDs: []string{node}}); e == nil {
		t.Fatal("approval implicitly granted execution")
	}
	grants, e := s.Identity.ListResourceGrants(ctx, login.Secret)
	if e != nil || len(grants) != 1 {
		t.Fatal(grants, e)
	}
	if code := approve(); code != 200 {
		t.Fatal("idempotent approved retry", code)
	}
	if e = s.Identity.RevokeResourceGrant(ctx, login.Secret, login.CSRF, grants[0].GrantID); e != nil {
		t.Fatal(e)
	}
	if code := approve(); code != 503 {
		t.Fatal("revoked grant recreated", code)
	}
	if e = s.Identity.Authorize(ctx, auth.Check{SubjectID: owner, Action: "nodes.read", NodeIDs: []string{node}}); e == nil {
		t.Fatal("revoked node still visible")
	}
}
