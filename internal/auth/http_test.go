package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func authRequest(t *testing.T, h *IdentityHTTP, method, path, body, secret, csrf string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "https://fleet.test"+path, strings.NewReader(body))
	r.Header.Set("Origin", "https://fleet.test")
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if secret != "" {
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: secret})
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, r)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache policy missing")
	}
	return response
}
func httpLogin(t *testing.T, h *IdentityHTTP, username, password string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	response := authRequest(t, h, "POST", "/api/v1/auth/login", string(body), "", "", nil)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var result struct {
		Identity IdentityView `json:"identity"`
		CSRF     string       `json:"csrfToken"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Domain != "" {
		t.Fatal("cookie policy")
	}
	if result.Identity.UserID == "" || !validSecret(result.CSRF) || bytes.Contains(response.Body.Bytes(), []byte(cookies[0].Value)) || bytes.Contains(response.Body.Bytes(), []byte(password)) {
		t.Fatal("invalid identity/secret disclosure")
	}
	return cookies[0].Value, result.CSRF
}

func TestIdentityHTTPUsersReplayPaginationDemotionAndSessions(t *testing.T) {
	s, _, _ := identityFixture(t)
	h, err := NewIdentityHTTP(s, IdentityHTTPConfig{Origin: "https://fleet.test"})
	if err != nil {
		t.Fatal(err)
	}
	admin, csrf := httpLogin(t, h, "admin", identityTestPassword)
	input := `{"username":"reader","password":"long-other-test-password","roleIds":["reader"]}`
	headers := map[string]string{"Idempotency-Key": "create-user-key-0001"}
	response := authRequest(t, h, "POST", "/api/v1/users", input, admin, csrf, headers)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var user ManagedUser
	_ = json.Unmarshal(response.Body.Bytes(), &user)
	replay := authRequest(t, h, "POST", "/api/v1/users", input, admin, csrf, headers)
	var duplicate ManagedUser
	_ = json.Unmarshal(replay.Body.Bytes(), &duplicate)
	if replay.Code != 200 || duplicate.UserID != user.UserID {
		t.Fatal("replay created another user", replay.Code)
	}
	changed := strings.Replace(input, "long-other-test-password", "different-long-password", 1)
	if response = authRequest(t, h, "POST", "/api/v1/users", changed, admin, csrf, headers); response.Code != 409 {
		t.Fatal("changed password reused receipt", response.Code)
	}
	response = authRequest(t, h, "GET", "/api/v1/users?limit=1", "", admin, "", nil)
	var first struct {
		Items []ManagedUser `json:"items"`
		Page  authPageMeta  `json:"page"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &first)
	if response.Code != 200 || len(first.Items) != 1 || !first.Page.HasMore || first.Page.NextCursor == nil {
		t.Fatal(response.Code, response.Body.String())
	}
	response = authRequest(t, h, "GET", "/api/v1/users?limit=1&cursor="+*first.Page.NextCursor, "", admin, "", nil)
	var second struct {
		Items []ManagedUser `json:"items"`
		Page  authPageMeta  `json:"page"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &second)
	if response.Code != 200 || len(second.Items) != 1 || second.Page.HasMore || second.Items[0].UserID == first.Items[0].UserID {
		t.Fatal(response.Code, response.Body.String())
	}
	reader, readerCSRF := httpLogin(t, h, "reader", "long-other-test-password")
	if response = authRequest(t, h, "GET", "/api/v1/users", "", reader, "", nil); response.Code != 403 {
		t.Fatal("reader listed identities", response.Code)
	}
	if response = authRequest(t, h, "POST", "/api/v1/users", input, reader, readerCSRF, headers); response.Code != 403 {
		t.Fatal("reader created user", response.Code)
	}
	if response = authRequest(t, h, "PATCH", "/api/v1/users/"+user.UserID, `{"enabled":false}`, admin, csrf, nil); response.Code != 428 {
		t.Fatal(response.Code)
	}
	if response = authRequest(t, h, "PATCH", "/api/v1/users/"+user.UserID, `{"enabled":false}`, admin, csrf, map[string]string{"If-Match": `"99"`}); response.Code != 412 {
		t.Fatal(response.Code)
	}
	if response = authRequest(t, h, "PATCH", "/api/v1/users/"+user.UserID, `{"enabled":false}`, admin, csrf, map[string]string{"If-Match": `"1"`}); response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if response = authRequest(t, h, "GET", "/api/v1/auth/me", "", reader, "", nil); response.Code != 401 {
		t.Fatal("disabled session active", response.Code)
	}
	if response = authRequest(t, h, "POST", "/api/v1/auth/logout", "", admin, "", nil); response.Code != 403 {
		t.Fatal("missing CSRF logged out", response.Code)
	}
	if response = authRequest(t, h, "POST", "/api/v1/auth/logout", "", admin, csrf, nil); response.Code != 204 {
		t.Fatal(response.Code)
	}
	if response = authRequest(t, h, "GET", "/api/v1/auth/me", "", admin, "", nil); response.Code != 401 {
		t.Fatal("logout did not revoke", response.Code)
	}
}

func TestIdentityHTTPStrictInputsOriginAmbiguityAndUnavailable(t *testing.T) {
	s, m, _ := identityFixture(t)
	h, _ := NewIdentityHTTP(s, IdentityHTTPConfig{Origin: "https://fleet.test"})
	secret, csrf := httpLogin(t, h, "admin", identityTestPassword)
	for _, tc := range []struct {
		body    string
		headers map[string]string
		status  int
	}{
		{`{"username":"admin","username":"reader","password":"x"}`, nil, 400},
		{`null`, nil, 400}, {`{"username":"admin","password":null}`, nil, 400}, {`{"username":"admin","password":"x","role":"administrator"}`, nil, 400},
		{`{} {}`, nil, 400}, {`{"username":"admin","password":"x"}`, map[string]string{"Origin": "https://evil.test"}, 403},
		{`{}`, map[string]string{"Content-Type": "text/plain"}, 400}, {`{}`, map[string]string{"Authorization": "Bearer fake"}, 401},
		{`{"username":"admin","password":"x","secondFactorCode":"123456"}`, nil, 503},
	} {
		response := authRequest(t, h, "POST", "/api/v1/auth/login", tc.body, "", "", tc.headers)
		if response.Code != tc.status || response.Header().Get("Set-Cookie") != "" {
			t.Fatal(tc.body, response.Code, response.Body.String())
		}
	}
	if response := authRequest(t, h, "GET", "/api/v1/auth/me", "", secret, "", map[string]string{"Authorization": ""}); response.Code != 401 {
		t.Fatal("empty mixed header accepted", response.Code)
	}
	if response := authRequest(t, h, "GET", "/api/v1/users?limit=1&limit=2", "", secret, "", nil); response.Code != 400 {
		t.Fatal("duplicate query accepted", response.Code)
	}
	if response := authRequest(t, h, "PATCH", "/api/v1/users/unknown", `{}`, secret, csrf, map[string]string{"If-Match": `""1""`}); response.Code != 400 {
		t.Fatal("malformed ETag accepted", response.Code)
	}
	m.fail = true
	response := authRequest(t, h, "GET", "/api/v1/auth/me", "", secret, "", nil)
	if response.Code != 503 || strings.Contains(response.Body.String(), "database path") {
		t.Fatal(response.Code, response.Body.String())
	}
	m.fail = false
	if _, err := NewIdentityHTTP(s, IdentityHTTPConfig{Origin: "https://fleet.test", RequireSecondFactor: true}); err == nil {
		t.Fatal("required MFA silently ignored")
	}
	response = httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest("POST", "http://fleet.test/api/v1/auth/login", strings.NewReader(`{}`)))
	if response.Code != 403 {
		t.Fatal("HTTP accepted")
	}
}

type identityReadHook struct {
	IdentityStore
	after func()
}

func (s identityReadHook) View(ctx context.Context, fn func([]byte) error) error {
	err := s.IdentityStore.View(ctx, fn)
	if err == nil && s.after != nil {
		s.after()
	}
	return err
}
func TestIdentityHTTPBuffersAndDropsRevokedRead(t *testing.T) {
	s, m, _ := identityFixture(t)
	session := identityLogin(t, s)
	fired := false
	s.store = identityReadHook{m, func() {
		if fired {
			return
		}
		fired = true
		_ = m.Update(context.Background(), func(b []byte) ([]byte, error) {
			var state identityState
			_ = json.Unmarshal(b, &state)
			u := state.Users[session.Identity.SubjectID]
			u.Epoch++
			state.Users[u.ID] = u
			return json.Marshal(state)
		})
	}}
	h, _ := NewIdentityHTTP(s, IdentityHTTPConfig{Origin: "https://fleet.test"})
	response := authRequest(t, h, "GET", "/api/v1/auth/me", "", session.Secret, "", nil)
	if response.Code != 401 || strings.Contains(response.Body.String(), session.Identity.SubjectID) || strings.Contains(response.Body.String(), session.CSRF) || response.Header().Get("ETag") != "" {
		t.Fatal("revoked buffered data escaped", response.Code, response.Body.String())
	}
	_, _ = io.Copy(io.Discard, response.Result().Body)
}
