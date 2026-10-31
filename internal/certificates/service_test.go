package certificates_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
)

type authorizerFunc func(context.Context, auth.Check) error

func (f authorizerFunc) Authorize(ctx context.Context, check auth.Check) error {
	return f(ctx, check)
}

type fakeIssuer struct {
	issueCalls  int
	renewCalls  int
	revokeCalls int
	issueErr    error
	renewErr    error
	revokeErr   error
}

func (f *fakeIssuer) Issue(_ context.Context, request certificates.IssueRequest) (certificates.IssuedCertificate, error) {
	f.issueCalls++
	if f.issueErr != nil {
		return certificates.IssuedCertificate{}, f.issueErr
	}
	return certificates.IssuedCertificate{
		Serial: fmt.Sprintf("serial-issue-%d", f.issueCalls), IssuerRef: "issuer-1",
		KeyRef: request.KeyRef, NotBefore: request.NotBefore, NotAfter: request.NotAfter,
	}, nil
}

func (f *fakeIssuer) Renew(_ context.Context, request certificates.RenewRequest) (certificates.IssuedCertificate, error) {
	f.renewCalls++
	if f.renewErr != nil {
		return certificates.IssuedCertificate{}, f.renewErr
	}
	return certificates.IssuedCertificate{
		Serial: fmt.Sprintf("serial-renew-%d", f.renewCalls), IssuerRef: "issuer-1",
		KeyRef: request.KeyRef, NotBefore: request.NotBefore, NotAfter: request.NotAfter,
	}, nil
}

func (f *fakeIssuer) Revoke(context.Context, certificates.RevokeRequest) error {
	f.revokeCalls++
	return f.revokeErr
}

type fakeTasks struct {
	calls  int
	taskID string
	err    error
	last   certificates.DistributionTask
}

func (f *fakeTasks) Submit(_ context.Context, task certificates.DistributionTask) (string, error) {
	f.calls++
	f.last = task
	if f.err != nil {
		return "", f.err
	}
	if f.taskID != "" {
		return f.taskID, nil
	}
	return fmt.Sprintf("task-%d", f.calls), nil
}

type fakeRevisions struct {
	revision uint64
	calls    int
	err      error
}

func (f *fakeRevisions) Revision(context.Context, string) (uint64, error) {
	f.calls++
	return f.revision, f.err
}

type certFixture struct {
	db        *storage.DB
	repo      *certificates.Repo
	audit     *audit.SQLRecorder
	service   *certificates.Service
	issuer    *fakeIssuer
	tasks     *fakeTasks
	revisions *fakeRevisions
	path      string
}

func newCertFixture(t *testing.T, authorizer auth.Authorizer, actions []string) certFixture {
	t.Helper()
	return newCertFixtureAt(t, filepath.Join(t.TempDir(), "fleet.db"), authorizer, actions)
}

