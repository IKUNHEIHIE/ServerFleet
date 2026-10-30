package files_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"serverfleet.local/serverfleet/internal/files"
)

func newHTTPFixture(t *testing.T) (*fixture, *httptest.Server) {
	t.Helper()
	f := newFixture(t)
	mux := http.NewServeMux()
	if err := files.RegisterHTTP(mux, f.service, func(*http.Request) (files.Principal, error) {
		return files.Principal{SubjectID: "user-1", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return f, httptest.NewServer(mux)
}

func TestHTTPTransferLifecycle(t *testing.T) {
	t.Parallel()
	f, server := newHTTPFixture(t)
	defer server.Close()
	body, _ := json.Marshal(map[string]any{
		"sourceNodeId": "n-src", "sourcePaths": []string{"/etc/app/config.yml"},
		"destinationNodeId": "n-dst", "destinationDir": "/srv/app", "conflictPolicy": "skip",
	})
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/transfers", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "idem-12345")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var job struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	json.NewDecoder(response.Body).Decode(&job)
	response.Body.Close()
	if job.ID == "" || job.Status != "queued" {
		t.Fatalf("job = %+v", job)
	}
	// Same idempotency key must return the same transfer.
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/transfers", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "idem-12345")
	again, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var duplicate struct {
		ID string `json:"id"`
	}
	json.NewDecoder(again.Body).Decode(&duplicate)
	again.Body.Close()
	if duplicate.ID != job.ID || len(f.submitter.commands) != 1 {
		t.Fatalf("duplicate = %+v commands=%d", duplicate, len(f.submitter.commands))
	}
	get, err := http.Get(server.URL + "/api/v1/transfers/" + job.ID)
	if err != nil || get.StatusCode != http.StatusOK {
		t.Fatalf("get = %v %d", err, get.StatusCode)
	}
	get.Body.Close()
	cancel, err := http.Post(server.URL+"/api/v1/transfers/"+job.ID+"/cancel", "application/json", nil)
	if err != nil || cancel.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel = %v %d", err, cancel.StatusCode)
	}
	cancel.Body.Close()
}

func TestHTTPUploadSession(t *testing.T) {
	t.Parallel()
	_, server := newHTTPFixture(t)
	defer server.Close()
	body, _ := json.Marshal(map[string]any{
		"destinationDir": "/srv/app",
		"files":          []map[string]any{{"name": "empty.txt", "sizeBytes": 0}},
	})
	response, err := http.Post(server.URL+"/api/v1/nodes/n-dst/files/uploads", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var session struct {
		UploadID string `json:"uploadId"`
		Status   string `json:"status"`
	}
	json.NewDecoder(response.Body).Decode(&session)
	response.Body.Close()
	if session.UploadID == "" || session.Status != "awaiting_data" {
		t.Fatalf("session = %+v", session)
	}
	complete, err := http.Post(server.URL+"/api/v1/nodes/n-dst/files/uploads/"+session.UploadID+"/complete", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if complete.StatusCode != http.StatusOK {
		t.Fatalf("complete status = %d", complete.StatusCode)
	}
	complete.Body.Close()
}

func TestHTTPTransferRequiresIdempotencyAndAuth(t *testing.T) {
	t.Parallel()
	_, server := newHTTPFixture(t)
	defer server.Close()
	response, err := http.Post(server.URL+"/api/v1/transfers", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", response.StatusCode)
	}
	response.Body.Close()
}
