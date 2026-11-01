//go:build cx_r1_controller

package platform_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/ai"
	"serverfleet.local/serverfleet/internal/auth"
)

// Uses only the minimal proposed Controller patch through Go overlay. The test
// is explicitly tagged until the integration owner adopts the shared Config.
func TestR1ControllerHTTPBorrowedBYOKLocalTLSBudgetAndTypedAudit(t *testing.T) {
	ctx := context.Background()
	const key = "synthetic-r1-local-provider-key"
	var calls atomic.Int32
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer "+key || r.TLS == nil {
			t.Error("wrong real supplier boundary")
		}
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		terminal := map[string]any{"id": "local-response", "object": "response", "status": "completed", "model": "fixture-model", "store": false, "output": []any{map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]string{"type": "output_text", "text": "controlled local generation"}}}}}
		raw, _ := json.Marshal(terminal)
		fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0}\n\nevent: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":1,\"delta\":\"controlled local generation\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":%s}\n\n", raw)
	}))
	defer provider.Close()
	roots := x509.NewCertPool()
	roots.AddCert(provider.Certificate())
	pki := newTestPKI(t)
	host := httptest.NewUnstartedServer(nil)
	port := strconv.Itoa(host.Listener.Addr().(*net.TCPAddr).Port)
	origin := "https://localhost:" + port
	var budget *ai.PersistentGenerationBudget
	composed, err := server.New(ctx, server.Config{DBPath: filepath.Join(t.TempDir(), "controller.db"), Origin: origin, AITypedAudit: true, LocalAIProvider: &ai.LocalTLSProviderConfig{Origin: provider.URL, Roots: roots}, AIGenerationQuota: func(ctx context.Context, r ai.GenerationRequest) error {
		if budget == nil {
			return ai.ErrModelUnavailable
		}
		return budget.Admit(ctx, r)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer composed.Close()
	for _, q := range append(ai.GenerationBudgetSchemaStatements(), ai.ModelSettingsAuditSchemaStatements()...) {
		if _, err = composed.DB.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	host.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pki.serverCert}}
	host.Config.Handler = composed
	host.StartTLS()
	defer host.Close()
	jar, _ := cookiejar.New(nil)
	client := newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", port), jar)
	sequence := 0
	request := func(method, path string, body any, csrf string) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, origin+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Requested-With", "ServerFleetWeb")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		sequence++
		if method == "POST" {
			req.Header.Set("Idempotency-Key", fmt.Sprintf("fixture-request-%04d", sequence))
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, raw
	}
	setup, _ := composed.Identity.PrepareSetup(ctx)
	if status, body := request("POST", "/api/v1/auth/bootstrap", map[string]string{"setupToken": setup, "username": "admin", "password": "synthetic-long-r1-password"}, ""); status != 200 && status != 204 {
		t.Fatal(status, string(body))
	}
	status, raw := request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "synthetic-long-r1-password"}, "")
	if status != 200 {
		t.Fatal(status, string(raw))
	}
	var login struct {
		Identity struct {
			UserID string `json:"userId"`
		} `json:"identity"`
		CSRF string `json:"csrfToken"`
	}
	json.Unmarshal(raw, &login)
	status, raw = request("POST", "/api/v1/credentials", map[string]any{"name": "local protocol fixture", "kind": "ai_api_key", "secret": key, "usageScope": []string{"ai"}}, login.CSRF)
	if status != 201 && status != 200 {
		t.Fatal(status, string(raw))
	}
	var credential struct {
		ID string `json:"id"`
	}
	json.Unmarshal(raw, &credential)
	if _, err = composed.DB.Exec(ctx, `CREATE TRIGGER reject_r1_model_audit BEFORE INSERT ON cx_ai_model_audit BEGIN SELECT RAISE(ABORT,'synthetic typed audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	status, _ = request("PUT", "/api/v1/ai/model-settings", map[string]any{"expectedRevision": 0, "enabled": true, "provider": "openai-responses", "model": "fixture-model", "credentialRef": credential.ID, "credentialRevision": 1}, login.CSRF)
	if status != 503 {
		t.Fatal("audit failure committed model configuration", status)
	}
	status, raw = request("GET", "/api/v1/ai/model-settings", nil, login.CSRF)
	var unchanged ai.ModelSettings
	json.Unmarshal(raw, &unchanged)
	if status != 200 || unchanged.Revision != 0 {
		t.Fatal("audit failure advanced model revision")
	}
	if _, err = composed.DB.Exec(ctx, `DROP TRIGGER reject_r1_model_audit`); err != nil {
		t.Fatal(err)
	}
	status, raw = request("PUT", "/api/v1/ai/model-settings", map[string]any{"expectedRevision": 0, "enabled": true, "provider": "openai-responses", "model": "fixture-model", "credentialRef": credential.ID, "credentialRevision": 1}, login.CSRF)
	if status != 200 {
		t.Fatal(status, string(raw))
	}
	var model ai.ModelSettings
	json.Unmarshal(raw, &model)
	if login.Identity.UserID == "" || model.ID == "" {
		t.Fatal("fixture metadata identity/profile missing")
	}
	originURL, _ := url.Parse(origin)
	var sessionSecret string
	for _, cookie := range jar.Cookies(originURL) {
		if cookie.Name == auth.SessionCookieName {
			sessionSecret = cookie.Value
		}
	}
	budget, err = ai.NewPersistentGenerationBudget(newSQLBudget(t, composed.DB, new(atomic.Bool), new(atomic.Bool)), func(ctx context.Context, actor string, p ai.GenerationBudgetPolicy) error {
		if actor != p.OwnerID {
			return ai.ErrToolDenied
		}
		identity, err := composed.Identity.Authenticate(ctx, sessionSecret, "", false)
		if err != nil || !identity.Administrator || identity.SubjectID != actor {
			return ai.ErrToolDenied
		}
		return nil
	}, composed.AIConversation.CheckGenerationBudget)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = budget.Configure(ctx, login.Identity.UserID, 0, ai.GenerationBudgetPolicy{OwnerID: login.Identity.UserID, ProfileID: model.ID, WindowID: "local-verified-window", Revision: 1, StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Hour), RequestLimit: 1, InputByteLimit: 256000, OutputTokenLimit: 8192}); err != nil {
		t.Fatal(err)
	}
	status, raw = request("POST", "/api/v1/ai/sessions", map[string]any{}, login.CSRF)
	if status != 201 {
		t.Fatal(status, string(raw))
	}
	var session ai.SessionView
	json.Unmarshal(raw, &session)
	for i := 0; i < 2; i++ {
		status, raw = request("POST", "/api/v1/ai/sessions/"+session.ID+"/messages", map[string]any{"content": "local HTTP protocol question", "clientMessageId": fmt.Sprintf("client-%d", i), "scopeRevision": session.ScopeRevision}, login.CSRF)
		if status != 200 && status != 201 && status != 202 {
			t.Fatal(status, string(raw))
		}
		var message ai.Message
		json.Unmarshal(raw, &message)
		status, raw = request("POST", "/api/v1/ai/sessions/"+session.ID+"/messages/"+message.MessageID+"/generate", nil, login.CSRF)
		if i == 0 {
			var answer ai.Message
			json.Unmarshal(raw, &answer)
			if status != 200 || answer.Role != "assistant" || answer.RedactedContent != "controlled local generation" {
				t.Fatal(status, string(raw))
			}
		} else if status != 503 {
			t.Fatal("persistent quota not enforced", status, string(raw))
		}
	}
	if calls.Load() != 1 {
		t.Fatal("quota performed extra HTTP call", calls.Load())
	}
	var profile string
	var revision uint64
	if err = composed.DB.QueryRow(ctx, `SELECT profile_id,revision FROM cx_ai_model_audit WHERE subject_id=?`, login.Identity.UserID).Scan(&profile, &revision); err != nil || profile != model.ID || revision != 1 {
		t.Fatal("typed audit lost profile/revision", profile, revision, err)
	}
}
