package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"serverfleet.local/serverfleet/internal/audit"
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

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func createUploadSession(t *testing.T, base string, name string, size int64, digest string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"destinationDir": "/srv/app",
		"files":          []map[string]any{{"name": name, "sizeBytes": size, "sha256": digest}},
	})
	response, err := http.Post(base+"/api/v1/nodes/n-dst/files/uploads", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", response.StatusCode)
	}
	var session struct {
		UploadID string `json:"uploadId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.UploadID == "" {
		t.Fatal("upload session has no id")
	}
	return session.UploadID
}

func doRaw(t *testing.T, method, url string, body []byte) (int, []byte) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, buffer.Bytes()
}

func TestHTTPUploadContentAndCompleteChecksum(t *testing.T) {
	t.Parallel()
	f, server := newHTTPFixture(t)
	defer server.Close()
	content := []byte("hello world")
	digest := sha256Hex(content)
	uploadID := createUploadSession(t, server.URL, "app.bin", int64(len(content)), digest)
	base := server.URL + "/api/v1/nodes/n-dst/files/uploads/" + uploadID

	var progress struct {
		UploadID      string `json:"uploadId"`
		Status        string `json:"status"`
		BytesReceived int64  `json:"bytesReceived"`
	}
	status, body := doRaw(t, http.MethodPut, base+"/content", content[:4])
	if status != http.StatusOK {
		t.Fatalf("first chunk status = %d body=%s", status, body)
	}
	if err := json.Unmarshal(body, &progress); err != nil {
		t.Fatal(err)
	}
	if progress.UploadID != uploadID || progress.Status != "awaiting_data" || progress.BytesReceived != 4 {
		t.Fatalf("first progress = %+v", progress)
	}
	status, body = doRaw(t, http.MethodPut, base+"/content?offset=4", content[4:])
	if status != http.StatusOK {
		t.Fatalf("resumed chunk status = %d body=%s", status, body)
	}
	if err := json.Unmarshal(body, &progress); err != nil {
		t.Fatal(err)
	}
	if progress.BytesReceived != int64(len(content)) {
		t.Fatalf("resumed progress = %+v", progress)
	}
	if status, _ := doRaw(t, http.MethodPut, base+"/content?offset=3", content); status != http.StatusConflict {
		t.Fatalf("offset conflict status = %d", status)
	}
	if status, _ := doRaw(t, http.MethodPut, server.URL+"/api/v1/nodes/other/files/uploads/"+uploadID+"/content", content); status != http.StatusNotFound {
		t.Fatalf("node mismatch status = %d", status)
	}

	complete, err := http.Post(base+"/complete", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var completed struct {
		Status string `json:"status"`
		Files  []struct {
			SizeBytes int64  `json:"sizeBytes"`
			Version   string `json:"version"`
		} `json:"files"`
	}
	if err := json.NewDecoder(complete.Body).Decode(&completed); err != nil {
		t.Fatal(err)
	}
	complete.Body.Close()
	if complete.StatusCode != http.StatusOK || completed.Status != "completed" ||
		len(completed.Files) != 1 || completed.Files[0].SizeBytes != int64(len(content)) ||
		completed.Files[0].Version != digest {
		t.Fatalf("complete = %d %+v", complete.StatusCode, completed)
	}
	f.upload.mu.Lock()
	pushed, pushedSize, pushedDigest, calls := append([]byte(nil), f.upload.data...), f.upload.size, f.upload.digest, f.upload.calls
	f.upload.mu.Unlock()
	if !bytes.Equal(pushed, content) || pushedSize != int64(len(content)) || pushedDigest != digest || calls != 1 {
		t.Fatalf("pushed = %q size=%d digest=%s calls=%d", pushed, pushedSize, pushedDigest, calls)
	}
	page, err := f.audit.Query(context.Background(), audit.Query{Action: files.ActionUploadComplete, Result: "success"})
	if err != nil || page.Total == 0 {
		t.Fatalf("complete audit = %+v err=%v", page, err)
	}
}

func TestHTTPUploadContentRejections(t *testing.T) {
	t.Parallel()
	_, server := newHTTPFixture(t)
	defer server.Close()

	empty := createUploadSession(t, server.URL, "empty.bin", 3, sha256Hex([]byte("abc")))
	if status, body := doRaw(t, http.MethodPost, server.URL+"/api/v1/nodes/n-dst/files/uploads/"+empty+"/complete", nil); status != http.StatusBadRequest {
		t.Fatalf("complete without bytes status = %d body=%s", status, body)
	}

	mismatch := createUploadSession(t, server.URL, "mismatch.bin", 3, sha256Hex([]byte("xyz")))
	if status, body := doRaw(t, http.MethodPut, server.URL+"/api/v1/nodes/n-dst/files/uploads/"+mismatch+"/content", []byte("abc")); status != http.StatusOK {
		t.Fatalf("mismatch content status = %d body=%s", status, body)
	}
	if status, body := doRaw(t, http.MethodPost, server.URL+"/api/v1/nodes/n-dst/files/uploads/"+mismatch+"/complete", nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("mismatch complete status = %d body=%s", status, body)
	}

	oversize := createUploadSession(t, server.URL, "oversize.bin", 2, "")
	if status, body := doRaw(t, http.MethodPut, server.URL+"/api/v1/nodes/n-dst/files/uploads/"+oversize+"/content", []byte("abc")); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize content status = %d body=%s", status, body)
	}
	if status, _ := doRaw(t, http.MethodPut, server.URL+"/api/v1/nodes/n-dst/files/uploads/missing/content", []byte("abc")); status != http.StatusNotFound {
		t.Fatalf("missing upload status = %d", status)
	}
}
