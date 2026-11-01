package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelHTTPRequiresCurrentOwnerAndRevision(t *testing.T) {
	m, s, _, actor, _, _ := modelSettingsFixture(t)
	s.access = func(ctx context.Context, _ string, _ string) (SessionActor, error) {
		a, check, ok := ActorFromContext(ctx)
		if !ok || check == nil || check(ctx) != nil {
			return SessionActor{}, ErrToolDenied
		}
		return a, nil
	}
	current := true
	h, err := NewModelSettingsHTTP(m, func(*http.Request) (Principal, error) {
		return Principal{SubjectID: actor.SubjectID, Administrator: true, Recheck: func(context.Context) error {
			if !current {
				return ErrToolDenied
			}
			return nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://fleet.test/api/v1/ai/model-settings", strings.NewReader(body))
		if method == "PUT" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	input := `{"expectedRevision":0,"enabled":true,"provider":"openai-responses","model":"fixture-model","credentialRef":"credential-ref","credentialRevision":1}`
	if w := call("PUT", input); w.Code != 200 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("PUT", input); w.Code != 409 {
		t.Fatal("stale configuration accepted", w.Code)
	}
	actor.SubjectID = "other-owner"
	if w := call("GET", ""); w.Code != 200 || strings.Contains(w.Body.String(), "credential-ref") {
		t.Fatal("owner switch disclosed saved config", w.Body.String())
	}
	current = false
	if w := call("GET", ""); w.Code != 403 {
		t.Fatal("revocation ignored", w.Code)
	}
}
