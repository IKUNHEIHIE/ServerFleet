package updates

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskInstallHTTPExecutesVerifiedSlotAndRejectsReplay(t *testing.T) {
	f := installTestFixture(t)
	apply, err := NewTaskInstallExecutor(f.s, func(_ context.Context, r ApplyRequest) (PreparedInstall, error) {
		return PreparedInstall{PlanID: r.PlanID, SubjectID: r.Principal.SubjectID, Staged: f.staged, Policy: f.policy, Intent: f.intent, Hooks: f.hooks}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := NewHTTP(Options{Authenticate: func(*http.Request) (Principal, error) { return Principal{SubjectID: "admin", Administrator: true}, nil }, Apply: apply})
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://fleet.test/api/v1/system/updates/apply", strings.NewReader(`{"planId":"approved-plan","confirmation":"synthetic-confirmation","backupMode":"required"}`))
		r.TLS = &tls.ConnectionState{}
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := request()
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result ApplyResult
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Status != "installed" || result.ID != f.intent.ExecutionID {
		t.Fatal(w.Body.String())
	}
	w = request()
	if w.Code != 503 {
		t.Fatal("replayed installation", w.Code)
	}
}
func TestDirectorySourceRejectsUnsignedTamperedAndEscapingArtifacts(t *testing.T) {
	stager, raw, sig, body, policy := stagingFixture(t)
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	release := filepath.Join(dir, "0.9.0")
	os.Mkdir(release, 0700)
	os.WriteFile(filepath.Join(release, "manifest.json"), raw, 0600)
	os.WriteFile(filepath.Join(release, "signature.bin"), sig, 0600)
	os.WriteFile(filepath.Join(release, "artifact"), body, 0600)
	source, err := NewDirectoryReleaseSource(dir, stager)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	staged, err := source.Stage(context.Background(), "0.9.0", "test-key", policy)
	if err != nil {
		t.Fatal(err)
	}
	staged.Close()
	if _, err = source.Stage(context.Background(), "../0.9.0", "test-key", policy); err == nil {
		t.Fatal("escaped inbox")
	}
	os.WriteFile(filepath.Join(release, "artifact"), []byte("tampered"), 0600)
	if _, err = source.Stage(context.Background(), "0.9.0", "test-key", policy); err == nil {
		t.Fatal("tampered artifact")
	}
	os.WriteFile(filepath.Join(release, "artifact"), body, 0600)
	sig[0] ^= 1
	os.WriteFile(filepath.Join(release, "signature.bin"), sig, 0600)
	if _, err = source.Stage(context.Background(), "0.9.0", "test-key", policy); err == nil {
		t.Fatal("unsigned artifact")
	}
}
