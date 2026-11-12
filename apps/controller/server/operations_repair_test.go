package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"serverfleet.local/serverfleet/internal/auth"
	"testing"
)

func TestReadinessAndUpgradePreflightBeforeMigration(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database := filepath.Join(directory, "fleet.db")
	s, e := New(ctx, Config{DBPath: database, Origin: "https://fleet.test"})
	if e != nil {
		t.Fatal(e)
	}
	token, e := s.Identity.PrepareSetup(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Identity.Bootstrap(ctx, token, "admin", "synthetic-repair-password"); e != nil {
		t.Fatal(e)
	}
	request := func() int {
		r := httptest.NewRequest("GET", "https://fleet.test/readyz", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	if code := request(); code != 200 {
		t.Fatal("not ready", code)
	}
	s.runCancel()
	<-s.runDone
	if code := request(); code != 503 {
		t.Fatal("stopped scheduler reported ready", code)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	keys := filepath.Join(directory, "credential-keys")
	report, e := CheckUpgrade(ctx, database, keys)
	if e != nil || !report.CredentialsVerified {
		t.Fatal(report, e)
	}
	file := filepath.Join(keys, "credential-keys.json")
	raw, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	_ = os.Remove(file)
	if _, e = New(ctx, Config{DBPath: database, Origin: "https://fleet.test"}); e == nil {
		t.Fatal("missing old keys accepted")
	}
	var issue *auth.UpgradeError
	if !errors.As(e, &issue) || issue.Code != "CREDENTIAL_KEYRING_MISSING" {
		t.Fatal("non-specific upgrade failure", e)
	}
	if _, e = os.Stat(file); !os.IsNotExist(e) {
		t.Fatal("old keys regenerated")
	}
	_ = os.WriteFile(file, raw, 0600)
	restored, e := New(ctx, Config{DBPath: database, Origin: "https://fleet.test"})
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
}

func TestAccessWrapperPreservesFlushAndRecordsFailureStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapped := &accessResponse{ResponseWriter: recorder}
	wrapped.Header().Set("Content-Type", "application/json")
	wrapped.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(wrapped).Encode(map[string]string{"code": "HOST_MISMATCH"})
	wrapped.Flush()
	if wrapped.status != 403 || wrapped.bytes == 0 || !recorder.Flushed {
		t.Fatal("access wrapper changed response behavior")
	}
}
