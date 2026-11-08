package files_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/files"
	"strings"
	"testing"
)

func TestHTTPResumeProjectionRequiresCurrentOwnerAndNodeBinding(t *testing.T) {
	f, server := newHTTPFixture(t)
	defer server.Close()
	digest := strings.Repeat("a", 64)
	id := createUploadSession(t, server.URL, "resume.bin", 7, digest)
	status, body := doRaw(t, "GET", server.URL+"/api/v1/nodes/n-dst/files/uploads/"+id+"?resumeBinding=true", nil)
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if status != 200 || value["destinationPath"] != "/srv/app/resume.bin" || value["sha256Expected"] != digest || value["bytesReceived"] != float64(0) {
		t.Fatalf("binding projection=%s status=%d", body, status)
	}
	status, body = doRaw(t, "GET", server.URL+"/api/v1/nodes/wrong-node/files/uploads/"+id+"?resumeBinding=true", nil)
	if status != 404 || strings.Contains(string(body), digest) || strings.Contains(string(body), "/srv/app") {
		t.Fatalf("wrong-node leak: %d %s", status, body)
	}
	for _, test := range []struct {
		name, subject string
		deny          bool
	}{{"wrong-owner", "user-2", false}, {"revoked", "user-1", true}} {
		t.Run(test.name, func(t *testing.T) {
			authorizer := f.authorizer
			if test.deny {
				authorizer = authorizerFunc(func(context.Context, auth.Check) error { return auth.ErrDenied })
			}
			service, e := files.NewService(files.Options{DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: authorizer, StagingDir: f.staging})
			if e != nil {
				t.Fatal(e)
			}
			mux := http.NewServeMux()
			e = files.RegisterHTTP(mux, service, func(*http.Request) (files.Principal, error) {
				return files.Principal{SubjectID: test.subject, RequestID: "resume-deny"}, nil
			})
			if e != nil {
				t.Fatal(e)
			}
			request := httptest.NewRequest("GET", "/api/v1/nodes/n-dst/files/uploads/"+id+"?resumeBinding=true", nil)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			expected := 409
			if test.deny {
				expected = 403
			}
			if response.Code != expected || strings.Contains(response.Body.String(), digest) || strings.Contains(response.Body.String(), "/srv/app") {
				t.Fatalf("denied projection leak: %d %s", response.Code, response.Body.String())
			}
		})
	}
	// Default/false projections retain the closed R8 response shape.
	for _, query := range []string{"", "?resumeBinding=false"} {
		status, body = doRaw(t, "GET", server.URL+"/api/v1/nodes/n-dst/files/uploads/"+id+query, nil)
		if status != 200 || strings.Contains(string(body), "destinationPath") || strings.Contains(string(body), "sha256Expected") {
			t.Fatalf("legacy response changed: %d %s", status, body)
		}
	}
	for _, query := range []string{"?resumeBinding=bad", "?resumeBinding=true&resumeBinding=false"} {
		status, body = doRaw(t, "GET", server.URL+"/api/v1/nodes/n-dst/files/uploads/"+id+query, nil)
		if status != 400 || strings.Contains(string(body), digest) {
			t.Fatalf("ambiguous binding query: %d %s", status, body)
		}
	}
	unhashed := createUploadSession(t, server.URL, "legacy.bin", 1, "")
	_, body = doRaw(t, "GET", server.URL+"/api/v1/nodes/n-dst/files/uploads/"+unhashed+"?resumeBinding=true", nil)
	if strings.Contains(string(body), "sha256Expected") {
		t.Fatalf("invented absent digest: %s", body)
	}
}
