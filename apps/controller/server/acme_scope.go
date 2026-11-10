package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/tasks"
)

const actionPublicCertificateIssue = "certificates.public.issue"

type certificateExecutionKey struct{}
type publicCertificateScope struct {
	manager       *publicCertificateExecution
	dnsBatchIndex int
	task          tasks.Task
	zoneID        string
	cleanup       bool
	cleanupID     int64
	requestID     string
	mu            sync.Mutex
	leases        map[string]auth.CredentialOperation
}

func (m *publicCertificateExecution) scope(ctx context.Context, scope *publicCertificateScope) context.Context {
	if scope.requestID == "" {
		scope.requestID = scope.task.RequestID
	}
	ctx = context.WithValue(ctx, certificateExecutionKey{}, scope)
	return dns.WithSecretScope(ctx, dns.SecretScope{SubjectID: scope.task.SubmitterID, RequestID: scope.requestID, PlanDigest: scope.task.PlanHash})
}
func (m *publicCertificateExecution) current(ctx context.Context, scope *publicCertificateScope) error {
	if m == nil || scope == nil || scope.manager != m || ctx == nil || ctx.Err() != nil || m.s.runtimeGate(ctx) != nil || m.s.Identity.AuthorizeAdministrator(ctx, scope.task.SubmitterID) != nil {
		return certificates.ErrACMEConfig
	}
	for filename, expected := range m.trustFiles {
		raw, e := readPrivateDeployment(filename)
		if e != nil {
			return certificates.ErrACMEConfig
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != expected {
			return certificates.ErrACMEConfig
		}
	}
	if scope.cleanup {
		if m.s.CertificateCleanup == nil {
			return certificates.ErrCleanupUnavailable
		}
		record, err := m.s.CertificateCleanup.CurrentExecution(ctx)
		if err != nil || record.ID != scope.cleanupID || record.SubjectID != scope.task.SubmitterID || record.PlanID != scope.task.ID {
			return certificates.ErrCleanupBusy
		}
	} else {
		current, err := m.s.Tasks.CheckExecution(ctx, scope.task.ID, scope.task.PlanHash)
		if err != nil || current.Params != scope.task.Params || !m.validExecutionTask(ctx, current) {
			return certificates.ErrACMEConfig
		}
	}
	if scope.task.ActionID == actionPublicationDNSApply || scope.task.ActionID == actionPublicationDNSRollback {
		if m.batchCurrent(ctx, scope) != nil {
			return auth.ErrDenied
		}
	} else if scope.task.ActionID == actionDNSProviderInspect {
		p, e := m.inspectionParams(scope.task)
		if e != nil {
			return auth.ErrDenied
		}
		ref, e := m.s.DNS.AccountExecutionBinding(ctx, scope.task.SubmitterID, p["accountId"])
		if e != nil || ref != m.o.DNSCredentialID {
			return auth.ErrDenied
		}
	} else if scope.task.ActionID == actionDNSRecordApply {
		p, _, e := m.mutationParams(scope.task)
		if e != nil || p["zoneId"] != scope.zoneID || m.mutationCurrent(ctx, scope.task, p) != nil {
			return auth.ErrDenied
		}
	} else {
		csrID, zoneID, e := m.requestParams(scope.task)
		if e != nil || zoneID != scope.zoneID {
			return auth.ErrDenied
		}
		csr, e := m.csr(ctx, csrID, scope.task.Targets[0].NodeID)
		if e != nil || m.zone(ctx, scope.task, zoneID, csr.Domain) != nil {
			return auth.ErrDenied
		}
	}
	meta, err := m.s.Credentials.ServiceMetadata(ctx, scope.task.SubmitterID, m.o.DNSCredentialID)
	if err != nil || meta.Kind != "cloudflare_token" || meta.Revision != m.o.DNSCredentialRevision {
		return auth.ErrDenied
	}
	return nil
}
func (m *publicCertificateExecution) credentialGate(ctx context.Context, op auth.CredentialOperation) error {
	scope, ok := ctx.Value(certificateExecutionKey{}).(*publicCertificateScope)
	if !ok || scope == nil || scope.manager != m {
		return auth.ErrDenied
	}
	scope.mu.Lock()
	bound, active := scope.leases[op.OperationID]
	scope.mu.Unlock()
	if !active || bound != op || op.SubjectID != scope.task.SubmitterID || op.CredentialID != m.o.DNSCredentialID || op.ExpectedRevision != m.o.DNSCredentialRevision || op.Module != "dns" || op.Source != "web" || op.Action != "use" || op.PlanDigest != scope.task.PlanHash || op.ChangeID != "" {
		return auth.ErrDenied
	}
	return m.current(ctx, scope)
}
func (m *publicCertificateExecution) borrowDNS(ctx context.Context, use dns.SecretUse, consume func(context.Context, string) error) error {
	scope, ok := ctx.Value(certificateExecutionKey{}).(*publicCertificateScope)
	if !ok || scope == nil || scope.manager != m || use.Ref != m.o.DNSCredentialID || use.SubjectID != scope.task.SubmitterID || use.RequestID != scope.requestID || use.PlanDigest != scope.task.PlanHash || use.Module != "dns" || use.Purpose != "dns-provider" || consume == nil || m.current(ctx, scope) != nil {
		return auth.ErrDenied
	}
	if scope.task.ActionID == actionPublicationDNSApply || scope.task.ActionID == actionPublicationDNSRollback {
		if err := m.checkBatchOperation(ctx, scope, use.Operation); err != nil {
			return err
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	op := auth.CredentialOperation{SubjectID: scope.task.SubmitterID, CredentialID: m.o.DNSCredentialID, Module: "dns", Source: "web", OperationID: "acme-dns-" + hex.EncodeToString(nonce[:]), PlanDigest: scope.task.PlanHash, Action: "use", ExpectedRevision: m.o.DNSCredentialRevision}
	scope.mu.Lock()
	scope.leases[op.OperationID] = op
	scope.mu.Unlock()
	defer func() { scope.mu.Lock(); delete(scope.leases, op.OperationID); scope.mu.Unlock() }()
	return m.s.Credentials.UseForService(ctx, op, func(current context.Context, secret []byte) error {
		if m.current(current, scope) != nil {
			return auth.ErrDenied
		}
		return consume(current, string(secret))
	})
}

type certificateRoundTripper struct {
	manager   *publicCertificateExecution
	origin    string
	transport *http.Transport
}

func (t certificateRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	scope, ok := request.Context().Value(certificateExecutionKey{}).(*publicCertificateScope)
	if !ok || request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Scheme+"://"+request.URL.Host != t.origin || t.manager.current(request.Context(), scope) != nil {
		return nil, certificates.ErrACMEConfig
	}
	response, err := t.transport.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if t.manager.current(request.Context(), scope) != nil {
		response.Body.Close()
		return nil, certificates.ErrACMEConfig
	}
	response.Body = &certificateResponseBody{ReadCloser: response.Body, manager: t.manager, scope: scope, ctx: request.Context()}
	return response, nil
}

type certificateResponseBody struct {
	io.ReadCloser
	manager *publicCertificateExecution
	scope   *publicCertificateScope
	ctx     context.Context
}

func (b *certificateResponseBody) Read(raw []byte) (int, error) {
	if b.manager.current(b.ctx, b.scope) != nil {
		return 0, certificates.ErrACMEConfig
	}
	n, err := b.ReadCloser.Read(raw)
	if b.manager.current(b.ctx, b.scope) != nil {
		clear(raw[:n])
		return 0, certificates.ErrACMEConfig
	}
	return n, err
}
func (m *publicCertificateExecution) client(address, rootFile string) (*http.Client, *x509.CertPool, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" || parsed.RawPath != "" || parsed.ForceQuery {
		return nil, nil, certificates.ErrACMEConfig
	}
	roots, err := x509.SystemCertPool()
	if rootFile != "" {
		raw, e := readPrivateDeployment(rootFile)
		if e != nil {
			return nil, nil, e
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(raw) {
			return nil, nil, certificates.ErrACMEConfig
		}
	} else if err != nil {
		return nil, nil, err
	}
	tr := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}}
	m.transports = append(m.transports, tr)
	return &http.Client{Transport: certificateRoundTripper{manager: m, origin: parsed.Scheme + "://" + parsed.Host, transport: tr}, Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return certificates.ErrACMERedirect }}, roots, nil
}

