package nodes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
	"testing"
	"time"
)

func TestNodeManagementMetadataHeartbeatConflictAndArchive(t *testing.T) {
	ctx := context.Background()
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), readPolicy("u1", []string{"n1"}, []string{"n1"}))
	seedNode(t, h.db, h.repo, "n1", "Before")
	mux := http.NewServeMux()
	if e := nodes.RegisterHTTP(mux, h.svc, func(*http.Request) (nodes.Principal, error) {
		return nodes.Principal{SubjectID: "u1", RequestID: "manage-test"}, nil
	}); e != nil {
		t.Fatal(e)
	}
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	get := call("GET", "/api/v1/nodes/n1", nil)
	if get.Code != 200 {
		t.Fatal(get.Code, get.Body.String())
	}
	var detail struct {
		UpdatedAt   string `json:"updatedAt"`
		DisplayName string `json:"displayName"`
	}
	if e := json.Unmarshal(get.Body.Bytes(), &detail); e != nil || detail.UpdatedAt == "" {
		t.Fatal(detail, e)
	}
	if _, _, e := h.repo.ApplyAgentUpdate(ctx, h.db, nodes.AgentUpdate{NodeID: "n1", Status: nodes.StatusOnline, Sequence: 1, ObservedAt: time.Now().UTC()}); e != nil {
		t.Fatal(e)
	}
	in := nodes.NodeMetadataInput{Name: "After", Address: "192.0.2.7", Region: "Tokyo", Provider: "AWS", CountryCode: "jp", Tags: []string{"web"}, ExpectedUpdatedAt: detail.UpdatedAt}
	updated := call("PATCH", "/api/v1/nodes/n1", in)
	if updated.Code != 200 {
		t.Fatal("heartbeat invalidated metadata", updated.Code, updated.Body.String())
	}
	n, e := h.repo.Get(ctx, "n1")
	if e != nil || n.Name != "After" || n.Status != nodes.StatusOnline || n.CountryCode != "JP" {
		t.Fatal(n, e)
	}
	if conflict := call("PATCH", "/api/v1/nodes/n1", in); conflict.Code != 409 {
		t.Fatal("stale metadata accepted", conflict.Code)
	}
	bad := map[string]any{"name": "Bad", "status": "online", "expectedUpdatedAt": detail.UpdatedAt}
	if w := call("PATCH", "/api/v1/nodes/n1", bad); w.Code != 400 {
		t.Fatal("status field accepted", w.Code)
	}
	if w := call("DELETE", "/api/v1/nodes/n1", map[string]string{"reason": ""}); w.Code != 400 {
		t.Fatal("empty reason accepted", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := call("DELETE", "/api/v1/nodes/n1", map[string]string{"reason": "retired"}); w.Code != 204 {
			t.Fatal("archive", w.Code, w.Body.String())
		}
	}
	listed, e := h.repo.List(ctx)
	if e != nil || len(listed) != 0 {
		t.Fatal("archive listed", listed, e)
	}
	n, e = h.repo.Get(ctx, "n1")
	if e != nil || n.Status != nodes.StatusRevoked {
		t.Fatal("lost archival record", n, e)
	}
	if _, _, e = h.repo.ApplyAgentUpdate(ctx, h.db, nodes.AgentUpdate{NodeID: "n1", Status: nodes.StatusOnline, Sequence: 2, ObservedAt: time.Now().UTC()}); !errors.Is(e, nodes.ErrNodeRevoked) {
		t.Fatal("revived archived node", e)
	}
	if w := call("GET", "/api/v1/nodes/n1", nil); w.Code != 404 {
		t.Fatal("archived detail visible", w.Code)
	}
}
func TestNodeManagementMutationDeniedAndAuditFailureRollback(t *testing.T) {
	ctx := context.Background()
	for _, auditFail := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "audit rollback"}[auditFail], func(t *testing.T) {
			allow := authorizerFunc(func(context.Context, auth.Check) error {
				if !auditFail {
					return auth.ErrDenied
				}
				return nil
			})
			h := newServiceFixture(t, allow, readPolicy("u1", []string{"n1"}, []string{"n1"}))
			n := seedNode(t, h.db, h.repo, "n1", "Before")
			if auditFail {
				if _, e := h.db.Exec(ctx, `CREATE TRIGGER reject_node_audit BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT, 'synthetic audit failure'); END`); e != nil {
					t.Fatal(e)
				}
			}
			in := nodes.NodeMetadataInput{Name: "Forbidden", CountryCode: "JP", Tags: []string{}, ExpectedUpdatedAt: n.CreatedAt.Format(time.RFC3339Nano)}
			if e := h.svc.UpdateMetadata(ctx, "u1", "patch-test", "n1", in); e == nil {
				t.Fatal("unauthorized/unaudited edit")
			}
			if e := h.svc.Archive(ctx, "u1", "archive-test", "n1", "retired"); e == nil {
				t.Fatal("unauthorized/unaudited archive")
			}
			got, e := h.repo.Get(ctx, "n1")
			if e != nil || got.Name != "Before" || got.Status == nodes.StatusRevoked {
				t.Fatal("mutation survived", got, e)
			}
		})
	}
}