func newCertFixtureAt(t *testing.T, path string, authorizer auth.Authorizer, actions []string) certFixture {
	t.Helper()
	db := testkit.NewDBAt(t, path, certificates.Migrations(), audit.Migrations())
	repo, err := certificates.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	if actions == nil {
		actions = certificates.ServiceActions()
	}
	limits := audit.DefaultLimits()
	limits.RequireSource = true
	validator, err := audit.NewValidator(actions, limits)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSQLRecorder(db, validator, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if authorizer == nil {
		authorizer = authorizerFunc(func(context.Context, auth.Check) error { return nil })
	}
	issuer := &fakeIssuer{}
	tasks := &fakeTasks{}
	revisions := &fakeRevisions{revision: 4}
	service, err := certificates.NewService(certificates.Options{
		DB: db, Repo: repo, Audit: recorder, Authorizer: authorizer,
		Issuer: issuer, Tasks: tasks, GroupRevisions: revisions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return certFixture{
		db: db, repo: repo, audit: recorder, service: service,
		issuer: issuer, tasks: tasks, revisions: revisions, path: path,
	}
}

func certActionsExcept(exclude ...string) []string {
	skip := make(map[string]struct{}, len(exclude))
	for _, action := range exclude {
		skip[action] = struct{}{}
	}
	out := make([]string, 0)
	for _, action := range certificates.ServiceActions() {
		if _, omitted := skip[action]; !omitted {
			out = append(out, action)
		}
	}
	return out
}

func validValidity() certificates.Validity {
	now := time.Now().UTC()
	return certificates.Validity{NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour)}
}

func (f certFixture) createInput(id string, nodes ...string) certificates.CreateInternalDistributionInput {
	return certificates.CreateInternalDistributionInput{
		ID: id, CommonName: id + ".internal", ServiceGroupID: "sg-1",
		NodeIDs: nodes, ExpectedGroupRevision: f.revisions.revision,
		KeyRef:   certificates.KeyRef("secret-store/key-" + id),
		Validity: validValidity(), Confirm: true,
	}
}

func (f certFixture) createCert(t *testing.T, id string, nodes ...string) certificates.DistributionResult {
	t.Helper()
	result, err := f.service.CreateInternalDistribution(context.Background(), "admin", "req-"+id, f.createInput(id, nodes...))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func countCertRows(t *testing.T, db *storage.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func queryKeyRef(t *testing.T, db *storage.DB, id string) string {
	t.Helper()
	var ref string
	if err := db.QueryRow(context.Background(), "SELECT key_ref FROM certificates WHERE id = ?", id).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

func querySerial(t *testing.T, db *storage.DB, id string) string {
	t.Helper()
	var serial string
	if err := db.QueryRow(context.Background(), "SELECT serial FROM certificates WHERE id = ?", id).Scan(&serial); err != nil {
		t.Fatal(err)
	}
	return serial
}

func TestCreateInternalDistributionPersistsAcrossRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fleet.db")
	f := newCertFixtureAt(t, path, nil, nil)
	result := f.createCert(t, "cert-1", "n2", "n1")
	if result.Certificate.Kind != certificates.KindInternal || result.Certificate.Status != certificates.StatusActive {
		t.Fatalf("certificate = %+v", result.Certificate)
	}
	if result.TaskID != "task-1" || len(result.Distributions) != 2 {
		t.Fatalf("result = %+v", result)
	}
	for _, distribution := range result.Distributions {
		if distribution.Status != certificates.DistributionPending || distribution.TaskID != "task-1" {
			t.Fatalf("distribution = %+v", distribution)
		}
	}
	if f.tasks.last.SubjectID != "admin" || f.tasks.last.CertificateID != "cert-1" ||
		f.tasks.last.ServiceGroupID != "sg-1" || len(f.tasks.last.NodeIDs) != 2 {
		t.Fatalf("task request = %+v", f.tasks.last)
	}
	if got := queryKeyRef(t, f.db, "cert-1"); got != "secret-store/key-cert-1" {
		t.Fatalf("stored key ref = %q", got)
	}
	page, err := f.audit.Query(ctx, audit.Query{Action: certificates.ActionDistributionCreate})
	if err != nil || page.Total != 1 || page.Items[0].TaskID != "task-1" || len(page.Items[0].NodeIDs) != 2 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
	if _, err := f.service.CreateInternalDistribution(ctx, "admin", "req-dup", f.createInput("cert-1", "n1")); !errors.Is(err, certificates.ErrCertificateExists) {
		t.Fatalf("duplicate certificate err = %v", err)
	}
	if f.issuer.issueCalls != 1 || f.tasks.calls != 1 {
		t.Fatalf("duplicate triggered side effects: issue=%d task=%d", f.issuer.issueCalls, f.tasks.calls)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newCertFixtureAt(t, path, nil, nil)
	detail, err := reopened.service.Get(ctx, "admin", "cert-1")
	if err != nil || len(detail.Distributions) != 2 || detail.Certificate.ID != "cert-1" {
		t.Fatalf("detail after restart = %+v, %v", detail, err)
	}
	list, err := reopened.service.List(ctx, "admin")
	if err != nil || len(list) != 1 || list[0].ID != "cert-1" {
		t.Fatalf("list after restart = %+v, %v", list, err)
	}
	if got := queryKeyRef(t, reopened.db, "cert-1"); got != "secret-store/key-cert-1" {
		t.Fatalf("key ref after restart = %q", got)
	}
}

func TestRotateChangesKeyReferenceWithoutLeaking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newCertFixture(t, nil, nil)
	f.createCert(t, "cert-1", "n1")
	oldRef := queryKeyRef(t, f.db, "cert-1")
	view, err := f.service.Rotate(ctx, "admin", "req-rotate", "cert-1", certificates.RotateInput{
		NewKeyRef: certificates.KeyRef("secret-store/key-cert-1-v2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	newRef := queryKeyRef(t, f.db, "cert-1")
	if newRef != "secret-store/key-cert-1-v2" || newRef == oldRef {
		t.Fatalf("key ref did not rotate: old=%q new=%q", oldRef, newRef)
	}
	if f.issuer.renewCalls != 1 {
		t.Fatalf("issuer renew calls = %d", f.issuer.renewCalls)
	}
	detail, err := f.service.Get(ctx, "admin", "cert-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{view, detail, detail.Certificate, detail.Distributions} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "secret-store/key-") || strings.Contains(string(encoded), "PRIVATE KEY") {
			t.Fatalf("view leaked key material: %s", encoded)
		}
	}
	page, err := f.audit.Query(ctx, audit.Query{Action: certificates.ActionCertificateRotate})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
	encoded, err := json.Marshal(page.Items)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-store/key-") || strings.Contains(string(encoded), "PRIVATE KEY") {
		t.Fatalf("audit leaked key material: %s", encoded)
	}
	if _, err := f.service.Rotate(ctx, "admin", "req-rotate-bad", "cert-1", certificates.RotateInput{
		NewKeyRef: "-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----",
	}); !errors.Is(err, certificates.ErrKeyMaterialRejected) {
		t.Fatalf("pem rotation err = %v", err)
	}
	if got := queryKeyRef(t, f.db, "cert-1"); got != newRef {
		t.Fatalf("rejected rotation changed key ref to %q", got)
	}
}

func TestRenewUpdatesMetadataWithoutChangingKeyReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newCertFixture(t, nil, nil)
	f.createCert(t, "cert-1", "n1")
	keyRef := queryKeyRef(t, f.db, "cert-1")
	serial := querySerial(t, f.db, "cert-1")
	validity := validValidity()
	view, err := f.service.Renew(ctx, "admin", "req-renew", "cert-1", certificates.RenewInput{Validity: validity})
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != certificates.StatusActive || view.Serial == serial {
		t.Fatalf("renewed view = %+v", view)
	}
	if got := queryKeyRef(t, f.db, "cert-1"); got != keyRef {
		t.Fatalf("renew changed key ref: %q", got)
	}
	if f.issuer.renewCalls != 1 {
		t.Fatalf("issuer renew calls = %d", f.issuer.renewCalls)
	}
	page, err := f.audit.Query(ctx, audit.Query{Action: certificates.ActionCertificateRenew})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
}

func TestRevokeUpdatesCertificateAndDistributions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newCertFixture(t, nil, nil)
	f.createCert(t, "cert-1", "n1", "n2")
	view, err := f.service.Revoke(ctx, "admin", "req-revoke", "cert-1")
	if err != nil || view.Status != certificates.StatusRevoked {
		t.Fatalf("revoke = %+v, %v", view, err)
	}
	if f.issuer.revokeCalls != 1 {
		t.Fatalf("issuer revoke calls = %d", f.issuer.revokeCalls)
	}
	detail, err := f.service.Get(ctx, "admin", "cert-1")
	if err != nil || detail.Certificate.Status != certificates.StatusRevoked {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	for _, distribution := range detail.Distributions {
		if distribution.Status != certificates.DistributionRevoked {
			t.Fatalf("distribution = %+v", distribution)
		}
	}
	if _, err := f.service.Revoke(ctx, "admin", "req-revoke-2", "cert-1"); !errors.Is(err, certificates.ErrCertificateRevoked) {
		t.Fatalf("second revoke err = %v", err)
	}
	page, err := f.audit.Query(ctx, audit.Query{Action: certificates.ActionCertificateRevoke, Result: "success"})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
}

func TestExpectedRevisionMismatchFailsBeforeSideEffects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newCertFixture(t, nil, nil)
	f.revisions.revision = 5
	input := f.createInput("cert-1", "n1")
	input.ExpectedGroupRevision = 4
	if _, err := f.service.CreateInternalDistribution(ctx, "admin", "req-1", input); !errors.Is(err, certificates.ErrRevisionConflict) {
		t.Fatalf("err = %v", err)
	}
	if countCertRows(t, f.db, "certificates") != 0 || countCertRows(t, f.db, "certificate_distributions") != 0 {
		t.Fatal("revision mismatch persisted rows")
	}
	if f.issuer.issueCalls != 0 || f.tasks.calls != 0 {
		t.Fatalf("side effects: issue=%d task=%d", f.issuer.issueCalls, f.tasks.calls)
	}
	input.ExpectedGroupRevision = 0
	if _, err := f.service.CreateInternalDistribution(ctx, "admin", "req-2", input); !errors.Is(err, certificates.ErrInvalidRevision) {
		t.Fatalf("zero revision err = %v", err)
	}
}

func TestDeniedDistributionIsAuditedWithoutMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newCertFixture(t, authorizerFunc(func(context.Context, auth.Check) error {
		return auth.ErrDenied
	}), nil)
	if _, err := f.service.CreateInternalDistribution(ctx, "admin", "req-deny", f.createInput("cert-1", "n1")); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if countCertRows(t, f.db, "certificates") != 0 || countCertRows(t, f.db, "certificate_distributions") != 0 {
		t.Fatal("denied distribution persisted rows")
	}
	page, err := f.audit.Query(ctx, audit.Query{Action: certificates.ActionDistributionCreate, Result: "denied"})
	if err != nil || page.Total != 1 {
		t.Fatalf("denial audit = %+v, %v", page, err)
	}
	if _, err := f.service.List(ctx, "admin"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("denied list err = %v", err)
	}
}

func TestTaskSubmitFailureCreatesNoRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newCertFixture(t, nil, nil)
	f.tasks.err = errors.New("task backend failed for secret-store/key-cert-1")
	if _, err := f.service.CreateInternalDistribution(ctx, "admin", "req-1", f.createInput("cert-1", "n1")); !errors.Is(err, certificates.ErrTaskSubmitFailed) {
		t.Fatalf("err = %v", err)
	} else if strings.Contains(err.Error(), "secret-store/key-") {
		t.Fatalf("task error leaked key reference: %v", err)
	}
	if countCertRows(t, f.db, "certificates") != 0 || countCertRows(t, f.db, "certificate_distributions") != 0 {
		t.Fatal("task failure persisted certificate rows")
	}
	page, err := f.audit.Query(ctx, audit.Query{Action: certificates.ActionDistributionCreate})
	if err != nil || page.Total != 0 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
	f.tasks.err = nil
	result := f.createCert(t, "cert-1", "n1")
	if result.TaskID == "" || len(result.Distributions) != 1 {
		t.Fatalf("result after retry = %+v", result)
	}
}

func TestAuditFailureRollsBackCertificateAndDistribution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newCertFixture(t, nil, certActionsExcept(certificates.ActionDistributionCreate))
	if _, err := f.service.CreateInternalDistribution(ctx, "admin", "req-1", f.createInput("cert-1", "n1")); err == nil {
		t.Fatal("distribution succeeded without an audit record")
	}
	if countCertRows(t, f.db, "certificates") != 0 || countCertRows(t, f.db, "certificate_distributions") != 0 {
		t.Fatal("audit failure did not roll back certificate and distributions")
	}
}

