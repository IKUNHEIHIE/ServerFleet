package publishing_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/publishing"
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

type fakeGroupLookup struct {
	mu    sync.Mutex
	refs  map[string]publishing.GroupRef
	err   error
	calls []string
}

func (f *fakeGroupLookup) LookupGroup(_ context.Context, id string) (publishing.GroupRef, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id)
	if f.err != nil {
		return publishing.GroupRef{}, false, f.err
	}
	ref, ok := f.refs[id]
	return ref, ok, nil
}

func (f *fakeGroupLookup) setRevision(id string, revision uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ref := f.refs[id]
	ref.ID = id
	ref.Revision = revision
	f.refs[id] = ref
}

func (f *fakeGroupLookup) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

type callLog struct {
	mu     sync.Mutex
	events []publishing.StageKind
}

func (l *callLog) add(kind publishing.StageKind) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, kind)
}

func (l *callLog) snapshot() []publishing.StageKind {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]publishing.StageKind(nil), l.events...)
}

type rollbackCall struct {
	PlanID string
	Result publishing.StageResult
}

type fakeStage struct {
	kind        publishing.StageKind
	log         *callLog
	rollbackLog *callLog

	mu            sync.Mutex
	publishCount  int
	requests      []any
	result        publishing.StageResult
	err           error
	beforePublish func()
	rollbackCalls []rollbackCall
	rollbackErr   error
}

func (f *fakeStage) recordPublish(request any) (publishing.StageResult, error) {
	f.mu.Lock()
	f.publishCount++
	f.requests = append(f.requests, request)
	result, err, callback := f.result, f.err, f.beforePublish
	f.mu.Unlock()
	if f.log != nil {
		f.log.add(f.kind)
	}
	if callback != nil {
		callback()
	}
	return result, err
}

func (f *fakeStage) recordRollback(planID string, result publishing.StageResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rollbackCalls = append(f.rollbackCalls, rollbackCall{PlanID: planID, Result: result})
	if f.rollbackLog != nil {
		f.rollbackLog.add(f.kind)
	}
	return f.rollbackErr
}

func (f *fakeStage) set(result publishing.StageResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result, f.err = result, err
}

func (f *fakeStage) setBeforePublish(callback func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beforePublish = callback
}

func (f *fakeStage) setRollbackError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rollbackErr = err
}

func (f *fakeStage) counts() (publishes, rollbacks int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.publishCount, len(f.rollbackCalls)
}

func (f *fakeStage) lastRequest() any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func (f *fakeStage) lastRollback() rollbackCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rollbackCalls[len(f.rollbackCalls)-1]
}

type fakeDNS struct{ *fakeStage }

func (f *fakeDNS) Publish(_ context.Context, request publishing.DNSPublishRequest) (publishing.StageResult, error) {
	return f.recordPublish(request)
}

func (f *fakeDNS) Rollback(_ context.Context, planID string, result publishing.StageResult) error {
	return f.recordRollback(planID, result)
}

type fakeCertificate struct{ *fakeStage }

func (f *fakeCertificate) Publish(_ context.Context, request publishing.CertificateRequest) (publishing.StageResult, error) {
	return f.recordPublish(request)
}

func (f *fakeCertificate) Rollback(_ context.Context, planID string, result publishing.StageResult) error {
	return f.recordRollback(planID, result)
}

type fakeGateway struct{ *fakeStage }

func (f *fakeGateway) Publish(_ context.Context, request publishing.GatewayRequest) (publishing.StageResult, error) {
	return f.recordPublish(request)
}

func (f *fakeGateway) Rollback(_ context.Context, planID string, result publishing.StageResult) error {
	return f.recordRollback(planID, result)
}

type publicationPorts struct {
	groups      *fakeGroupLookup
	dns         *fakeDNS
	cert        *fakeCertificate
	gateway     *fakeGateway
	callLog     *callLog
	rollbackLog *callLog
}

func defaultPublicationPorts() *publicationPorts {
	log := &callLog{}
	rollbackLog := &callLog{}
	return &publicationPorts{
		groups: &fakeGroupLookup{refs: map[string]publishing.GroupRef{
			"sg1": {ID: "sg1", Revision: 3, NodeIDs: []string{"n2", "n1"}},
		}},
		dns:         &fakeDNS{&fakeStage{kind: publishing.StageDNS, log: log, rollbackLog: rollbackLog, result: publishing.StageResult{Summary: "dns ok"}}},
		cert:        &fakeCertificate{&fakeStage{kind: publishing.StageCertificate, log: log, rollbackLog: rollbackLog, result: publishing.StageResult{Summary: "certificate ok"}}},
		gateway:     &fakeGateway{&fakeStage{kind: publishing.StageGateway, log: log, rollbackLog: rollbackLog, result: publishing.StageResult{Summary: "gateway ok"}}},
		callLog:     log,
		rollbackLog: rollbackLog,
	}
}

