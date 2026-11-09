package server

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"serverfleet.local/serverfleet/internal/transport"
)

type publicCertificateExecution struct {
	s                                   *Server
	authorityID                         string
	o                                   ACMEDeployment
	profileSHA, accountSHA, accountSPKI string
	provider                            dns.Provider
	acmeClient                          *http.Client
	acmeRoots                           *x509.CertPool
	transports                          []*http.Transport
	trustFiles                          map[string]string
}

func (s *Server) newPublicCertificateExecution(ctx context.Context, o ACMEDeployment) (*publicCertificateExecution, error) {
	if !validPublicCertificateDeployment(o) {
		return nil, certificates.ErrACMEConfig
	}
	_, accountSHA, accountSPKI, err := loadACMEAccountKey(o.AccountKeyFile)
	if err != nil {
		return nil, err
	}
	for _, forbidden := range o.ForbiddenKeySPKI {
		if len(forbidden) != 64 || accountSPKI == forbidden {
			return nil, certificates.ErrACMEConfig
		}
	}
	o.ForbiddenKeySPKI = append([]string{}, o.ForbiddenKeySPKI...)
	trustFiles := map[string]string{}
	for _, filename := range []string{o.ACMERootFile, o.DNSRootFile} {
		if filename == "" {
			continue
		}
		raw, e := readPrivateDeployment(filename)
		if e != nil {
			return nil, certificates.ErrACMEConfig
		}
		digest := sha256.Sum256(raw)
		trustFiles[filename] = hex.EncodeToString(digest[:])
	}
	raw, err := json.Marshal(struct {
		Options    ACMEDeployment
		AccountSHA string
		TrustFiles map[string]string
	}{o, accountSHA, trustFiles})
	if err != nil {
		return nil, certificates.ErrACMEConfig
	}
	sum := sha256.Sum256(raw)
	m := &publicCertificateExecution{s: s, o: o, accountSHA: accountSHA, accountSPKI: accountSPKI, profileSHA: hex.EncodeToString(sum[:]), trustFiles: trustFiles}
	client, roots, err := m.client(o.DirectoryURL, o.ACMERootFile)
	if err != nil {
		m.Close()
		return nil, err
	}
	m.acmeClient, m.acmeRoots = client, roots
	providerClient, _, err := m.client(o.CloudflareURL, o.DNSRootFile)
	if err != nil {
		m.Close()
		return nil, err
	}
	m.provider, err = dns.NewCloudflareProvider(dns.CloudflareConfig{Client: providerClient, BaseURL: o.CloudflareURL, Secrets: dns.SecretBorrowerFunc(m.borrowDNS)})
	if err != nil {
		m.Close()
		return nil, err
	}
	if err = certificates.RecoverPublicOrders(ctx, s.DB); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}
