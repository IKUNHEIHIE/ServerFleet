package files_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDistributionHTTPValidationAndRejectedStateReadback(t *testing.T) {
	f, server := newHTTPFixture(t)
	defer server.Close()
	request := func(method, path string, body []byte, want int, code string) map[string]any {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatalf("invalid JSON: %s %v", raw, err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, response.StatusCode, want, raw)
		}
		if code != "" {
			e, ok := value["error"].(map[string]any)
			if !ok || e["code"] != code {
				t.Fatalf("error code=%v want=%s", value, code)
			}
		}
		return value
	}
	for _, targets := range []string{`[]`, `["n-a","n-a"]`} {
		request("POST", "/api/v1/files/distributions", []byte(`{"destinationDir":"/srv","targetNodes":`+targets+`,"files":[{"name":"x","sizeBytes":1}]}`), 400, "invalid_request")
	}
	for _, tc := range []struct{ method, suffix string }{{"GET", ""}, {"PUT", "/content"}, {"POST", "/complete"}, {"POST", "/cancel"}} {
		request(tc.method, "/api/v1/files/distributions/missing"+tc.suffix, nil, 404, "not_found")
	}
	created := request("POST", "/api/v1/files/distributions", []byte(`{"destinationDir":"/srv","targetNodes":["n-a"],"files":[{"name":"x","sizeBytes":1,"sha256":"`+strings.Repeat("0", 64)+`"}]}`), 201, "")
	id := created["distributionId"].(string)
	base := "/api/v1/files/distributions/" + id
	request("PUT", base+"/content?offset=1", []byte("x"), 409, "conflict")
	request("PUT", base+"/content?offset=0", []byte("xx"), 413, "upload_too_large")
	request("PUT", base+"/content?offset=0", []byte("x"), 200, "")
	request("POST", base+"/complete", nil, 422, "checksum_mismatch")
	state := request("GET", base, nil, 200, "")
	if state["status"] != "failed" || state["sha256"] == "" || state["taskId"] != nil {
		t.Fatalf("false staging/task projection: %+v", state)
	}
	request("POST", base+"/complete", nil, 409, "conflict")
	request("PUT", base+"/content?offset=1", []byte("x"), 409, "conflict")
	if len(f.submitter.commands) != 0 {
		t.Fatal("rejected staging reached the task submitter")
	}
}