type publicationFixture struct {
	db         *storage.DB
	repo       *publishing.Repo
	recorder   *audit.SQLRecorder
	service    *publishing.Service
	clock      *testClock
	ports      *publicationPorts
	authorizer *switchAuthorizer
	path       string
}

func newPublicationFixture(t *testing.T) *publicationFixture {
	t.Helper()
	return newPublicationFixtureAt(t, filepath.Join(t.TempDir(), "publishing.db"), nil, nil)
}

func newPublicationFixtureAt(t *testing.T, path string, ports *publicationPorts, authorizer auth.Authorizer) *publicationFixture {
	t.Helper()
	if ports == nil {
		ports = defaultPublicationPorts()
	}
	switched, _ := authorizer.(*switchAuthorizer)
	if authorizer == nil {
		switched = &switchAuthorizer{}
		authorizer = switched
	}
	db := testkit.NewDBAt(t, path, publishing.Migrations(), audit.Migrations())
	repo, err := publishing.NewRepo(db)
	if err != nil {
		t.Fatalf("NewRepo: %v", err)
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(publishing.ServiceActions(), limits)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatalf("NewSQLRecorder: %v", err)
	}
	clock := newTestClock()
	service, err := publishing.NewService(publishing.Options{
		DB: db, Repo: repo, Groups: ports.groups, DNS: ports.dns, Certificates: ports.cert,
		Gateway: ports.gateway, Audit: recorder, Authorizer: authorizer, Now: clock.Now,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return &publicationFixture{
		db: db, repo: repo, recorder: recorder, service: service, clock: clock,
		ports: ports, authorizer: switched, path: path,
	}
}

func (f *publicationFixture) createPlan(t *testing.T, id, domain, idempotencyKey string) publishing.Plan {
	t.Helper()
	plan, err := f.service.Create(context.Background(), "admin", "req-create-"+id, publishing.CreateInput{
		ID: id, Name: "Web", Domain: domain, ServiceGroupID: "sg1",
		ExpectedGroupRevision: 3, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return plan
}

func (f *publicationFixture) plan(t *testing.T, planID string) (publishing.Plan, []publishing.Stage) {
	t.Helper()
	plan, stages, err := f.repo.Get(context.Background(), planID)
	if err != nil {
		t.Fatalf("Get plan: %v", err)
	}
	return plan, stages
}

func (f *publicationFixture) auditCount(t *testing.T, action, result string) int {
	t.Helper()
	page, err := f.recorder.Query(context.Background(), audit.Query{Action: action, Result: result, Limit: 200})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	return page.Total
}

func newPublicationService(t *testing.T, f *publicationFixture, authorizer auth.Authorizer) *publishing.Service {
	t.Helper()
	service, err := publishing.NewService(publishing.Options{
		DB: f.db, Repo: f.repo, Groups: f.ports.groups, DNS: f.ports.dns,
		Certificates: f.ports.cert, Gateway: f.ports.gateway, Audit: f.recorder,
		Authorizer: authorizer, Now: f.clock.Now,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func stageByKind(t *testing.T, stages []publishing.Stage, kind publishing.StageKind) publishing.Stage {
	t.Helper()
	for _, stage := range stages {
		if stage.Kind == kind {
			return stage
		}
	}
	t.Fatalf("stage %q not found in %+v", kind, stages)
	return publishing.Stage{}
}

func (f *publicationFixture) verifyPublished(t *testing.T, planID string) {
	t.Helper()
	plan, stages := f.plan(t, planID)
	if plan.Status != publishing.PlanCompleted || plan.CurrentStage != publishing.StageGateway {
		t.Fatalf("plan = %+v", plan)
	}
	for _, stage := range stages {
		if stage.Status != publishing.StageSucceeded || stage.Attempts != 1 ||
			stage.StartedAt.IsZero() || stage.FinishedAt.IsZero() || stage.ErrorCode != "" {
			t.Fatalf("stage = %+v", stage)
		}
	}
}