func (m *publicCertificateExecution) Close() {
	if m != nil {
		for _, tr := range m.transports {
			tr.CloseIdleConnections()
		}
	}
}
func (m *publicCertificateExecution) taskScope(t tasks.Task, zone string) *publicCertificateScope {
	return &publicCertificateScope{manager: m, task: t, zoneID: zone, leases: map[string]auth.CredentialOperation{}}
}
func (m *publicCertificateExecution) requestParams(t tasks.Task) (string, string, error) {
	var p map[string]string
	if t.ActionID != actionPublicCertificateIssue || t.Source != "web" || t.ScopeRevision != 0 || t.PluginID != "" || t.OperationClass != tasks.OperationClassModifying || !t.ApprovalRequired || len(t.Targets) != 1 || json.Unmarshal([]byte(t.Params), &p) != nil || len(p) != 3 || p["profileSha256"] != m.profileSHA || !deploymentID.MatchString(p["csrTaskId"]) || !deploymentID.MatchString(p["zoneId"]) {
		return "", "", certificates.ErrACMEConfig
	}
	return p["csrTaskId"], p["zoneId"], nil
}
func (m *publicCertificateExecution) csr(ctx context.Context, id, node string) (transport.CertificateCSRReceipt, error) {
	task, err := m.s.TasksRepo.Get(ctx, id)
	if err != nil || task.ActionID != transport.CertificateCSRAction || task.Status != tasks.StatusCompleted || len(task.Targets) != 1 || task.Targets[0].NodeID != node || task.Targets[0].State != tasks.TargetSucceeded {
		return transport.CertificateCSRReceipt{}, certificates.ErrACMEConfig
	}
	proof, err := transport.ReadCertificateCSR(ctx, m.s.DB, id)
	if err != nil || proof.PlanSHA256 != task.PlanHash || proof.NodeID != node {
		return transport.CertificateCSRReceipt{}, certificates.ErrACMEConfig
	}
	return proof, nil
}
func (m *publicCertificateExecution) zone(ctx context.Context, t tasks.Task, zone, domain string) error {
	binding, err := m.s.DNS.ExecutionBinding(ctx, t.SubmitterID, zone)
	name := strings.TrimPrefix(domain, "*.")
	if err != nil || binding.CredentialID != m.o.DNSCredentialID || name != binding.Name && !strings.HasSuffix(name, "."+binding.Name) {
		return certificates.ErrACMEConfig
	}
	return nil
}
func (m *publicCertificateExecution) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	result := func(code tasks.NodeResultCode, message string) (map[string]tasks.WorkerResult, error) {
		return map[string]tasks.WorkerResult{t.Targets[0].NodeID: {Code: code, Message: message}}, nil
	}
	if len(t.Targets) != 1 {
		return nil, tasks.ErrInvalidTask
	}
	csrID, zoneID, err := m.requestParams(t)
	if err != nil {
		return result(tasks.NodeResultFailed, "public certificate plan rejected")
	}
	scope := m.taskScope(t, zoneID)
	ctx, cancel := context.WithTimeout(m.scope(ctx, scope), 3*time.Minute)
	defer cancel()
	if m.current(ctx, scope) != nil {
		return result(tasks.NodeResultFailed, "certificate authority unavailable")
	}
	csr, err := m.csr(ctx, csrID, t.Targets[0].NodeID)
	if err != nil || m.zone(ctx, t, zoneID, csr.Domain) != nil {
		return result(tasks.NodeResultFailed, "CSR or DNS zone binding rejected")
	}
	order := certificates.PublicOrder{TaskID: t.ID, NodeID: csr.NodeID, CSRTaskID: csrID, Domain: csr.Domain, KeyRef: csr.KeyRef, PlanSHA256: t.PlanHash, ProfileSHA256: m.profileSHA, ZoneID: zoneID, Status: "running", UpdatedAt: time.Now().UTC()}
	err = m.s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := certificates.ReservePublicOrderTx(ctx, tx, order); err != nil {
			return err
		}
		return m.s.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: t.SubmitterID, Action: actionPublicCertificateIssue, Result: "allowed", RequestID: t.RequestID, TaskID: t.ID, NodeIDs: []string{csr.NodeID}, Source: audit.SourceWeb, ReasonCode: "public_certificate_order_reserved", At: order.UpdatedAt})
	})
	if err != nil {
		return result(tasks.NodeResultUnknown, "certificate reservation outcome unknown; no automatic replay")
	}
	publisher, err := dns.NewACMEChallengePublisher(dns.ACMEPublisherConfig{Service: m.s.DNS, SubjectID: t.SubmitterID, RequestID: t.RequestID, ZoneID: zoneID, TTL: 60})
	if err != nil {
		return m.finish(ctx, t, order, nil, err, nil)
	}
	tracked := &trackedCertificateChallenges{manager: m, scope: scope, next: publisher, pending: map[string]certificateChallengeState{}}
	client, err := certificates.NewACMEClient(certificates.ACMEConfig{DirectoryURL: m.o.DirectoryURL, Client: m.acmeClient, AccountKey: "account", Signer: privateACMEAccountSigner{manager: m, scope: scope}, Challenge: tracked, TrustRoots: m.acmeRoots, Timeout: 2 * time.Minute})
	if err != nil {
		return m.finish(ctx, t, order, nil, err, tracked)
	}
	csrDER, err := base64.StdEncoding.Strict().DecodeString(csr.PublicResult.CSRDER)
	if err != nil {
		return m.finish(ctx, t, order, nil, err, tracked)
	}
	issued, issueErr := client.IssueDNS01WithCSR(ctx, certificates.ACMERequest{CommonName: csr.Domain, KeyRef: certificates.KeyRef(csr.KeyRef)}, csrDER)
	return m.finish(ctx, t, order, &issued, issueErr, tracked)
}

type certificateChallengeState struct {
	record  certificates.ACMEDNS01Record
	unknown bool
}
type trackedCertificateChallenges struct {
	manager *publicCertificateExecution
	scope   *publicCertificateScope
	next    *dns.ACMEChallengePublisher
	mu      sync.Mutex
	pending map[string]certificateChallengeState
}

