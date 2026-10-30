package gateway_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type switchAuthorizer struct {
	mu   sync.Mutex
	deny bool
	err  error
}

func (a *switchAuthorizer) Authorize(context.Context, auth.Check) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	if a.deny {
		return auth.ErrDenied
	}
	return nil
}

func (a *switchAuthorizer) setDeny(value bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deny = value
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Now().UTC()}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Millisecond)
	return c.now
}

type fakeNodeLookup struct {
	mu    sync.Mutex
	nodes map[string]gateway.NodeRef
	err   error
	calls []string
}

func (f *fakeNodeLookup) LookupNode(_ context.Context, nodeID string) (gateway.NodeRef, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, nodeID)
	if f.err != nil {
		return gateway.NodeRef{}, false, f.err
	}
	node, ok := f.nodes[nodeID]
	return node, ok, nil
}

func (f *fakeNodeLookup) setOnline(nodeID string, online bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	node := f.nodes[nodeID]
	node.ID = nodeID
	node.Online = online
	f.nodes[nodeID] = node
}

func (f *fakeNodeLookup) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

type fakeServiceGroupLookup struct {
	mu     sync.Mutex
	groups map[string]gateway.ServiceGroupRef
	err    error
	calls  []string
}

func (f *fakeServiceGroupLookup) LookupServiceGroup(_ context.Context, id string) (gateway.ServiceGroupRef, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id)
	if f.err != nil {
		return gateway.ServiceGroupRef{}, false, f.err
	}
	group, ok := f.groups[id]
	return group, ok, nil
}

func (f *fakeServiceGroupLookup) setRevision(id string, revision uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groups[id] = gateway.ServiceGroupRef{ID: id, Revision: revision}
}

func (f *fakeServiceGroupLookup) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

type fakeChecker struct {
	mu     sync.Mutex
	result gateway.CheckResult
	err    error
	calls  []gateway.RouteSpec
}

func (f *fakeChecker) CheckRoute(_ context.Context, spec gateway.RouteSpec) (gateway.CheckResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, spec)
	return f.result, f.err
}

func (f *fakeChecker) set(result gateway.CheckResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result = result
	f.err = err
}

type fakeTaskSubmitter struct {
	mu    sync.Mutex
	next  int
	err   error
	calls []gateway.TaskRequest
}

func (f *fakeTaskSubmitter) Submit(_ context.Context, request gateway.TaskRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	request.NodeIDs = append([]string(nil), request.NodeIDs...)
	copied := make(map[string]string, len(request.Params))
	for key, value := range request.Params {
		copied[key] = value
	}
	request.Params = copied
	f.calls = append(f.calls, request)
	f.next++
	return fmt.Sprintf("task-%d", f.next), nil
}

func (f *fakeTaskSubmitter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeTaskSubmitter) lastRequest() gateway.TaskRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func (f *fakeTaskSubmitter) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

type gatewayPorts struct {
	nodes   *fakeNodeLookup
	groups  *fakeServiceGroupLookup
	checker *fakeChecker
	tasks   *fakeTaskSubmitter
}

func defaultGatewayPorts() *gatewayPorts {
	return &gatewayPorts{
		nodes: &fakeNodeLookup{nodes: map[string]gateway.NodeRef{
			"n1": {ID: "n1", Online: true},
			"n2": {ID: "n2", Online: false},
		}},
		groups: &fakeServiceGroupLookup{groups: map[string]gateway.ServiceGroupRef{
			"sg1": {ID: "sg1", Revision: 7},
			"sg2": {ID: "sg2", Revision: 3},
		}},
		checker: &fakeChecker{result: gateway.CheckResult{Passed: true}},
		tasks:   &fakeTaskSubmitter{},
	}
}

type gatewayFixture struct {
	db         *storage.DB
	repo       *gateway.Repo
	recorder   *audit.SQLRecorder
	service    *gateway.Service
	clock      *testClock
	ports      *gatewayPorts
	authorizer *switchAuthorizer
	path       string
}

