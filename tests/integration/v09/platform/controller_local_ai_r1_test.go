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
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/ai"
)

// Exercises the adopted Controller composition, registered migrations and
// default persistent budget, with no overlay or manually created schema.
func TestR1ControllerHTTPBorrowedBYOKLocalTLSBudgetAndTypedAudit(t *testing.T) {
	runControllerHTTPBorrowedBYOKBudget(t, false)
}

func runControllerHTTPBorrowedBYOKBudget(t *testing.T, budgetHTTP bool) {
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
	composed, err := server.New(ctx, server.Config{DBPath: filepath.Join(t.TempDir(), "controller.db"), Origin: origin, LocalAIProvider: &ai.LocalTLSProviderConfig{Origin: provider.URL, Roots: roots}})
	if err != nil {
		t.Fatal(err)
	}
	defer composed.Close()
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
	budget := composed.AIBudget
	now := time.Now().UTC()
	policy := ai.GenerationBudgetPolicy{OwnerID: login.Identity.UserID, ProfileID: model.ID, WindowID: "local-verified-window", Revision: 1, StartsAt: now.Add(-time.Minute), EndsAt: now.Add(time.Hour), RequestLimit: 1, InputByteLimit: 256000, OutputTokenLimit: 8192}
	wrongProfile := policy
	wrongProfile.ProfileID = "nonexistent-profile"
	if budget.Configure(ctx, login.Identity.UserID, 0, wrongProfile) == nil {
		t.Fatal("budget accepted another profile")
	}
	if budget.Configure(ctx, "another-actor", 0, policy) == nil {
		t.Fatal("budget accepted another owner")
	}
	budgetPath := "/api/v1/ai/model-settings/" + model.ID + "/budget"
	budgetInput := map[string]any{"expectedRevision": 0, "windowId": policy.WindowID, "startsAt": policy.StartsAt, "endsAt": policy.EndsAt, "requestLimit": policy.RequestLimit, "inputByteLimit": policy.InputByteLimit, "outputTokenLimit": policy.OutputTokenLimit}
	if budgetHTTP {
		status, raw = request("GET", budgetPath, nil, login.CSRF)
		var empty ai.GenerationBudgetView
		json.Unmarshal(raw, &empty)
		if status != 200 || empty.Configured || empty.Policy != nil {
			t.Fatal("missing policy not explicit", status, string(raw))
		}
		if status, _ = request("PUT", budgetPath, budgetInput, ""); status != 403 {
			t.Fatal("budget bypassed CSRF", status)
		}
		if status, _ = request("GET", "/api/v1/ai/model-settings/another-profile/budget", nil, login.CSRF); status != 403 {
			t.Fatal("budget accepted another profile", status)
		}
		budgetInput["ownerId"] = "other-owner"
		if status, _ = request("PUT", budgetPath, budgetInput, login.CSRF); status != 400 {
			t.Fatal("budget accepted body owner", status)
		}
		delete(budgetInput, "ownerId")
		status, raw = request("PUT", budgetPath, budgetInput, login.CSRF)
		if status != 200 {
			t.Fatal(status, string(raw))
		}
		status, raw = request("PUT", budgetPath, budgetInput, login.CSRF)
		if status != 409 || !bytes.Contains(raw, []byte("BUDGET_REVISION_CONFLICT")) {
			t.Fatal("budget CAS missing", status, string(raw))
		}
		if _, err = composed.DB.Exec(ctx, `CREATE TRIGGER reject_r2_budget_audit BEFORE INSERT ON cx_ai_budget_audit WHEN NEW.action='ai.budget.configure' BEGIN SELECT RAISE(ABORT,'forced budget audit boundary'); END`); err != nil {
			t.Fatal(err)
		}
		budgetInput["expectedRevision"] = 1
		if status, _ = request("PUT", budgetPath, budgetInput, login.CSRF); status != 503 {
			t.Fatal("budget audit failure did not reject", status)
		}
		composed.DB.Exec(ctx, `DROP TRIGGER reject_r2_budget_audit`)
		status, raw = request("GET", budgetPath, nil, login.CSRF)
		var view ai.GenerationBudgetView
		json.Unmarshal(raw, &view)
		if status != 200 || view.Policy == nil || view.Policy.Revision != 1 {
			t.Fatal("audit failure advanced budget", status, string(raw))
		}
		adminClient := client
		for _, role := range []string{"administrator", "operator"} {
			client = adminClient
			status, raw = request("POST", "/api/v1/users", map[string]any{"username": "budget-" + role, "password": "synthetic-second-owner-password", "roleIds": []string{role}}, login.CSRF)
			if status != 200 {
				t.Fatal(status, string(raw))
			}
			jar, _ := cookiejar.New(nil)
			client = newTestHTTPClient(pki, net.JoinHostPort("127.0.0.1", port), jar)
			status, raw = request("POST", "/api/v1/auth/login", map[string]any{"username": "budget-" + role, "password": "synthetic-second-owner-password"}, "")
			if status != 200 {
				t.Fatal(status, string(raw))
			}
			var other struct {
				CSRF string `json:"csrfToken"`
			}
			json.Unmarshal(raw, &other)
			if status, _ = request("GET", budgetPath, nil, other.CSRF); status != 403 {
				t.Fatal("other owner read budget", role, status)
			}
			if status, _ = request("PUT", budgetPath, budgetInput, other.CSRF); status != 403 {
				t.Fatal("other owner configured budget", role, status)
			}
		}
		client = adminClient
		encoded, _ := json.Marshal(budgetInput)
		req, _ := http.NewRequestWithContext(ctx, "PUT", origin+budgetPath, bytes.NewReader(encoded))
		req.Header.Set("Origin", "https://wrong-origin.invalid")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", login.CSRF)
		response, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatal("budget bypassed Origin", response.StatusCode)
		}
	} else if err = budget.Configure(ctx, login.Identity.UserID, 0, policy); err != nil {
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
	if budgetHTTP {
		status, raw = request("GET", budgetPath, nil, login.CSRF)
		var view ai.GenerationBudgetView
		json.Unmarshal(raw, &view)
		if status != 200 || view.Usage.Requests != 1 || view.Usage.OutputTokens != 8192 || view.Remaining.Requests != 0 {
			t.Fatal("budget usage missing", status, string(raw))
		}
		var before, after []byte
		if err = composed.DB.QueryRow(ctx, `SELECT payload FROM cx_ai_generation_budget`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		budgetInput["expectedRevision"] = 1
		budgetInput["requestLimit"] = 2
		status, raw = request("PUT", budgetPath, budgetInput, login.CSRF)
		json.Unmarshal(raw, &view)
		if status != 200 || view.Usage.Requests != 1 || view.Remaining.Requests != 1 || view.Policy.Revision != 2 {
			t.Fatal("CAS reset existing window usage", status, string(raw))
		}
		if err = composed.DB.QueryRow(ctx, `SELECT payload FROM cx_ai_generation_budget`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		var first, last map[string]json.RawMessage
		json.Unmarshal(before, &first)
		json.Unmarshal(after, &last)
		if !bytes.Equal(first["Operations"], last["Operations"]) || !bytes.Equal(first["Counters"], last["Counters"]) {
			t.Fatal("policy CAS changed operation fences/counters")
		}
		budgetInput["usage"] = map[string]int{"requests": 0}
		if status, _ = request("PUT", budgetPath, budgetInput, login.CSRF); status != 400 {
			t.Fatal("HTTP accepted counter reset", status)
		}
		delete(budgetInput, "usage")
	}
	var profile string
	var revision uint64
	if err = composed.DB.QueryRow(ctx, `SELECT profile_id,revision FROM cx_ai_model_audit WHERE subject_id=?`, login.Identity.UserID).Scan(&profile, &revision); err != nil || profile != model.ID || revision != 1 {
		t.Fatal("typed audit lost profile/revision", profile, revision, err)
	}
}