func TestNoKeyMaterialInViewsOrAudit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newCertFixture(t, nil, nil)
	f.createCert(t, "cert-1", "n1")
	detail, err := f.service.Get(ctx, "admin", "cert-1")
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.audit.Query(ctx, audit.Query{})
	if err != nil || page.Total == 0 {
		t.Fatalf("audit = %+v, %v", page, err)
	}
	for _, value := range []any{detail, detail.Certificate, detail.Distributions, page.Items} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "PRIVATE KEY") || strings.Contains(string(encoded), "secret-store/key-") {
			t.Fatalf("key material leaked: %s", encoded)
		}
	}
	if _, err := certificates.ParseKeyRef("-----BEGIN RSA PRIVATE KEY-----"); !errors.Is(err, certificates.ErrKeyMaterialRejected) {
		t.Fatalf("rsa pem key ref err = %v", err)
	}
	input := f.createInput("cert-2", "n1")
	input.CommonName = "BEGIN PRIVATE KEY"
	if _, err := f.service.CreateInternalDistribution(ctx, "admin", "req-2", input); !errors.Is(err, certificates.ErrKeyMaterialRejected) {
		t.Fatalf("pem common name err = %v", err)
	}
	if countCertRows(t, f.db, "certificates") != 1 {
		t.Fatal("rejected key material created a certificate")
	}
}

func TestNilPortsFailClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	allow := authorizerFunc(func(context.Context, auth.Check) error { return nil })
	f := newCertFixture(t, nil, nil)
	f.createCert(t, "cert-existing", "n1")

	noIssuer, err := certificates.NewService(certificates.Options{
		DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: allow,
		Tasks: f.tasks, GroupRevisions: f.revisions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noIssuer.CreateInternalDistribution(ctx, "admin", "req-1", f.createInput("cert-1", "n1")); !errors.Is(err, certificates.ErrIssuerUnavailable) {
		t.Fatalf("nil issuer err = %v", err)
	}
	if _, err := noIssuer.Revoke(ctx, "admin", "req-2", "cert-existing"); !errors.Is(err, certificates.ErrIssuerUnavailable) {
		t.Fatalf("nil issuer revoke err = %v", err)
	}

	noTasks, err := certificates.NewService(certificates.Options{
		DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: allow,
		Issuer: f.issuer, GroupRevisions: f.revisions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noTasks.CreateInternalDistribution(ctx, "admin", "req-3", f.createInput("cert-2", "n1")); !errors.Is(err, certificates.ErrTaskSubmitterUnavailable) {
		t.Fatalf("nil task submitter err = %v", err)
	}

	noRevisions, err := certificates.NewService(certificates.Options{
		DB: f.db, Repo: f.repo, Audit: f.audit, Authorizer: allow,
		Issuer: f.issuer, Tasks: f.tasks,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noRevisions.CreateInternalDistribution(ctx, "admin", "req-4", f.createInput("cert-3", "n1")); !errors.Is(err, certificates.ErrGroupRevisionsUnavailable) {
		t.Fatalf("nil revisions err = %v", err)
	}
	if countCertRows(t, f.db, "certificates") != 1 {
		t.Fatal("failed-closed operations persisted certificates")
	}
}

func TestReadsAreRestrictedToDistributionNodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	authorizer := authorizerFunc(func(_ context.Context, check auth.Check) error {
		if check.Action != certificates.ActionCertificateRead {
			return nil
		}
		for _, nodeID := range check.NodeIDs {
			if nodeID != "n1" {
				return auth.ErrDenied
			}
		}
		return nil
	})
	f := newCertFixture(t, authorizer, nil)
	f.createCert(t, "cert-wide", "n1", "n2")
	f.createCert(t, "cert-narrow", "n1")
	list, err := f.service.List(ctx, "admin")
	if err != nil || len(list) != 1 || list[0].ID != "cert-narrow" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := f.service.Get(ctx, "admin", "cert-wide"); !errors.Is(err, auth.ErrDenied) {
		t.Fatalf("wide get err = %v", err)
	}
	if detail, err := f.service.Get(ctx, "admin", "cert-narrow"); err != nil || detail.Certificate.ID != "cert-narrow" {
		t.Fatalf("narrow get = %+v, %v", detail, err)
	}
}
