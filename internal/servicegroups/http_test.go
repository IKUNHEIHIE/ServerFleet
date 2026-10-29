package servicegroups_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"serverfleet.local/serverfleet/internal/servicegroups"
)

func TestHTTPCreateListAndMembers(t *testing.T) {
	t.Parallel()
	h := newFixture(t, nil, nil, nil)
	mux := http.NewServeMux()
	if err := servicegroups.RegisterHTTP(mux, h.service, func(*http.Request) (servicegroups.Principal, error) {
		return servicegroups.Principal{SubjectID: "admin", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	invalid, _ := json.Marshal(map[string]any{"name": "bad", "memberNodeIds": []string{"n1"}})
	bad, err := http.Post(server.URL+"/api/v1/service-groups", "application/json", bytes.NewReader(invalid))
	if err != nil || bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid = %v %d", err, bad.StatusCode)
	}
	bad.Body.Close()

	body, _ := json.Marshal(map[string]any{
		"name": "Web", "memberNodeIds": []string{"n1"},
		"service":       map[string]any{"port": 8080, "protocol": "http"},
		"loadBalancing": map[string]any{"enabled": true, "strategy": "round_robin"},
	})
	create, err := http.Post(server.URL+"/api/v1/service-groups", "application/json", bytes.NewReader(body))
	if err != nil || create.StatusCode != http.StatusCreated {
		t.Fatalf("create = %v %d", err, create.StatusCode)
	}
	var group struct {
		ID       string `json:"id"`
		Revision uint64 `json:"revision"`
	}
	json.NewDecoder(create.Body).Decode(&group)
	create.Body.Close()
	if group.ID == "" || group.Revision != 1 {
		t.Fatalf("group = %+v", group)
	}
	list, err := http.Get(server.URL + "/api/v1/service-groups")
	if err != nil || list.StatusCode != http.StatusOK {
		t.Fatalf("list = %v %d", err, list.StatusCode)
	}
	list.Body.Close()
	members, _ := json.Marshal(map[string]any{"memberNodeIds": []string{"n1", "n2"}, "expectedRevision": 1})
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/api/v1/service-groups/"+group.ID+"/members", bytes.NewReader(members))
	request.Header.Set("Content-Type", "application/json")
	replace, err := http.DefaultClient.Do(request)
	if err != nil || replace.StatusCode != http.StatusOK {
		t.Fatalf("members = %v %d", err, replace.StatusCode)
	}
	replace.Body.Close()
}