type privateACMEAccountSigner struct {
	manager *publicCertificateExecution
	scope   *publicCertificateScope
}

func (s privateACMEAccountSigner) read(ctx context.Context, ref certificates.KeyRef) (*ecdsa.PrivateKey, error) {
	if string(ref) != "account" || s.manager.current(ctx, s.scope) != nil {
		return nil, certificates.ErrACMESigner
	}
	key, fileSHA, publicSHA, err := loadACMEAccountKey(s.manager.o.AccountKeyFile)
	if err != nil || fileSHA != s.manager.accountSHA || publicSHA != s.manager.accountSPKI {
		return nil, certificates.ErrACMESigner
	}
	return key, nil
}
func (s privateACMEAccountSigner) PublicKey(ctx context.Context, ref certificates.KeyRef) (crypto.PublicKey, error) {
	key, err := s.read(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &key.PublicKey, nil
}
func (s privateACMEAccountSigner) SignDigest(ctx context.Context, ref certificates.KeyRef, digest []byte) ([]byte, error) {
	if len(digest) != sha256.Size {
		return nil, certificates.ErrACMESigner
	}
	key, err := s.read(ctx, ref)
	if err != nil {
		return nil, err
	}
	r, value, err := ecdsa.Sign(rand.Reader, key, digest)
	if err != nil {
		return nil, certificates.ErrACMESigner
	}
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	value.FillBytes(out[32:])
	if s.manager.current(ctx, s.scope) != nil {
		clear(out)
		return nil, certificates.ErrACMESigner
	}
	return out, nil
}

func validPublicCertificateDeployment(o ACMEDeployment) bool {
	return runtime.GOOS == "linux" && os.Geteuid() != 0 && len(o.ForbiddenKeySPKI) >= 2 && o.DNSCredentialRevision > 0
}
