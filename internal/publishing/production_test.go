package publishing_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/publishing"
)

type fakePlanLookup struct{ plan publishing.Plan }

func (f *fakePlanLookup) GetPlan(context.Context, string) (publishing.Plan, error) {
	if f.plan.ID == "" {
		return publishing.Plan{}, publishing.ErrPlanNotFound
	}
	return f.plan, nil
}

type fakeDNSWriter struct {
	mu        sync.Mutex
	records   map[string]bool // id -> created by this bridge
	nextID    int
	failAt    int
	deletes   []string
	deleteErr error
}

func newFakeDNSWriter() *fakeDNSWriter {
	return &fakeDNSWriter{records: map[string]bool{}}
}

func (f *fakeDNSWriter) EnsureRecord(_ context.Context, _, _, _, _ string, _ dns.RecordType, _ string, _ int) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	if f.failAt == f.nextID {
		return "", false, errors.New("provider create failed")
	}
	id := "record-" + string(rune('0'+f.nextID))
	if existing, ok := f.records[id]; ok {
		return id, existing, nil
	}
	f.records[id] = true
	return id, true, nil
}

func (f *fakeDNSWriter) DeleteRecord(_ context.Context, _, _, _, recordID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, recordID)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.records, recordID)
	return nil
}

func (f *fakeDNSWriter) deleted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletes...)
}

type fakeAddresses struct {
	addresses []string
	err       error
}

func (f fakeAddresses) Addresses(context.Context, string, []string) ([]string, error) {
	return append([]string(nil), f.addresses...), f.err
}

type fakeCertificateInspection struct {
	summary, certPath, keyPath string
	inspectErr, pathErr        error
}

func (f fakeCertificateInspection) Inspect(context.Context, string, string, string) (string, error) {
	return f.summary, f.inspectErr
}

func (f fakeCertificateInspection) Paths(context.Context, string, string) (string, string, error) {
	return f.certPath, f.keyPath, f.pathErr
}

type fakeUpstreams struct {
	values []string
	err    error
}

func (f fakeUpstreams) Upstreams(context.Context, string, string) ([]string, error) {
	return append([]string(nil), f.values...), f.err
}

type fakeGatewayApplier struct {
	summary       string
	applyErr      error
	rollbackErr   error
	applied       int
	rolledBack    int
	lastUpstreams []string
}

func (f *fakeGatewayApplier) Apply(_ context.Context, _ publishing.Plan, _ publishing.GatewayRequest, upstreams []string, _, _ string) (string, error) {
	f.applied++
	f.lastUpstreams = append([]string(nil), upstreams...)
	return f.summary, f.applyErr
}

func (f *fakeGatewayApplier) Rollback(context.Context, publishing.Plan, string) error {
	f.rolledBack++
	return f.rollbackErr
}

func productionFixture(t *testing.T, dnsWriter *fakeDNSWriter, certs fakeCertificateInspection, upstreams fakeUpstreams, gateway *fakeGatewayApplier) publishing.LocalBridge {
	t.Helper()
	bridge, err := publishing.NewProductionBridge(publishing.ProductionBridgeConfig{
		Plans: &fakePlanLookup{plan: publishing.Plan{
			ID: "plan-1", CreatedBy: "admin-1", TargetsPersisted: true, TargetRevision: 1,
			GatewayIDs: []string{"gw-1"}, ZoneID: "zone-1", CertificateID: "cert-1",
		}},
		DNS: dnsWriter, Addresses: fakeAddresses{addresses: []string{"203.0.113.10", "2001:db8::10", "203.0.113.10"}},
		Certificates: certs, Upstreams: upstreams, Gateway: gateway,
	})
	if err != nil {
		t.Fatal(err)
	}
	return bridge
}

func TestProductionDNSStageAndCompensation(t *testing.T) {
	dnsWriter := newFakeDNSWriter()
	bridge := productionFixture(t, dnsWriter, fakeCertificateInspection{}, fakeUpstreams{}, &fakeGatewayApplier{})
	request := publishing.DNSPublishRequest{
		PlanID: "plan-1", SubjectID: "admin-1", RequestID: "req-1", Domain: "app.example.com",
		ServiceGroupID: "sg1", ExpectedGroupRevision: 3, GatewayIDs: []string{"gw-1"},
		ZoneID: "zone-1", TargetRevision: 1,
	}
	result, err := bridge.DNS.Publish(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "zone=zone-1") || !strings.Contains(result.Summary, "record-1:1") || !strings.Contains(result.Summary, "record-2:1") {
		t.Fatalf("summary = %q", result.Summary)
	}
	if err := bridge.DNS.Rollback(context.Background(), "plan-1", result); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(dnsWriter.deleted(), ","); got != "record-1,record-2" {
		t.Fatalf("deleted = %s", got)
	}
}

