package nodes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
)

func TestHTTPNodesGroupsAndClassifications(t *testing.T) {
	t.Parallel()
	policy := readPolicy("u1", []string{"n1"}, []string{"n1"})
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), policy,
		nodes.ServiceActions()...)
	seedNode(t, h.db, h.repo, "n1", "Node One")
	mux := http.NewServeMux()
	if err := nodes.RegisterHTTP(mux, h.svc, func(*http.Request) (nodes.Principal, error) {
		return nodes.Principal{SubjectID: "u1", RequestID: "req-http"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	defer server.Close()

	list, err := http.Get(server.URL + "/api/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	var nodesPage struct {
		Items []struct {
			ID            string   `json:"id"`
			CPUPercent    *float64 `json:"cpuPercent"`
			MemoryPercent *float64 `json:"memoryPercent"`
		} `json:"items"`
	}
	json.NewDecoder(list.Body).Decode(&nodesPage)
	list.Body.Close()
	if len(nodesPage.Items) != 1 || nodesPage.Items[0].CPUPercent != nil || nodesPage.Items[0].MemoryPercent != nil {
		t.Fatalf("nodes = %+v", nodesPage)
	}

	body, _ := json.Marshal(map[string]any{"name": "Maintenance", "description": "patching"})
	create, err := http.Post(server.URL+"/api/v1/groups", "application/json", bytes.NewReader(body))
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

	members, _ := json.Marshal(map[string]any{"memberNodeIds": []string{"n1"}, "expectedRevision": 1})
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/api/v1/groups/"+group.ID+"/members", bytes.NewReader(members))
	request.Header.Set("Content-Type", "application/json")
	replace, err := http.DefaultClient.Do(request)
	if err != nil || replace.StatusCode != http.StatusOK {
		t.Fatalf("members = %v %d", err, replace.StatusCode)
	}
	replace.Body.Close()

	facets, err := http.Get(server.URL + "/api/v1/groups/classifications?dimension=provider")
	if err != nil || facets.StatusCode != http.StatusOK {
		t.Fatalf("facets = %v %d", err, facets.StatusCode)
	}
	facets.Body.Close()
}