func (p *trackedCertificateChallenges) Publish(ctx context.Context, fqdn, value string) (string, func(context.Context) error, error) {
	// Freeze the intended name/value before any remote create. A crash or lost
	// provider response keeps an UNKNOWN intent without granting deletion rights.
	intentSum := sha256.Sum256([]byte(fqdn + "\x00" + value))
	intent := "unobserved-" + hex.EncodeToString(intentSum[:])
	p.mu.Lock()
	p.pending[intent] = certificateChallengeState{record: certificates.ACMEDNS01Record{FQDN: fqdn, Value: value}, unknown: true}
	err := p.persist(ctx)
	p.mu.Unlock()
	if err != nil {
		return "", nil, err
	}
	id, cleanup, err := p.next.Publish(ctx, fqdn, value)
	if err != nil {
		return id, nil, err
	}
	p.mu.Lock()
	delete(p.pending, intent)
	p.pending[id] = certificateChallengeState{record: certificates.ACMEDNS01Record{FQDN: fqdn, Value: value, RecordID: id}}
	persistErr := p.persist(ctx)
	p.mu.Unlock()
	if persistErr != nil {
		return id, nil, persistErr
	}
	return id, func(ctx context.Context) error {
		ctx = p.manager.scope(ctx, p.scope)
		err := cleanup(ctx)
		p.mu.Lock()
		defer p.mu.Unlock()
		if err == nil {
			delete(p.pending, id)
		} else {
			state := p.pending[id]
			state.unknown = !errors.Is(err, dns.ErrProviderFailed) || errors.Is(err, auth.ErrCredentialUseUnknown) || ctx.Err() != nil
			p.pending[id] = state
		}
		if persistErr := p.persist(ctx); persistErr != nil && err == nil {
			return persistErr
		}
		return err
	}, nil
}

// Caller holds pending's mutex; the audit and updated public evidence commit
// together. No provider call or plaintext secret is part of this transaction.
func (p *trackedCertificateChallenges) persist(ctx context.Context) error {
	records := make([]certificates.ACMEDNS01Record, 0, len(p.pending))
	for _, record := range p.pending {
		records = append(records, record.record)
	}
	return p.manager.s.DB.InTx(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := certificates.ObservePublicOrderChallengesTx(ctx, tx, p.scope.task.ID, p.scope.task.PlanHash, records); err != nil {
			return err
		}
		return p.manager.s.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: p.scope.task.SubmitterID, Action: actionPublicCertificateIssue, Result: "allowed", RequestID: p.scope.task.RequestID, TaskID: p.scope.task.ID, Source: audit.SourceWeb, ReasonCode: "dns_challenge_observation", At: time.Now().UTC()})
	})
}
func (m *publicCertificateExecution) finish(ctx context.Context, t tasks.Task, order certificates.PublicOrder, issued *certificates.ACMEIssued, issueErr error, tracked *trackedCertificateChallenges) (map[string]tasks.WorkerResult, error) {
	finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	pending := []certificateChallengeState{}
	if tracked != nil {
		tracked.mu.Lock()
		for _, record := range tracked.pending {
			pending = append(pending, record)
		}
		tracked.mu.Unlock()
	}
	for _, record := range pending {
		order.PendingCleanup = append(order.PendingCleanup, record.record)
		if m.s.CertificateCleanup != nil && record.record.RecordID != "" {
			_, _ = m.s.CertificateCleanup.RecordFailureOutcome(finalCtx, t.SubmitterID, t.RequestID, t.ID, order.Domain, []certificates.ACMEDNS01Record{record.record}, record.unknown)
		}
	}
	order.Status = "unknown"
	order.ErrorCode = "certificate_provider_outcome_unknown"
	order.UpdatedAt = time.Now().UTC()
	code := tasks.NodeResultUnknown
	if issued != nil && len(issued.CertificatePEM) > 0 {
		chain := append(append([]byte{}, issued.CertificatePEM...), issued.ChainPEM...)
		if _, err := transport.ParsePublicCertificateChain(chain); err == nil {
			order.ChainPEM = chain
			sum := sha256.Sum256(chain)
			order.ChainSHA256 = hex.EncodeToString(sum[:])
			order.Serial = issued.Serial
			order.NotAfter = issued.NotAfter
			order.Status = "issued"
			order.ErrorCode = ""
			code = tasks.NodeResultSucceeded
			if len(pending) > 0 || issueErr != nil {
				order.Status = "issued_cleanup_pending"
				order.ErrorCode = "cleanup_unresolved"
				code = tasks.NodeResultFailed
			}
		}
	}
	err := m.s.DB.InTx(finalCtx, func(ctx context.Context, tx storage.Tx) error {
		if err := certificates.FinishPublicOrderTx(ctx, tx, order); err != nil {
			return err
		}
		result := "failure"
		if code == tasks.NodeResultSucceeded {
			result = "success"
		}
		return m.s.Audit.AppendTx(ctx, tx, audit.Event{SubjectID: t.SubmitterID, Action: actionPublicCertificateIssue, Result: result, RequestID: t.RequestID, TaskID: t.ID, NodeIDs: []string{order.NodeID}, Source: audit.SourceWeb, ReasonCode: "public_order_terminal_observed", At: order.UpdatedAt})
	})
	if err != nil {
		code = tasks.NodeResultUnknown
	}
	return map[string]tasks.WorkerResult{order.NodeID: {Code: code, Message: order.Status + "; no automatic replay of CA/provider effects"}}, nil
}