func TestProductionDNSCompensationSkipsExistingRecords(t *testing.T) {
	dnsWriter := newFakeDNSWriter()
	bridge := productionFixture(t, dnsWriter, fakeCertificateInspection{}, fakeUpstreams{}, &fakeGatewayApplier{})
	result := publishing.StageResult{Summary: "dns;zone=zone-1;records=existing-1:0,created-1:1"}
	if err := bridge.DNS.Rollback(context.Background(), "plan-1", result); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(dnsWriter.deleted(), ","); got != "created-1" {
		t.Fatalf("deleted = %s", got)
	}
}

func TestProductionDNSFailureCleansCreatedRecords(t *testing.T) {
	dnsWriter := newFakeDNSWriter()
	dnsWriter.failAt = 2
	bridge := productionFixture(t, dnsWriter, fakeCertificateInspection{}, fakeUpstreams{}, &fakeGatewayApplier{})
	_, err := bridge.DNS.Publish(context.Background(), publishing.DNSPublishRequest{
		PlanID: "plan-1", SubjectID: "admin-1", RequestID: "req-1", Domain: "app.example.com",
		GatewayIDs: []string{"gw-1"}, ZoneID: "zone-1", TargetRevision: 1,
	})
	if err == nil {
		t.Fatal("partial provider failure reported success")
	}
	if got := strings.Join(dnsWriter.deleted(), ","); got != "record-1" {
		t.Fatalf("created record was not cleaned: %s", got)
	}
}

func TestProductionDNSRollbackFailureIsNotSwallowed(t *testing.T) {
	dnsWriter := newFakeDNSWriter()
	dnsWriter.deleteErr = errors.New("provider delete failed")
	bridge := productionFixture(t, dnsWriter, fakeCertificateInspection{}, fakeUpstreams{}, &fakeGatewayApplier{})
	if err := bridge.DNS.Rollback(context.Background(), "plan-1", publishing.StageResult{Summary: "dns;zone=zone-1;records=record-1:1"}); err == nil {
		t.Fatal("rollback deletion failure was swallowed")
	}
}

func TestProductionCertificateStage(t *testing.T) {
	certs := fakeCertificateInspection{summary: "serial=123", certPath: "/cert.pem", keyPath: "/key.pem"}
	bridge := productionFixture(t, newFakeDNSWriter(), certs, fakeUpstreams{}, &fakeGatewayApplier{})
	result, err := bridge.Certificates.Publish(context.Background(), publishing.CertificateRequest{
		PlanID: "plan-1", SubjectID: "admin-1", Domain: "app.example.com", HTTPS: true, CertificateID: "cert-1", TargetRevision: 1,
	})
	if err != nil || !strings.Contains(result.Summary, "certificate=cert-1") || !strings.Contains(result.Summary, "serial=123") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if err := bridge.Certificates.Rollback(context.Background(), "plan-1", result); err != nil {
		t.Fatal(err)
	}
	skipped, err := bridge.Certificates.Publish(context.Background(), publishing.CertificateRequest{PlanID: "plan-1", TargetRevision: 1, HTTPS: false})
	if err != nil || skipped.Summary != "certificate=not_required" {
		t.Fatalf("https=false result = %+v, err = %v", skipped, err)
	}
	if _, err := bridge.Certificates.Publish(context.Background(), publishing.CertificateRequest{PlanID: "plan-1", TargetRevision: 1, HTTPS: true}); !errors.Is(err, publishing.ErrProductionCertificate) {
		t.Fatalf("missing certificate err = %v", err)
	}
}

func TestProductionGatewayStageAndCompensation(t *testing.T) {
	applier := &fakeGatewayApplier{summary: "gateway=gw-1;route=plan-1;site=/etc/nginx/conf.d/site.conf;sha=" + strings.Repeat("a", 64)}
	bridge := productionFixture(t, newFakeDNSWriter(), fakeCertificateInspection{certPath: "/cert.pem", keyPath: "/key.pem"}, fakeUpstreams{values: []string{"10.0.0.2:8080"}}, applier)
	request := publishing.GatewayRequest{
		PlanID: "plan-1", SubjectID: "admin-1", Domain: "app.example.com", ServiceGroupID: "sg1",
		GatewayIDs: []string{"gw-1"}, ZoneID: "zone-1", CertificateID: "cert-1", HTTPS: true, TargetRevision: 1,
	}
	result, err := bridge.Gateway.Publish(context.Background(), request)
	if err != nil || result.Summary == "" || applier.applied != 1 || len(applier.lastUpstreams) != 1 {
		t.Fatalf("result = %+v, err = %v, applier = %+v", result, err, applier)
	}
	if err := bridge.Gateway.Rollback(context.Background(), "plan-1", result); err != nil || applier.rolledBack != 1 {
		t.Fatalf("rollback err = %v, calls = %d", err, applier.rolledBack)
	}
	request.GatewayIDs = []string{"gw-1", "gw-2"}
	if _, err := bridge.Gateway.Publish(context.Background(), request); !errors.Is(err, publishing.ErrProductionTarget) {
		t.Fatalf("multi-gateway err = %v", err)
	}
}

