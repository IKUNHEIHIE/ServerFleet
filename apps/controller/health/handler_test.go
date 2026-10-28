package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLivenessAndClosedManagementRoutes(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/healthz", http.StatusOK},
		{"POST", "/healthz", http.StatusMethodNotAllowed},
		{"GET", "/api/v1/nodes", http.StatusNotFound},
		{"POST", "/api/v1/tasks", http.StatusNotFound},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			Handler().ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			if tc.status == http.StatusOK {
				var body map[string]string
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body["status"] != "scaffold" {
					t.Fatalf("unexpected liveness body: %v", body)
				}
			}
		})
	}
}
