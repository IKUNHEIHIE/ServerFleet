package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/quota"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/storage/testkit"
	"serverfleet.local/serverfleet/internal/tasks"
)

// waveAuthorizer grants admin and limited (read-only node n1) and denies every
// other subject. It is a test boundary, not a production authorizer.
type waveAuthorizer struct{}

func (waveAuthorizer) Authorize(_ context.Context, check auth.Check) error {
	switch check.SubjectID {
	case "admin":
		return nil
	case "limited":
		for _, nodeID := range check.NodeIDs {
			if nodeID != "n1" {
				return auth.ErrDenied
			}
		}
		return nil
	default:
		return auth.ErrDenied
	}
}

type waveNotifier struct {
	mu     sync.Mutex
	alerts int
}

func (n *waveNotifier) SendAlert(context.Context, notifications.Alert) (notifications.Delivery, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.alerts++
	return notifications.Delivery{ID: int64(n.alerts), Status: notifications.DeliveryProviderAccepted}, nil
}

type waveProber struct{}

func (waveProber) HTTP(context.Context, monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	code := 200
	return monitoring.ProbeResult{Healthy: true, StatusCode: &code}, nil
}
func (waveProber) TCP(context.Context, monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return monitoring.ProbeResult{Healthy: true}, nil
}
func (waveProber) TLS(context.Context, monitoring.TargetSpec) (monitoring.ProbeResult, error) {
	return monitoring.ProbeResult{Healthy: true}, nil
}

type waveIssuer struct {
	mu          sync.Mutex
	issued      int
	revoked     int
	revokeError error
}

func (i *waveIssuer) Issue(_ context.Context, request certificates.IssueRequest) (certificates.IssuedCertificate, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.issued++
	return certificates.IssuedCertificate{
		Serial: fmt.Sprintf("serial-%d", i.issued), IssuerRef: "issuer-wave3",
		KeyRef: request.KeyRef, NotBefore: request.NotBefore, NotAfter: request.NotAfter,
	}, nil
}

func (i *waveIssuer) Renew(_ context.Context, request certificates.RenewRequest) (certificates.IssuedCertificate, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.issued++
	return certificates.IssuedCertificate{
		Serial: fmt.Sprintf("serial-%d", i.issued), IssuerRef: "issuer-wave3",
		KeyRef: request.KeyRef, NotBefore: request.NotBefore, NotAfter: request.NotAfter,
	}, nil
}

func (i *waveIssuer) Revoke(context.Context, certificates.RevokeRequest) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.revoked++
	return i.revokeError
}

func (i *waveIssuer) setRevokeError(err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.revokeError = err
}

type waveCertTasks struct{ next atomic.Int64 }

func (t *waveCertTasks) Submit(context.Context, certificates.DistributionTask) (string, error) {
	return fmt.Sprintf("task-cert-%d", t.next.Add(1)), nil
}

type waveRevisions struct{ revision uint64 }

func (r *waveRevisions) Revision(context.Context, string) (uint64, error) { return r.revision, nil }

type waveMembers struct{ members []string }

func (m *waveMembers) Members(context.Context, string) ([]string, error) {
	return append([]string(nil), m.members...), nil
}

type waveGroupLookup struct{ ref publishing.GroupRef }

func (g *waveGroupLookup) LookupGroup(_ context.Context, id string) (publishing.GroupRef, bool, error) {
	if id != g.ref.ID {
		return publishing.GroupRef{}, false, nil
	}
	return g.ref, true, nil
}

type waveStage struct {
	mu        sync.Mutex
	publishes int
	rollbacks int
	fail      error
}

func (s *waveStage) publish() (publishing.StageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishes++
	if s.fail != nil {
		return publishing.StageResult{}, s.fail
	}
	return publishing.StageResult{Summary: "ok"}, nil
}

func (s *waveStage) rollback() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollbacks++
	return nil
}

type waveDNS struct{ *waveStage }

func (p *waveDNS) Publish(context.Context, publishing.DNSPublishRequest) (publishing.StageResult, error) {
	return p.publish()
}
func (p *waveDNS) Rollback(context.Context, string, publishing.StageResult) error {
	return p.rollback()
}

type waveCertificate struct{ *waveStage }

func (p *waveCertificate) Publish(context.Context, publishing.CertificateRequest) (publishing.StageResult, error) {
	return p.publish()
}
func (p *waveCertificate) Rollback(context.Context, string, publishing.StageResult) error {
	return p.rollback()
}

type waveGateway struct{ *waveStage }

func (p *waveGateway) Publish(context.Context, publishing.GatewayRequest) (publishing.StageResult, error) {
	return p.publish()
}
func (p *waveGateway) Rollback(context.Context, string, publishing.StageResult) error {
	return p.rollback()
}

type waveGatewayNodes struct{ online map[string]bool }