func TestProductionRejectsUnpersistedTargets(t *testing.T) {
	dnsWriter := newFakeDNSWriter()
	bridge := productionFixture(t, dnsWriter, fakeCertificateInspection{}, fakeUpstreams{}, &fakeGatewayApplier{})
	// The fixture uses persisted targets; a legacy plan must not execute.
	bridge, err := publishing.NewProductionBridge(publishing.ProductionBridgeConfig{
		Plans: &fakePlanLookup{plan: publishing.Plan{ID: "plan-1", CreatedBy: "admin-1", TargetRevision: 1}},
		DNS:   dnsWriter, Addresses: fakeAddresses{addresses: []string{"203.0.113.10"}},
		Certificates: fakeCertificateInspection{}, Upstreams: fakeUpstreams{}, Gateway: &fakeGatewayApplier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.DNS.Publish(context.Background(), publishing.DNSPublishRequest{
		PlanID: "plan-1", SubjectID: "admin-1", RequestID: "req-1", Domain: "app.example.com",
		GatewayIDs: []string{"gw-1"}, ZoneID: "zone-1", TargetRevision: 1,
	}); !errors.Is(err, publishing.ErrProductionTarget) {
		t.Fatalf("legacy target err = %v", err)
	}
}

func TestProductionDNSFailurePreservesFailedCleanupIdentity(t *testing.T) {
	dnsWriter := newFakeDNSWriter()
	dnsWriter.failAt = 2
	dnsWriter.deleteErr = errors.New("delete failed")
	bridge := productionFixture(t, dnsWriter, fakeCertificateInspection{}, fakeUpstreams{}, &fakeGatewayApplier{})
	result, err := bridge.DNS.Publish(context.Background(), publishing.DNSPublishRequest{
		PlanID: "plan-1", SubjectID: "admin-1", RequestID: "req-1", Domain: "app.example.com",
		GatewayIDs: []string{"gw-1"}, ZoneID: "zone-1", TargetRevision: 1,
	})
	if err == nil {
		t.Fatal("partial provider failure reported success")
	}
	if !strings.Contains(result.Summary, "zone=zone-1") || !strings.Contains(result.Summary, "record-1:1") {
		t.Fatalf("failed cleanup identity was swallowed: %q", result.Summary)
	}
	dnsWriter.mu.Lock()
	dnsWriter.deleteErr = nil
	dnsWriter.mu.Unlock()
	if err := bridge.DNS.Rollback(context.Background(), "plan-1", result); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(dnsWriter.deleted(), ","); !strings.Contains(got, "record-1") {
		t.Fatalf("rollback did not retry the preserved record: %s", got)
	}
}

func TestProductionGatewayFailureKeepsPartialCompensationSummary(t *testing.T) {
	applier := &fakeGatewayApplier{
		summary:  "gateway=partial;route=plan-1;site=/etc/nginx/conf.d/site.conf",
		applyErr: errors.New("agent outcome unknown"),
	}
	bridge := productionFixture(t, newFakeDNSWriter(), fakeCertificateInspection{certPath: "/cert.pem", keyPath: "/key.pem"}, fakeUpstreams{values: []string{"10.0.0.2:8080"}}, applier)
	result, err := bridge.Gateway.Publish(context.Background(), publishing.GatewayRequest{
		PlanID: "plan-1", SubjectID: "admin-1", Domain: "app.example.com", ServiceGroupID: "sg1",
		GatewayIDs: []string{"gw-1"}, ZoneID: "zone-1", CertificateID: "cert-1", HTTPS: true, TargetRevision: 1,
	})
	if err == nil || result.Summary == "" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if err := bridge.Gateway.Rollback(context.Background(), "plan-1", result); err != nil || applier.rolledBack != 1 {
		t.Fatalf("partial summary was not usable for rollback: %v/%d", err, applier.rolledBack)
	}
}

func TestRunFailurePersistsPartialStageSummary(t *testing.T) {
	f := newPublicationFixture(t)
	f.createPlan(t, "plan-1", "shop.example.com", "")
	f.ports.cert.set(publishing.StageResult{Summary: "certificate=cert-1;state=unknown"}, errors.New("provider exploded"))
	if _, err := f.service.Run(context.Background(), "admin", "req-run", "plan-1"); !errors.Is(err, publishing.ErrStageFailed) {
		t.Fatalf("Run err = %v", err)
	}
	_, stages := f.plan(t, "plan-1")
	cert := stageByKind(t, stages, publishing.StageCertificate)
	if cert.Status != publishing.StageFailed || cert.ResultSummary != "certificate=cert-1;state=unknown" {
		t.Fatalf("failed stage lost compensation identity: %+v", cert)
	}
}

func TestProductionBridgeRequiresAllPorts(t *testing.T) {
	if _, err := publishing.NewProductionBridge(publishing.ProductionBridgeConfig{}); !errors.Is(err, publishing.ErrProductionUnavailable) {
		t.Fatalf("empty bridge err = %v", err)
	}
}
