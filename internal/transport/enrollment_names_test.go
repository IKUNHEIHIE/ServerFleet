package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnrollmentManagementNamesAreDistinctAndPreserveExplicitName(t *testing.T) {
	s, _, _ := enrollmentFixture(t)
	h, err := NewEnrollmentManagementHTTP(s, func(*http.Request) (EnrollmentPrincipal, error) {
		return EnrollmentPrincipal{SubjectID: "admin", RequestID: "name-test", Administrator: true}, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for i, body := range []string{`{}`, `{}`, `{"name":"  东京节点  "}`} {
		r := httptest.NewRequest("POST", "https://fleet.test/api/v1/node-enrollment-tokens", strings.NewReader(body))
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 && w.Code != 201 {
			t.Fatalf("issue %d: %d", i, w.Code)
		}
		var token EnrollmentToken
		if err := json.NewDecoder(bytes.NewReader(w.Body.Bytes())).Decode(&token); err != nil {
			t.Fatal(err)
		}
		_, err := s.Submit(context.Background(), AgentEnrollmentInput{EnrollmentToken: token.Token, RequestID: []string{"name-a", "name-b", "name-c"}[i], CSRPEM: enrollmentCSR(t), PinnedRootSPKI: s.issuer.rootPin, AgentVersion: "test", Platform: "linux"})
		if err != nil {
			t.Fatal(err)
		}
		views, err := s.List(context.Background(), EnrollmentOperation{ActorID: "admin", Action: "enrollment.list"})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range views {
			if v.ID == []string{"name-a", "name-b", "name-c"}[i] {
				names = append(names, v.Name)
			}
		}
	}
	if len(names) != 3 || names[0] == names[1] || !strings.HasPrefix(names[0], "node-") || names[2] != "东京节点" {
		t.Fatalf("names=%v", names)
	}
}