func newGatewayFixture(t *testing.T) *gatewayFixture {
	t.Helper()
	return newGatewayFixtureAt(t, filepath.Join(t.TempDir(), "gateway.db"), nil, nil)
}

func newGatewayFixtureAt(t *testing.T, path string, ports *gatewayPorts, authorizer auth.Authorizer) *gatewayFixture {
	t.Helper()
	if ports == nil {
		ports = defaultGatewayPorts()
	}
	switched, _ := authorizer.(*switchAuthorizer)
	if authorizer == nil {
		switched = &switchAuthorizer{}
		authorizer = switched
	}
	db := testkit.NewDBAt(t, path, gateway.Migrations(), audit.Migrations())
	repo, err := gateway.NewRepo(db)
	if err != nil {
		t.Fatalf("NewRepo: %v", err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(gateway.ServiceActions(), limits)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatalf("NewSQLRecorder: %v", err)
	}
	clock := newTestClock()
	var checker gateway.Checker
	if ports.checker != nil {
		checker = ports.checker
	}
	service, err := gateway.NewService(gateway.Options{
		DB: db, Repo: repo, Nodes: ports.nodes, ServiceGroups: ports.groups,
		Checker: checker, Tasks: ports.tasks, Audit: recorder, Authorizer: authorizer,
		Now: clock.Now,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return &gatewayFixture{
		db: db, repo: repo, recorder: recorder, service: service, clock: clock,
		ports: ports, authorizer: switched, path: path,
	}
}

func (f *gatewayFixture) createGateway(t *testing.T, id, name string, enabled bool) gateway.GatewayNode {
	t.Helper()
	created, err := f.service.CreateGateway(context.Background(), "admin", "req-create-"+id, gateway.CreateGatewayInput{
		ID: id, NodeID: "n1", Name: name, Region: "hk", CountryCode: "hk", Enabled: enabled,
	})
	if err != nil {
		t.Fatalf("CreateGateway: %v", err)
	}
	return created
}

func (f *gatewayFixture) gateway(t *testing.T, id string) gateway.GatewayNode {
	t.Helper()
	record, err := f.repo.GetGateway(context.Background(), f.db, id)
	if err != nil {
		t.Fatalf("GetGateway: %v", err)
	}
	return record
}

func (f *gatewayFixture) routes(t *testing.T, gatewayID string) []gateway.GatewayRoute {
	t.Helper()
	routes, err := f.repo.ListRoutes(context.Background(), f.db, gatewayID)
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	return routes
}

func (f *gatewayFixture) provisions(t *testing.T, gatewayID string) []gateway.Provision {
	t.Helper()
	provisions, err := f.repo.ListProvisions(context.Background(), f.db, gatewayID)
	if err != nil {
		t.Fatalf("ListProvisions: %v", err)
	}
	return provisions
}

func (f *gatewayFixture) auditCount(t *testing.T, action, result string) int {
	t.Helper()
	page, err := f.recorder.Query(context.Background(), audit.Query{Action: action, Result: result, Limit: 200})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	return page.Total
}

func newGatewayService(t *testing.T, f *gatewayFixture, authorizer auth.Authorizer) *gateway.Service {
	t.Helper()
	var checker gateway.Checker
	if f.ports.checker != nil {
		checker = f.ports.checker
	}
	service, err := gateway.NewService(gateway.Options{
		DB: f.db, Repo: f.repo, Nodes: f.ports.nodes, ServiceGroups: f.ports.groups,
		Checker: checker, Tasks: f.ports.tasks, Audit: f.recorder, Authorizer: authorizer,
		Now: f.clock.Now,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func validRouteInput(id, domain string) gateway.CreateRouteInput {
	return gateway.CreateRouteInput{ID: id, Domain: domain, Upstream: "127.0.0.1:8080"}
}