func (n *waveGatewayNodes) LookupNode(_ context.Context, nodeID string) (gateway.NodeRef, bool, error) {
	online, ok := n.online[nodeID]
	if !ok {
		return gateway.NodeRef{}, false, nil
	}
	return gateway.NodeRef{ID: nodeID, Online: online}, true, nil
}

type waveGatewayGroups struct{}

func (waveGatewayGroups) LookupServiceGroup(context.Context, string) (gateway.ServiceGroupRef, bool, error) {
	return gateway.ServiceGroupRef{}, false, nil
}

type waveGatewayTasks struct{ next atomic.Int64 }

func (t *waveGatewayTasks) Submit(context.Context, gateway.TaskRequest) (string, error) {
	return fmt.Sprintf("task-gw-%d", t.next.Add(1)), nil
}

type waveReadGate struct{}

func (waveReadGate) AuthorizeTaskRead(context.Context, string, string) error { return nil }

type waveRiskSource struct{ service *quota.Service }

func (r waveRiskSource) ListTrafficRisks(ctx context.Context, subjectID string) ([]monitoring.TrafficRisk, error) {
	risks, err := r.service.ListTrafficRisks(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	out := make([]monitoring.TrafficRisk, 0, len(risks))
	for _, risk := range risks {
		out = append(out, monitoring.TrafficRisk{
			NodeID: risk.NodeID, Kind: risk.Kind, Severity: risk.Severity, Estimated: risk.Estimated,
		})
	}
	return out, nil
}

type waveEnv struct {
	db         *storage.DB
	recorder   *audit.SQLRecorder
	nodeRepo   *nodes.Repo
	groupRepo  *nodes.GroupRepo
	nodeSvc    *nodes.Service
	taskSvc    *tasks.Service
	taskRepo   *tasks.Repo
	monitorSvc *monitoring.Service
	quotaSvc   *quota.Service
	certSvc    *certificates.Service
	pubSvc     *publishing.Service
	gwSvc      *gateway.Service
	issuer     *waveIssuer
	server     *httptest.Server
}

func waveSubject(r *http.Request) (string, string, bool) {
	subject := r.Header.Get("X-Test-Subject")
	if subject == "" {
		subject = "admin"
	}
	if subject == "anonymous" {
		return "", "", false
	}
	return subject, "req-wave3", true
}

func newWaveEnv(t *testing.T) *waveEnv {
	t.Helper()
	ctx := context.Background()
	db := testkit.NewDBAt(t, filepath.Join(t.TempDir(), "wave3.db"), v09Migrations()...)

	actions := append(append(append(append(append(append(
		nodes.ServiceActions(), tasks.ServiceActions()...), monitoring.ServiceActions()...),
		quota.ServiceActions()...), certificates.ServiceActions()...), publishing.ServiceActions()...),
		gateway.ServiceActions()...)
	actions = append(actions, "host.patch")
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
	nodeRepo, err := nodes.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	groupRepo, err := nodes.NewGroupRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	policy := &auth.NodePolicy{
		KnownNodes: map[string]struct{}{"n1": {}, "n2": {}},
		ReadGrants: map[string]map[string]struct{}{
			"admin":   {"n1": {}, "n2": {}},
			"limited": {"n1": {}},
		},
	}
	readPolicy := func(context.Context) (*auth.NodePolicy, error) { return policy, nil }
	nodeSvc, err := nodes.NewService(nodes.Options{
		DB: db, Nodes: nodeRepo, Groups: groupRepo, Audit: recorder,
		Authorizer: waveAuthorizer{}, ReadPolicy: readPolicy,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"n1", "n2"} {
		if _, err := nodeRepo.Create(ctx, db, nodes.NodeCreateInput{ID: id, Name: "Node " + id, Status: nodes.StatusOffline}); err != nil {
			t.Fatal(err)
		}
	}
	if _, applied, err := nodeRepo.ApplyAgentUpdate(ctx, db, nodes.AgentUpdate{
		NodeID: "n1", Status: nodes.StatusOnline, Sequence: 1, ObservedAt: time.Now().UTC(),
	}); err != nil || !applied {
		t.Fatalf("seed agent update: %v applied=%t", err, applied)
	}

	taskRepo, err := tasks.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	taskSvc, err := tasks.NewService(tasks.Options{
		DB: db, Repo: taskRepo, Audit: recorder, Authorizer: waveAuthorizer{},
		Dispatcher: &recordingDispatcher{}, Nodes: onlineLookup{},
	})
	if err != nil {
		t.Fatal(err)
	}

	monitorRepo, err := monitoring.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	monitorSvc, err := monitoring.NewService(monitoring.Options{
		DB: db, Repo: monitorRepo, Audit: recorder, Authorizer: waveAuthorizer{},
		Prober: waveProber{}, Notifier: &waveNotifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	quotaRepo, err := quota.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	quotaSvc, err := quota.NewService(quota.Options{
		DB: db, Repo: quotaRepo, Nodes: nodeRepo, Audit: recorder,
		Authorizer: waveAuthorizer{}, Notifier: &waveNotifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	certRepo, err := certificates.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	issuer := &waveIssuer{}
	certSvc, err := certificates.NewService(certificates.Options{
		DB: db, Repo: certRepo, Audit: recorder, Authorizer: waveAuthorizer{},
		Issuer: issuer, Tasks: &waveCertTasks{}, GroupRevisions: &waveRevisions{revision: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	pubRepo, err := publishing.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	pubSvc, err := publishing.NewService(publishing.Options{
		DB: db, Repo: pubRepo,
		Groups: &waveGroupLookup{ref: publishing.GroupRef{ID: "sg1", Revision: 1, NodeIDs: []string{"n1", "n2"}}},
		DNS:    &waveDNS{&waveStage{}}, Certificates: &waveCertificate{&waveStage{}}, Gateway: &waveGateway{&waveStage{}},
		Audit: recorder, Authorizer: waveAuthorizer{}, Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	gwRepo, err := gateway.NewRepo(db)
	if err != nil {
		t.Fatal(err)
	}
	gwSvc, err := gateway.NewService(gateway.Options{
		DB: db, Repo: gwRepo, Nodes: &waveGatewayNodes{online: map[string]bool{"n1": true}},
		ServiceGroups: waveGatewayGroups{}, Tasks: &waveGatewayTasks{},
		Audit: recorder, Authorizer: waveAuthorizer{},
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	if err := nodes.RegisterHTTP(mux, nodeSvc, func(r *http.Request) (nodes.Principal, error) {
		subject, requestID, ok := waveSubject(r)
		if !ok {
			return nodes.Principal{}, errors.New("no session")
		}
		return nodes.Principal{SubjectID: subject, RequestID: requestID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Register(mux, taskSvc, func(r *http.Request) (tasks.Principal, error) {
		subject, requestID, ok := waveSubject(r)
		if !ok {
			return tasks.Principal{}, errors.New("no session")
		}
		return tasks.Principal{SubjectID: subject, RequestID: requestID}, nil
	}, nil, func(string) (tasks.OperationClass, error) { return tasks.OperationClassStandard, nil }, waveReadGate{}); err != nil {
		t.Fatal(err)
	}
	if err := monitoring.RegisterHTTP(mux, monitorSvc, waveRiskSource{service: quotaSvc}, func(r *http.Request) (monitoring.Principal, error) {
		subject, requestID, ok := waveSubject(r)
		if !ok {
			return monitoring.Principal{}, errors.New("no session")
		}
		return monitoring.Principal{SubjectID: subject, RequestID: requestID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := quota.RegisterHTTP(mux, quotaSvc, func(r *http.Request) (quota.Principal, error) {
		subject, requestID, ok := waveSubject(r)
		if !ok {
			return quota.Principal{}, errors.New("no session")
		}
		return quota.Principal{SubjectID: subject, RequestID: requestID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := certificates.RegisterHTTP(mux, certSvc, &waveMembers{members: []string{"n2", "n1"}}, func(r *http.Request) (certificates.Principal, error) {
		subject, requestID, ok := waveSubject(r)
		if !ok {
			return certificates.Principal{}, errors.New("no session")
		}
		return certificates.Principal{SubjectID: subject, RequestID: requestID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := publishing.RegisterHTTP(mux, pubSvc, func(r *http.Request) (publishing.Principal, error) {
		subject, requestID, ok := waveSubject(r)
		if !ok {
			return publishing.Principal{}, errors.New("no session")
		}
		return publishing.Principal{SubjectID: subject, RequestID: requestID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterHTTP(mux, gwSvc, func(r *http.Request) (gateway.Principal, error) {
		subject, requestID, ok := waveSubject(r)
		if !ok {
			return gateway.Principal{}, errors.New("no session")
		}
		return gateway.Principal{SubjectID: subject, RequestID: requestID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &waveEnv{
		db: db, recorder: recorder, nodeRepo: nodeRepo, groupRepo: groupRepo,
		nodeSvc: nodeSvc, taskSvc: taskSvc, taskRepo: taskRepo, monitorSvc: monitorSvc,
		quotaSvc: quotaSvc, certSvc: certSvc, pubSvc: pubSvc, gwSvc: gwSvc,
		issuer: issuer, server: server,
	}
}

type waveResult struct {
	Status int
	Body   []byte
}

func waveDo(t *testing.T, url, method string, body any, subject string) waveResult {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if subject != "" {
		request.Header.Set("X-Test-Subject", subject)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	return waveResult{Status: response.StatusCode, Body: buffer.Bytes()}
}

func waveExpect(t *testing.T, result waveResult, status int) {
	t.Helper()
	if result.Status != status {
		t.Fatalf("status = %d want %d body=%s", result.Status, status, result.Body)
	}
}

func waveDecode(t *testing.T, result waveResult, destination any) {
	t.Helper()
	if err := json.Unmarshal(result.Body, destination); err != nil {
		t.Fatalf("decode %s: %v", result.Body, err)
	}
}

func TestWave3ServiceHTTPEndToEnd(t *testing.T) {
	env := newWaveEnv(t)
	base := env.server.URL

	t.Run("monitoring", func(t *testing.T) {
		created := waveDo(t, base+"/api/v1/monitoring/availability-monitors", http.MethodPost, map[string]any{
			"name": "Public API", "type": "http", "target": "https://example.com/health",
			"intervalSeconds": 60, "timeoutSeconds": 5,
		}, "admin")
		waveExpect(t, created, http.StatusCreated)
		var monitor struct {
			ID string `json:"id"`
		}
		waveDecode(t, created, &monitor)
		paused := waveDo(t, base+"/api/v1/monitoring/availability-monitors/"+monitor.ID, http.MethodPatch, map[string]any{
			"state": "paused",
		}, "admin")
		waveExpect(t, paused, http.StatusOK)
		var pausedBody struct {
			State   string `json:"state"`
			Enabled bool   `json:"enabled"`
		}
		waveDecode(t, paused, &pausedBody)
		if pausedBody.State != "paused" || pausedBody.Enabled {
			t.Fatalf("paused = %+v", pausedBody)
		}
		list := waveDo(t, base+"/api/v1/monitoring/availability-monitors", http.MethodGet, nil, "admin")
		waveExpect(t, list, http.StatusOK)
		var monitors []struct {
			ID string `json:"id"`
		}
		waveDecode(t, list, &monitors)
		if len(monitors) != 1 || monitors[0].ID != monitor.ID {
			t.Fatalf("monitors = %+v", monitors)
		}
		risks := waveDo(t, base+"/api/v1/monitoring/traffic-risks", http.MethodGet, nil, "admin")
		waveExpect(t, risks, http.StatusOK)
		if denied := waveDo(t, base+"/api/v1/monitoring/availability-monitors", http.MethodGet, nil, "intruder"); denied.Status != http.StatusForbidden {
			t.Fatalf("intruder monitor list = %d", denied.Status)
		}
		if anonymous := waveDo(t, base+"/api/v1/monitoring/availability-monitors", http.MethodGet, nil, "anonymous"); anonymous.Status != http.StatusUnauthorized {
			t.Fatalf("anonymous monitor list = %d", anonymous.Status)
		}
	})

	t.Run("quota", func(t *testing.T) {
		put := waveDo(t, base+"/api/v1/nodes/n1/traffic-quota", http.MethodPut, map[string]any{
			"nodeId": "n1", "quotaGb": 1, "thresholdsPct": []int{80}, "direction": "combined",
		}, "admin")
		waveExpect(t, put, http.StatusOK)
		now := time.Now().UTC()
		periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		if _, err := env.quotaSvc.RecordUsage(context.Background(), quota.UsageSample{
			NodeID: "n1", PeriodStart: periodStart, PeriodEnd: periodStart.Add(30 * 24 * time.Hour),
			BytesIn: 600e6, BytesOut: 300e6, Source: quota.SourceAgent, ObservedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		get := waveDo(t, base+"/api/v1/nodes/n1/traffic-quota", http.MethodGet, nil, "admin")
		waveExpect(t, get, http.StatusOK)
		var view struct {
			QuotaBytes int64    `json:"quotaBytes"`
			UsedBytes  *float64 `json:"usedBytes"`
			Source     string   `json:"source"`
		}
		waveDecode(t, get, &view)
		if view.QuotaBytes != 1e9 || view.UsedBytes == nil || *view.UsedBytes != 900e6 || view.Source != "agent_estimate" {
			t.Fatalf("quota view = %+v", view)
		}
		risks := waveDo(t, base+"/api/v1/monitoring/traffic-risks", http.MethodGet, nil, "admin")
		waveExpect(t, risks, http.StatusOK)
		var riskItems []monitoring.TrafficRisk
		waveDecode(t, risks, &riskItems)
		if len(riskItems) != 1 || riskItems[0].Kind != "quota_near_limit" || !riskItems[0].Estimated {
			t.Fatalf("risks = %+v", riskItems)
		}
		if denied := waveDo(t, base+"/api/v1/nodes/n1/traffic-quota", http.MethodGet, nil, "intruder"); denied.Status != http.StatusForbidden {
			t.Fatalf("intruder quota = %d", denied.Status)
		}
		if limited := waveDo(t, base+"/api/v1/nodes/n2/traffic-quota", http.MethodPut, map[string]any{"quotaGb": 1, "warningPercent": 80}, "limited"); limited.Status != http.StatusForbidden {
			t.Fatalf("limited n2 quota = %d body=%s", limited.Status, limited.Body)
		}
		if unknown := waveDo(t, base+"/api/v1/nodes/missing/traffic-quota", http.MethodPut, map[string]any{"quotaGb": 1, "warningPercent": 80}, "admin"); unknown.Status != http.StatusNotFound {
			t.Fatalf("unknown quota = %d", unknown.Status)
		}
		// Concurrent policy writes must serialize through SQLite and leave one
		// complete winner rather than a torn row.
		var wg sync.WaitGroup
		statuses := make([]int, 2)
		for index, limit := range []float64{2, 3} {
			wg.Add(1)
			go func(index int, limit float64) {
				defer wg.Done()
				result := waveDo(t, base+"/api/v1/nodes/n1/traffic-quota", http.MethodPut, map[string]any{
					"quotaGb": limit, "thresholdsPct": []int{70},
				}, "admin")
				statuses[index] = result.Status
			}(index, limit)
		}
		wg.Wait()
		for _, status := range statuses {
			if status != http.StatusOK {
				t.Fatalf("concurrent put status = %v", statuses)
			}
		}
		final := waveDo(t, base+"/api/v1/nodes/n1/traffic-quota", http.MethodGet, nil, "admin")
		waveExpect(t, final, http.StatusOK)
		var finalView struct {
			QuotaBytes int64 `json:"quotaBytes"`
		}
		waveDecode(t, final, &finalView)
		if finalView.QuotaBytes != 2e9 && finalView.QuotaBytes != 3e9 {
			t.Fatalf("concurrent quota result = %d", finalView.QuotaBytes)
		}
	})

	t.Run("certificates", func(t *testing.T) {
		created := waveDo(t, base+"/api/v1/certificates/internal/distributions", http.MethodPost, map[string]any{
			"serviceGroupId": "sg1", "credentialType": "trust_bundle", "confirmServiceImpact": true,
			"expectedGroupRevision": 3, "validityHours": 24,
		}, "admin")
		waveExpect(t, created, http.StatusAccepted)
		if bytes.Contains(created.Body, []byte("PRIVATE KEY")) || bytes.Contains(created.Body, []byte("keyRef")) {
			t.Fatalf("certificate response leaks key metadata: %s", created.Body)
		}
		var job struct {
			JobID          string   `json:"jobId"`
			State          string   `json:"state"`
			MemberNodeIDs  []string `json:"memberNodeIdsSnapshot"`
			CredentialType string   `json:"credentialType"`
		}
		waveDecode(t, created, &job)
		if job.JobID == "" || job.State != "queued" || len(job.MemberNodeIDs) != 2 || job.CredentialType != "trust_bundle" {
			t.Fatalf("job = %+v", job)
		}
		list := waveDo(t, base+"/api/v1/certificates/internal/distributions", http.MethodGet, nil, "admin")
		waveExpect(t, list, http.StatusOK)
		var page struct {
			Items []struct {
				JobID string `json:"jobId"`
			} `json:"items"`
		}
		waveDecode(t, list, &page)
		if len(page.Items) != 1 || page.Items[0].JobID != job.JobID {
			t.Fatalf("list = %s", list.Body)
		}
		distributionID := fmt.Sprintf("%d:%s:%s", len(job.JobID), job.JobID, "n1")
		revoked := waveDo(t, base+"/api/v1/certificates/internal/distributions/"+distributionID+"/revoke", http.MethodPost, map[string]any{
			"reason": "node retired",
		}, "admin")
		waveExpect(t, revoked, http.StatusAccepted)
		var revokedBody struct {
			State string `json:"state"`
		}
		waveDecode(t, revoked, &revokedBody)
		if revokedBody.State != "revoked" {
			t.Fatalf("revoked = %+v", revokedBody)
		}
		if env.issuer.revoked == 0 {
			t.Fatal("issuer revoke was not called")
		}
		hidden := waveDo(t, base+"/api/v1/certificates/internal/distributions", http.MethodGet, nil, "intruder")
		waveExpect(t, hidden, http.StatusOK)
		var hiddenPage struct {
			Items []struct {
				JobID string `json:"jobId"`
			} `json:"items"`
		}
		waveDecode(t, hidden, &hiddenPage)
		if len(hiddenPage.Items) != 0 {
			t.Fatalf("intruder saw certificate rows: %s", hidden.Body)
		}
		if denied := waveDo(t, base+"/api/v1/certificates/internal/distributions", http.MethodPost, map[string]any{
			"serviceGroupId": "sg1", "credentialType": "trust_bundle", "confirmServiceImpact": true,
			"expectedGroupRevision": 3,
		}, "intruder"); denied.Status != http.StatusForbidden {
			t.Fatalf("intruder certificate create = %d body=%s", denied.Status, denied.Body)
		}

		second := waveDo(t, base+"/api/v1/certificates/internal/distributions", http.MethodPost, map[string]any{
			"serviceGroupId": "sg1", "credentialType": "per_node_identity", "confirmServiceImpact": true,
			"expectedGroupRevision": 3,
		}, "admin")
		waveExpect(t, second, http.StatusAccepted)
		var secondJob struct {
			JobID string `json:"jobId"`
		}
		waveDecode(t, second, &secondJob)
		env.issuer.setRevokeError(errors.New("issuer offline"))
		failed := waveDo(t, base+"/api/v1/certificates/internal/distributions/"+secondJob.JobID+"/revoke", http.MethodPost, map[string]any{
			"reason": "rotate",
		}, "admin")
		waveExpect(t, failed, http.StatusBadGateway)
		after := waveDo(t, base+"/api/v1/certificates/internal/distributions", http.MethodGet, nil, "admin")
		waveExpect(t, after, http.StatusOK)
		var afterPage struct {
			Items []struct {
				JobID string `json:"jobId"`
				State string `json:"state"`
			} `json:"items"`
		}
		waveDecode(t, after, &afterPage)
		for _, item := range afterPage.Items {
			if item.JobID == secondJob.JobID && item.State != "queued" {
				t.Fatalf("failed revoke changed state: %+v", item)
			}
		}
	})

	t.Run("publishing", func(t *testing.T) {
		draft := map[string]any{
			"serviceGroupId": "sg1", "gatewayIds": []string{"gw1"}, "domain": "app.example.com", "https": true,
		}
		preview := waveDo(t, base+"/api/v1/publishing/preview", http.MethodPost, draft, "admin")
		waveExpect(t, preview, http.StatusOK)
		var previewBody struct {
			Eligible              bool     `json:"eligible"`
			TargetNodeIDsSnapshot []string `json:"targetNodeIdsSnapshot"`
		}
		waveDecode(t, preview, &previewBody)
		if !previewBody.Eligible || len(previewBody.TargetNodeIDsSnapshot) != 2 {
			t.Fatalf("preview = %+v", previewBody)
		}
		created := waveDo(t, base+"/api/v1/publishing/publications", http.MethodPost, draft, "admin")
		waveExpect(t, created, http.StatusAccepted)
		var operation struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Stages []struct {
				Name string `json:"name"`
			} `json:"stages"`
		}
		waveDecode(t, created, &operation)
		if operation.ID == "" || operation.Status != "queued" || len(operation.Stages) != 3 {
			t.Fatalf("operation = %+v", operation)
		}
		list := waveDo(t, base+"/api/v1/publishing/publications", http.MethodGet, nil, "admin")
		waveExpect(t, list, http.StatusOK)
		var items []struct {
			ID string `json:"id"`
		}
		waveDecode(t, list, &items)
		if len(items) != 1 || items[0].ID != operation.ID {
			t.Fatalf("publications = %+v", items)
		}
		one := waveDo(t, base+"/api/v1/publishing/publications/"+operation.ID, http.MethodGet, nil, "admin")
		waveExpect(t, one, http.StatusOK)
		staleDraft := map[string]any{
			"serviceGroupId": "sg1", "gatewayIds": []string{"gw1"}, "domain": "app.example.com",
			"https": true, "expectedGroupRevision": 2,
		}
		stalePreview := waveDo(t, base+"/api/v1/publishing/preview", http.MethodPost, staleDraft, "admin")
		waveExpect(t, stalePreview, http.StatusOK)
		var staleBody struct {
			Eligible bool `json:"eligible"`
		}
		waveDecode(t, stalePreview, &staleBody)
		if staleBody.Eligible {
			t.Fatal("stale preview reported eligible")
		}
		staleCreate := waveDo(t, base+"/api/v1/publishing/publications", http.MethodPost, staleDraft, "admin")
		waveExpect(t, staleCreate, http.StatusConflict)
		if denied := waveDo(t, base+"/api/v1/publishing/preview", http.MethodPost, draft, "intruder"); denied.Status != http.StatusForbidden {
			t.Fatalf("intruder preview = %d", denied.Status)
		}
		if missing := waveDo(t, base+"/api/v1/publishing/publications/missing", http.MethodGet, nil, "admin"); missing.Status != http.StatusNotFound {
			t.Fatalf("missing publication = %d", missing.Status)
		}
	})

	t.Run("gateway", func(t *testing.T) {
		created, err := env.gwSvc.CreateGateway(context.Background(), "admin", "req-gw", gateway.CreateGatewayInput{
			ID: "gw-wave3", NodeID: "n1", Name: "Edge", Enabled: false,
		})
		if err != nil {
			t.Fatal(err)
		}
		checks := waveDo(t, base+"/api/v1/gateways/"+created.ID+"/checks", http.MethodGet, nil, "admin")
		waveExpect(t, checks, http.StatusOK)
		var checksBody struct {
			Recorded bool   `json:"recorded"`
			Message  string `json:"message"`
		}
		waveDecode(t, checks, &checksBody)
		if checksBody.Recorded || checksBody.Message == "" {
			t.Fatalf("checks = %+v", checksBody)
		}
		dispatch := waveDo(t, base+"/api/v1/gateways/"+created.ID+"/dispatch", http.MethodPatch, map[string]any{
			"state": "enabled", "expectedRevision": created.Revision,
		}, "admin")
		waveExpect(t, dispatch, http.StatusOK)
		var dispatched struct {
			Enabled bool `json:"enabled"`
		}
		waveDecode(t, dispatch, &dispatched)
		if !dispatched.Enabled {
			t.Fatalf("dispatch = %+v", dispatched)
		}
		stale := waveDo(t, base+"/api/v1/gateways/"+created.ID+"/dispatch", http.MethodPatch, map[string]any{
			"enabled": false, "expectedRevision": created.Revision,
		}, "admin")
		waveExpect(t, stale, http.StatusConflict)
		if _, err := env.gwSvc.RecordChecks(context.Background(), "admin", "req-gw-checks", created.ID, created.Revision+1, gateway.CheckReport{
			NginxInstalled: true, ChecksPassed: true, NginxVersion: "1.27.0",
		}); err != nil {
			t.Fatal(err)
		}
		recorded := waveDo(t, base+"/api/v1/gateways/"+created.ID+"/checks", http.MethodGet, nil, "admin")
		waveExpect(t, recorded, http.StatusOK)
		var recordedBody struct {
			Recorded       bool `json:"recorded"`
			NginxInstalled bool `json:"nginxInstalled"`
			ChecksPassed   bool `json:"checksPassed"`
		}
		waveDecode(t, recorded, &recordedBody)
		if !recordedBody.Recorded || !recordedBody.NginxInstalled || !recordedBody.ChecksPassed {
			t.Fatalf("recorded checks = %+v", recordedBody)
		}
		if denied := waveDo(t, base+"/api/v1/gateways/"+created.ID+"/checks", http.MethodGet, nil, "intruder"); denied.Status != http.StatusForbidden {
			t.Fatalf("intruder gateway = %d", denied.Status)
		}
	})

	t.Run("nodes", func(t *testing.T) {
		identity := waveDo(t, base+"/api/v1/nodes/n1/identity", http.MethodGet, nil, "admin")
		waveExpect(t, identity, http.StatusOK)
		var identityBody struct {
			Recorded bool `json:"recorded"`
			Revoked  bool `json:"revoked"`
		}
		waveDecode(t, identity, &identityBody)
		if identityBody.Recorded || identityBody.Revoked {
			t.Fatalf("identity = %+v", identityBody)
		}
		capabilities := waveDo(t, base+"/api/v1/nodes/n1/capabilities", http.MethodGet, nil, "admin")
		waveExpect(t, capabilities, http.StatusOK)
		var capabilitiesBody struct {
			Recorded     bool     `json:"recorded"`
			Capabilities []string `json:"capabilities"`
		}
		waveDecode(t, capabilities, &capabilitiesBody)
		if capabilitiesBody.Recorded || capabilitiesBody.Capabilities == nil {
			t.Fatalf("capabilities = %+v", capabilitiesBody)
		}
		diagnostics := waveDo(t, base+"/api/v1/nodes/n1/diagnostics", http.MethodGet, nil, "admin")
		waveExpect(t, diagnostics, http.StatusOK)
		var diagnosticsBody struct {
			ConnectionState string `json:"connectionState"`
		}
		waveDecode(t, diagnostics, &diagnosticsBody)
		if diagnosticsBody.ConnectionState != "online" {
			t.Fatalf("diagnostics = %+v", diagnosticsBody)
		}
		suspend := waveDo(t, base+"/api/v1/nodes/n1/suspend", http.MethodPost, map[string]any{"reason": "maintenance window"}, "admin")
		waveExpect(t, suspend, http.StatusOK)
		node, err := env.nodeRepo.Get(context.Background(), "n1")
		if err != nil || node.Status != nodes.StatusSuspended {
			t.Fatalf("suspended node = %+v err=%v", node, err)
		}
		if _, applied, err := env.nodeRepo.ApplyAgentUpdate(context.Background(), env.db, nodes.AgentUpdate{
			NodeID: "n1", Status: nodes.StatusOnline, Sequence: 2, ObservedAt: time.Now().UTC(),
		}); err != nil || !applied {
			t.Fatalf("agent update after suspend: %v applied=%t", err, applied)
		}
		node, err = env.nodeRepo.Get(context.Background(), "n1")
		if err != nil || node.Status != nodes.StatusSuspended {
			t.Fatalf("agent update overrode suspension: %+v err=%v", node, err)
		}
		revoke := waveDo(t, base+"/api/v1/nodes/n2/revoke", http.MethodPost, map[string]any{"reason": "retired"}, "admin")
		waveExpect(t, revoke, http.StatusOK)
		var revoked struct {
			Status  string `json:"status"`
			Revoked bool   `json:"revoked"`
		}
		waveDecode(t, revoke, &revoked)
		if revoked.Status != "revoked" || !revoked.Revoked {
			t.Fatalf("revoked = %+v", revoked)
		}
		late := waveDo(t, base+"/api/v1/nodes/n2/suspend", http.MethodPost, map[string]any{"reason": "late"}, "admin")
		waveExpect(t, late, http.StatusConflict)
		if denied := waveDo(t, base+"/api/v1/nodes/n2/identity", http.MethodGet, nil, "limited"); denied.Status != http.StatusForbidden {
			t.Fatalf("limited node identity = %d", denied.Status)
		}
	})

	t.Run("tasks", func(t *testing.T) {
		created := waveDo(t, base+"/api/v1/tasks", http.MethodPost, map[string]any{
			"name": "offline wait", "command": "host.patch",
			"target": map[string]any{"type": "node_ids", "nodeIds": []string{"n2"}},
			"dryRun": false,
		}, "admin")
		waveExpect(t, created, http.StatusAccepted)
		var job struct {
			JobID    string `json:"jobId"`
			State    string `json:"state"`
			Revision uint64 `json:"revision"`
		}
		waveDecode(t, created, &job)
		if job.JobID == "" || job.State != "queued" || job.Revision != 1 {
			t.Fatalf("job = %+v", job)
		}
		executions := waveDo(t, base+"/api/v1/tasks/"+job.JobID+"/executions", http.MethodGet, nil, "admin")
		waveExpect(t, executions, http.StatusOK)
		var page struct {
			Items []struct {
				NodeID string `json:"nodeId"`
				State  string `json:"state"`
			} `json:"items"`
		}
		waveDecode(t, executions, &page)
		if len(page.Items) != 1 || page.Items[0].NodeID != "n2" || page.Items[0].State != "queued" {
			t.Fatalf("executions = %+v", page)
		}
		resumed := waveDo(t, base+"/api/v1/tasks/"+job.JobID+"/resume", http.MethodPost, map[string]any{
			"reason": "extend offline wait", "expectedRevision": 1,
		}, "admin")
		waveExpect(t, resumed, http.StatusOK)
		var resumedBody struct {
			Revision uint64 `json:"revision"`
			State    string `json:"state"`
		}
		waveDecode(t, resumed, &resumedBody)
		if resumedBody.Revision != 2 || resumedBody.State != "queued" {
			t.Fatalf("resumed = %+v", resumedBody)
		}
		stale := waveDo(t, base+"/api/v1/tasks/"+job.JobID+"/resume", http.MethodPost, map[string]any{
			"reason": "again", "expectedRevision": 1,
		}, "admin")
		waveExpect(t, stale, http.StatusConflict)
		cancel := waveDo(t, base+"/api/v1/tasks/"+job.JobID+"/cancel", http.MethodPost, map[string]any{
			"reason": "operator stop", "expectedRevision": 2,
		}, "admin")
		waveExpect(t, cancel, http.StatusOK)
		terminal := waveDo(t, base+"/api/v1/tasks/"+job.JobID+"/resume", http.MethodPost, map[string]any{
			"reason": "too late",
		}, "admin")
		waveExpect(t, terminal, http.StatusConflict)
		if denied := waveDo(t, base+"/api/v1/tasks", http.MethodPost, map[string]any{
			"name": "intruder", "command": "host.patch",
			"target": map[string]any{"type": "node_ids", "nodeIds": []string{"n1"}},
		}, "intruder"); denied.Status != http.StatusForbidden {
			t.Fatalf("intruder task = %d", denied.Status)
		}
	})

	t.Run("audit", func(t *testing.T) {
		for _, action := range []string{
			monitoring.ActionCreate, quota.ActionPolicy, certificates.ActionDistributionCreate,
			certificates.ActionCertificateRevoke, publishing.ActionCreate, gateway.ActionUpdate,
			nodes.ActionNodesSuspend, nodes.ActionNodesRevoke, tasks.ActionResume, tasks.ActionCancel,
		} {
			page, err := env.recorder.Query(context.Background(), audit.Query{Action: action, Limit: 50})
			if err != nil {
				t.Fatalf("audit query %s: %v", action, err)
			}
			if page.Total == 0 {
				t.Fatalf("audit action %s has no events", action)
			}
		}
	})
}
