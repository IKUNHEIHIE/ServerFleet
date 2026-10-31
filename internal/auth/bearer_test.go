package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func tokenFixture(t *testing.T) (*IdentityService, *identityTestStore, *time.Time, LoginSession, RestrictedTokenInput) {
	t.Helper()
	s, store, now := identityFixture(t)
	s.resources = func(_ context.Context, kind, id string) ([]string, error) {
		if kind == "node" {
			return []string{id}, nil
		}
		return []string{"node-1", "node-2"}, nil
	}
	admin := identityLogin(t, s)
	input := RestrictedTokenInput{"Local script", "script-read", []string{"nodes.read"}, []string{"node-1"}, 3600}
	return s, store, now, admin, input
}
func TestRestrictedBearerCurrentIntersectionRevocationAndExpiry(t *testing.T) {
	ctx := context.Background()
	s, store, now, admin, input := tokenFixture(t)
	if _, err := s.IssueRestrictedToken(ctx, admin.Secret, admin.CSRF, input); err != ErrDenied {
		t.Fatal("admin implicit node token", err)
	}
	grant, err := s.CreateResourceGrant(ctx, admin.Secret, admin.CSRF, GrantRequest{PrincipalID: admin.Identity.SubjectID, ResourceType: "node_group", ResourceID: "group-1", Permissions: []string{"nodes.read"}})
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.IssueRestrictedToken(ctx, admin.Secret, admin.CSRF, input)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(store.data, []byte(token.Secret)) {
		t.Fatal("persisted raw bearer")
	}
	list, err := s.ListRestrictedTokens(ctx, admin.Secret)
	encoded, _ := json.Marshal(list)
	if err != nil || bytes.Contains(encoded, []byte(token.Secret)) {
		t.Fatal("token metadata leak", err)
	}
	observed, err := s.AuthorizeRestrictedToken(ctx, token.Secret, "nodes.read", []string{"node-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthorizeRestrictedToken(ctx, token.Secret, "nodes.read", []string{"node-2"}); err != ErrDenied {
		t.Fatal("expanded token cap", err)
	}
	if _, err = s.Authenticate(ctx, token.Secret, "", false); err != ErrCredential {
		t.Fatal("bearer treated as session", err)
	}
	input.Actions = []string{"tasks.submit"}
	if _, err = s.IssueRestrictedToken(ctx, admin.Secret, admin.CSRF, input); err != ErrInvalidCheck {
		t.Fatal("execution token", err)
	}
	if err = s.RevokeResourceGrant(ctx, admin.Secret, admin.CSRF, grant.GrantID); err != nil {
		t.Fatal(err)
	}
	if err = s.BearerCommitGate(ctx, token.Secret, "nodes.read", []string{"node-1"}, observed); err != ErrDenied {
		t.Fatal("stale observed allow", err)
	}
	if _, err = s.CreateResourceGrant(ctx, admin.Secret, admin.CSRF, GrantRequest{PrincipalID: admin.Identity.SubjectID, ResourceType: "node", ResourceID: "node-1", Permissions: []string{"nodes.read"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthorizeRestrictedToken(ctx, token.Secret, "nodes.read", []string{"node-1"}); err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeRestrictedToken(ctx, admin.Secret, admin.CSRF, token.Metadata.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthorizeRestrictedToken(ctx, token.Secret, "nodes.read", []string{"node-1"}); err != ErrCredential {
		t.Fatal(err)
	}
	input.Actions = []string{"nodes.read"}
	token, err = s.IssueRestrictedToken(ctx, admin.Secret, admin.CSRF, input)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	if _, err = s.AuthorizeRestrictedToken(ctx, token.Secret, "nodes.read", []string{"node-1"}); err != ErrCredential {
		t.Fatal("expired token", err)
	}
}
func TestRestrictedBearerHTTPSelectionAndNoBrowserEscalation(t *testing.T) {
	s, _, _, admin, input := tokenFixture(t)
	ctx := context.Background()
	_, err := s.CreateResourceGrant(ctx, admin.Secret, admin.CSRF, GrantRequest{PrincipalID: admin.Identity.SubjectID, ResourceType: "node", ResourceID: "node-1", Permissions: []string{"nodes.read"}})
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.IssueRestrictedToken(ctx, admin.Secret, admin.CSRF, input)
	if err != nil {
		t.Fatal(err)
	}
	request := func() *http.Request {
		r := httptest.NewRequest("GET", "https://fleet.test/api/v1/nodes", nil)
		r.Header.Set("Authorization", "Bearer "+token.Secret)
		return r
	}
	if got, err := RestrictedBearerCredential(request()); err != nil || got != token.Secret {
		t.Fatal(got, err)
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+token.Secret) }, func(r *http.Request) { r.Header.Set("Cookie", "") }, func(r *http.Request) { r.Header.Set("Origin", "") }, func(r *http.Request) { r.TLS = nil }, func(r *http.Request) { r.Method = "POST" }, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "none") }, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token.Secret+" ") }} {
		r := request()
		mutate(r)
		if _, err = RestrictedBearerCredential(r); err == nil {
			t.Fatal("ambiguous/browser token accepted")
		}
	}
	h, _ := NewIdentityHTTP(s, IdentityHTTPConfig{Origin: "https://fleet.test", EnableRestrictedTokens: true})
	r := request()
	r.URL.Path = "/api/v1/auth/me"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("token escaped into cookie APIs", w.Code)
	}
	body, _ := json.Marshal(input)
	w = authRequest(t, h, "POST", "/api/v1/auth/tokens", string(body), admin.Secret, admin.CSRF, nil)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"token":`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = authRequest(t, h, "GET", "/api/v1/auth/tokens", "", admin.Secret, "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"token":`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