func TestNodeManagementMetadataSnapshotConcurrentUpdate(t *testing.T) {
	ctx := context.Background()
	h := newServiceFixture(t, authorizerFunc(func(context.Context, auth.Check) error { return nil }), readPolicy("u1", []string{"n1"}, []string{"n1"}))
	seedNode(t, h.db, h.repo, "n1", "Before")
	mux := http.NewServeMux()
	if err := nodes.RegisterManagementHTTP(mux, h.svc, func(*http.Request) (nodes.Principal, error) {
		return nodes.Principal{SubjectID: "u1", RequestID: "snapshot-test"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	type detail struct {
		NodeID      string       `json:"nodeId"`
		DisplayName string       `json:"displayName"`
		Address     string       `json:"address"`
		Region      string       `json:"region"`
		Provider    string       `json:"provider"`
		CountryCode string       `json:"countryCode"`
		Tags        []string     `json:"tags"`
		Status      nodes.Status `json:"status"`
		LastSeenAt  string       `json:"lastSeenAt"`
		UpdatedAt   string       `json:"updatedAt"`
	}
	get := func() detail {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/nodes/n1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET: %d %s", w.Code, w.Body.String())
		}
		var got detail
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	// Literal expectations detect metadata paired with another snapshot's version.
	want := map[string]detail{
		"2026-01-01T00:00:00Z": {
			NodeID: "n1", DisplayName: "Alpha", Address: "192.0.2.10", Region: "Tokyo",
			Provider: "AWS", CountryCode: "JP", Tags: []string{"alpha", "web"},
			Status: nodes.StatusOffline, LastSeenAt: "", UpdatedAt: "2026-01-01T00:00:00Z",
		},
		"2026-01-02T00:00:00Z": {
			NodeID: "n1", DisplayName: "Beta", Address: "198.51.100.20", Region: "Paris",
			Provider: "OVH", CountryCode: "FR", Tags: []string{},
			Status: nodes.StatusOnline, LastSeenAt: "2026-01-02T01:02:03Z", UpdatedAt: "2026-01-02T00:00:00Z",
		},
	}
	if _, err := h.db.Exec(ctx, `UPDATE nodes SET name='Alpha',address='192.0.2.10',region='Tokyo',provider='AWS',country_code='JP',tags='["alpha","web"]',status='offline',last_seen_at=NULL,created_at='2026-01-01T00:00:00Z',metadata_updated_at='' WHERE id='n1'`); err != nil {
		t.Fatal(err)
	}
	if got := get(); !reflect.DeepEqual(got, want["2026-01-01T00:00:00Z"]) {
		t.Fatalf("created_at fallback: %+v", got)
	}
	// Each statement atomically commits a complete row and its CAS version.
	updates := []string{
		`UPDATE nodes SET name='Beta',address='198.51.100.20',region='Paris',provider='OVH',country_code='FR',tags='[]',status='online',last_seen_at='2026-01-02T03:02:03+02:00',metadata_updated_at='2026-01-02T00:00:00Z' WHERE id='n1'`,
		`UPDATE nodes SET name='Alpha',address='192.0.2.10',region='Tokyo',provider='AWS',country_code='JP',tags='["alpha","web"]',status='offline',last_seen_at=NULL,metadata_updated_at='2026-01-01T00:00:00Z' WHERE id='n1'`,
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			_, err := h.db.Exec(ctx, updates[i%len(updates)])
			if i == 0 {
				close(started)
			}
			if err != nil {
				done <- err
				return
			}
			runtime.Gosched()
		}
	}()
	defer func() {
		close(stop)
		if err := <-done; err != nil {
			t.Errorf("concurrent update: %v", err)
		}
	}()
	<-started
	for i := 0; i < 1000; i++ {
		got := get()
		expected, ok := want[got.UpdatedAt]
		if !ok || !reflect.DeepEqual(got, expected) {
			t.Fatalf("fields/CAS mismatch at read %d: got %+v; want %+v", i, got, expected)
		}
	}
}
