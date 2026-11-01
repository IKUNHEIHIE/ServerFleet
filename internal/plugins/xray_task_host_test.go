package plugins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestXrayHostSubmitsOnlyThroughAdmittedTaskPort(t *testing.T) {
	state := Installation{ID: "xray-manager", Version: "0.9.0", Installed: true, Enabled: true, Phase: "enabled", Revision: 1}
	allowed := true
	submits := 0
	h, err := NewXrayHTTPWithTaskHost(func(*http.Request) (LifecyclePrincipal, error) {
		return LifecyclePrincipal{SubjectID: "admin", RequestID: "request-one", Administrator: true}, nil
	}, func(context.Context) (Installation, error) { return state, nil }, nil, XrayTaskHost{Admit: func(_ context.Context, r XrayTaskRequest) error {
		if !allowed || r.ParametersSHA256 == "" || r.Operation != "xray.user.create" {
			return ErrPackageRejected
		}
		return nil
	}, Submit: func(_ context.Context, r XrayTaskRequest) (string, error) {
		submits++
		if r.Principal.SubjectID != "admin" || r.IdempotencyKey != "request-key-one" {
			t.Error("lost authorization binding")
		}
		return "unified-task-one", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://fleet.test/api/v1/plugins/xray-manager/users", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "request-key-one")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call(`{"name":"synthetic user","credentialRef":"reference-only"}`); w.Code != 202 || submits != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	allowed = false
	if w := call(`{}`); w.Code != 403 || submits != 1 {
		t.Fatal("host bypassed current authorization", w.Code)
	}
	allowed = true
	state.Enabled = false
	if w := call(`{}`); w.Code != 403 || submits != 1 {
		t.Fatal("disabled runtime submitted task")
	}
	state.Enabled = true
	if w := call(`{"privateKey":"forbidden"}`); w.Code != 400 || submits != 1 {
		t.Fatal("inline secret admitted")
	}
}
