package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type gateFunc func(context.Context, string) error

func (f gateFunc) AuthorizeAuditRead(ctx context.Context, subject string) error {
	return f(ctx, subject)
}

func newHTTPFixture(t *testing.T, gate audit.ReadGate) (*audit.SQLRecorder, *httptest.Server) {
	t.Helper()
	db := testkit.NewDB(t, audit.Migrations())
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator([]string{"audit.read", "nodes.update"}, limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Append(context.Background(), audit.Event{
		SubjectID: "user-1", Action: "nodes.update", Result: "success", RequestID: "req-1",
		Source: audit.SourceWeb, At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := audit.Register(mux, recorder,
		func(r *http.Request) (audit.Principal, error) {
			if r.Header.Get("X-Test-Unauthorized") == "1" {
				return audit.Principal{}, errors.New("no session")
			}
			return audit.Principal{SubjectID: "admin-1", RequestID: "req-http"}, nil
		}, gate); err != nil {
		t.Fatal(err)
	}
	return recorder, httptest.NewServer(mux)
}

func TestAuditHTTPListAndGet(t *testing.T) {
	t.Parallel()
	recorder, server := newHTTPFixture(t, gateFunc(func(context.Context, string) error { return nil }))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/v1/audit/events?limit=10")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			EventID    string `json:"eventId"`
			ActorType  string `json:"actorType"`
			ActorID    string `json:"actorId"`
			Action     string `json:"action"`
			Result     string `json:"result"`
			RequestID  string `json:"requestId"`
			ResourceID string `json:"resourceId"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(page.Items) != 1 || page.Items[0].EventID == "" || page.Items[0].ActorType != "user" ||
		page.Items[0].Action != "nodes.update" || page.Items[0].RequestID != "req-1" {
		t.Fatalf("page = %+v", page)
	}
	get, err := http.Get(server.URL + "/api/v1/audit/events/" + page.Items[0].EventID)
	if err != nil {
		t.Fatal(err)
	}
	if get.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", get.StatusCode)
	}
	get.Body.Close()
	bad, _ := http.Get(server.URL + "/api/v1/audit/events/not-a-number")
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad id status = %d", bad.StatusCode)
	}
	bad.Body.Close()
	oversized, _ := http.Get(server.URL + "/api/v1/audit/events?limit=500")
	if oversized.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized limit status = %d", oversized.StatusCode)
	}
	oversized.Body.Close()
	count, err := recorder.Count(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("count = %d, %v", count, err)
	}
}

func TestAuditHTTPDeniedReadIsAudited(t *testing.T) {
	t.Parallel()
	recorder, server := newHTTPFixture(t, gateFunc(func(context.Context, string) error { return errors.New("no role") }))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/v1/audit/events")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", response.StatusCode)
	}
	response.Body.Close()
	page, err := recorder.Query(context.Background(), audit.Query{Action: "audit.read", Result: "denied"})
	if err != nil || page.Total != 1 || page.Items[0].SubjectID != "admin-1" {
		t.Fatalf("denial = %+v, %v", page, err)
	}
}

func TestAuditHTTPUnauthorized(t *testing.T) {
	t.Parallel()
	_, server := newHTTPFixture(t, gateFunc(func(context.Context, string) error { return nil }))
	defer server.Close()
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/audit/events", nil)
	request.Header.Set("X-Test-Unauthorized", "1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.StatusCode)
	}
	response.Body.Close()
}
