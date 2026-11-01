package platform_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"serverfleet.local/serverfleet/apps/controller/server"
	"serverfleet.local/serverfleet/internal/auth"
)

func TestWave6ControllerKeyRestartAndOwnerModelHTTP(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	https := httptest.NewUnstartedServer(nil)
	origin := "https://" + https.Listener.Addr().String()
	cfg := server.Config{DBPath: filepath.Join(dir, "controller.db"), Origin: origin}
	first, err := server.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	https.Config.Handler = first
	https.StartTLS()
	defer https.Close()
	if first.FilesTransport != nil {
		t.Fatal("missing Agent channel silently selected a Controller-local node filesystem")
	}
	setup, err := first.Identity.PrepareSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Identity.Bootstrap(ctx, setup, "admin", "synthetic-wave6-admin-password"); err != nil {
		t.Fatal(err)
	}
	login, err := first.Identity.Login(ctx, "admin", "synthetic-wave6-admin-password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := first.Credentials.Create(ctx, login.Secret, login.CSRF, auth.CredentialInput{
		Name: "Synthetic review key", Kind: "ai_api_key", Secret: "synthetic-wave6-byok", UsageScope: []string{"ai"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := func(target *server.Server, method, path string, body []byte, csrf string) (int, []byte) {
		t.Helper()
		r := httptest.NewRequest(method, origin+path, bytes.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Requested-With", "ServerFleetWeb")
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: login.Secret})
		w := httptest.NewRecorder()
		target.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	body, _ := json.Marshal(map[string]any{"expectedRevision": 0, "enabled": true, "provider": "openai-responses", "model": "fixture-model", "credentialRef": credential.ID, "credentialRevision": credential.Revision})
	if status, raw := request(first, "PUT", "/api/v1/ai/model-settings", body, ""); status != 403 {
		t.Fatalf("missing CSRF = %d: %s", status, raw)
	}
	if status, raw := request(first, "PUT", "/api/v1/ai/model-settings", body, login.CSRF); status != 200 {
		t.Fatalf("save = %d: %s", status, raw)
	}
	if status, _ := request(first, "PUT", "/api/v1/ai/model-settings", body, login.CSRF); status != 409 {
		t.Fatal("stale model revision accepted")
	}
	// Exercise a real TLS socket too; Cookie authentication uses the same owner.
	r, _ := http.NewRequest("GET", https.URL+"/api/v1/ai/model-settings", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: login.Secret})
	r.Header.Set("X-Requested-With", "ServerFleetWeb")
	r.Header.Set("Origin", origin)
	response, err := https.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	raw, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil || response.StatusCode != 200 || bytes.Contains(raw, []byte("synthetic-wave6-byok")) {
		t.Fatalf("TLS metadata = %d: %s, %v", response.StatusCode, raw, readErr)
	}
	keyPath := filepath.Join(dir, "credential-keys", "credential-keys.json")
	keyBefore, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(keyBefore)
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := server.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Credentials.ServiceMetadata(ctx, login.Identity.SubjectID, credential.ID); err != nil {
		t.Fatal(err)
	}
	if status, raw := request(restarted, "GET", "/api/v1/ai/model-settings", nil, ""); status != 200 || !bytes.Contains(raw, []byte(`"revision":1`)) {
		t.Fatalf("persisted model settings = %d: %s", status, raw)
	}
	keyAfter, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(keyAfter)
	if !bytes.Equal(keyBefore, keyAfter) {
		t.Fatal("restart replaced the master key")
	}
	if err = restarted.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if unexpected, err := server.New(ctx, cfg); err == nil {
		unexpected.Close()
		t.Fatal("initialized Controller silently generated a replacement key")
	}
	if _, err = os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("missing key was replaced")
	}
}
