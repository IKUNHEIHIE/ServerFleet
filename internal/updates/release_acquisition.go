package updates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

const ActionAcquireRelease = "system.updates.acquire"

// HTTPReleaseFeed contains only operator-configured public endpoints and trust.
// No URL/IP/CA/credentials can come from HTTP, AI, plugin or task parameters.
type HTTPReleaseFeed struct {
	Origin    string   `json:"origin"`
	Addresses []string `json:"addresses"`
	RootPEM   []byte   `json:"-"`
}
type ReleaseAcquisitionOptions struct {
	DB                                           *storage.DB
	Audit                                        InstallAuditSink
	Inbox, StageDir, KeyID, Version, AuthorityID string
	Feed                                         HTTPReleaseFeed
	Policy                                       ReleasePolicy
	Current                                      func(context.Context, string, string) (tasks.Task, error)
	Administrator                                func(context.Context, string) error
	RuntimeCurrent                               func(context.Context) error
}
type ReleaseAcquirer struct {
	o         ReleaseAcquisitionOptions
	sourceSHA string
	origin    *url.URL
	client    *http.Client
	transport *http.Transport
	root      *os.Root
	stager    *ArtifactStager
}

func NewReleaseAcquirer(o ReleaseAcquisitionOptions) (*ReleaseAcquirer, error) {
	if o.DB == nil || o.Audit == nil || !healthDirectoryPrivate(o.Inbox) || !healthDirectoryPrivate(o.StageDir) || !installID(o.AuthorityID) || !installID(o.KeyID) || !versionPattern.MatchString(o.Version) || o.Current == nil || o.Administrator == nil || o.RuntimeCurrent == nil || o.Policy.Component != "agent" || o.Policy.Architecture != "amd64" && o.Policy.Architecture != "arm64" || len(o.Policy.TrustedKeys[o.KeyID]) != 32 || o.Policy.MaxBytes <= 0 || o.Policy.MaxBytes > 256<<20 {
		return nil, ErrArtifactRejected
	}
	origin, err := url.Parse(o.Feed.Origin)
	if err != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.User != nil || origin.Opaque != "" || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" || origin.RawPath != "" || origin.ForceQuery || len(o.Feed.Addresses) == 0 || len(o.Feed.Addresses) > 8 {
		return nil, ErrArtifactRejected
	}
	port := origin.Port()
	if port == "" {
		port = "443"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number <= 0 || number > 65535 {
		return nil, ErrArtifactRejected
	}
	o.Feed.Addresses = append([]string{}, o.Feed.Addresses...)
	sort.Strings(o.Feed.Addresses)
	for i, address := range o.Feed.Addresses {
		ip := net.ParseIP(address)
		if ip == nil || ip.String() != address || ip.IsUnspecified() || ip.IsMulticast() || i > 0 && address == o.Feed.Addresses[i-1] {
			return nil, ErrArtifactRejected
		}
	}
	var roots *x509.CertPool
	if len(o.Feed.RootPEM) > 0 {
		if len(o.Feed.RootPEM) > 128<<10 {
			return nil, ErrArtifactRejected
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(o.Feed.RootPEM) {
			return nil, ErrArtifactRejected
		}
	}
	root, err := os.OpenRoot(o.Inbox)
	if err != nil {
		return nil, ErrArtifactRejected
	}
	stager, err := NewArtifactStager(o.StageDir)
	if err != nil {
		root.Close()
		return nil, err
	}
	o.Policy = cloneReleasePolicy(o.Policy)
	o.Feed.RootPEM = bytes.Clone(o.Feed.RootPEM)
	raw, _ := json.Marshal(struct {
		Origin                      string
		Addresses                   []string
		RootSHA                     string
		KeyID, Version, AuthorityID string
		Policy                      ReleasePolicy
	}{o.Feed.Origin, o.Feed.Addresses, releaseDigest(o.Feed.RootPEM), o.KeyID, o.Version, o.AuthorityID, o.Policy})
	tr := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: origin.Hostname()}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != net.JoinHostPort(origin.Hostname(), port) {
			return nil, ErrArtifactRejected
		}
		var last error
		for _, ip := range o.Feed.Addresses {
			conn, e := (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1}).DialContext(ctx, "tcp", net.JoinHostPort(ip, port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}}
	if _, err = o.DB.Exec(context.Background(), `UPDATE cx_release_acquisitions SET status='unknown' WHERE status='publishing'`); err != nil {
		stager.Close()
		root.Close()
		tr.CloseIdleConnections()
		return nil, err
	}
	return &ReleaseAcquirer{o: o, sourceSHA: releaseDigest(raw), origin: origin, transport: tr, root: root, stager: stager, client: &http.Client{Transport: tr, Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrArtifactRejected }}}, nil
}
func releaseDigest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func (a *ReleaseAcquirer) Close() error {
	if a == nil {
		return nil
	}
	a.transport.CloseIdleConnections()
	a.stager.Close()
	return a.root.Close()
}
func (a *ReleaseAcquirer) Metadata() (string, string, string) {
	return a.o.Version, a.sourceSHA, a.origin.Hostname()
}
func (a *ReleaseAcquirer) TaskRequest(subject, request, key string) (tasks.SubmitRequest, error) {
	if a == nil || !installID(subject) || !installID(request) || !installID(key) {
		return tasks.SubmitRequest{}, ErrArtifactRejected
	}
	return tasks.SubmitRequest{SubjectID: subject, RequestID: request, Name: "Acquire signed Agent release", ActionID: ActionAcquireRelease, OperationClass: tasks.OperationClassStandard, Source: audit.SourceWeb, Params: map[string]string{"version": a.o.Version, "sourceSha256": a.sourceSHA}, IdempotencyKey: "release-acquire-" + releaseDigest([]byte(key))}, nil
}
func (a *ReleaseAcquirer) current(ctx context.Context, t tasks.Task) error {
	if ctx == nil || ctx.Err() != nil || a.o.RuntimeCurrent(ctx) != nil || a.o.Administrator(ctx, t.SubmitterID) != nil {
		return ErrArtifactRejected
	}
	current, err := a.o.Current(ctx, t.ID, t.PlanHash)
	if err != nil || current.ActionID != ActionAcquireRelease || current.TargetType != tasks.TargetTypeController || current.TargetResourceID != a.o.AuthorityID || current.Source != string(audit.SourceWeb) || current.Params != t.Params {
		return ErrArtifactRejected
	}
	return nil
}
func (a *ReleaseAcquirer) floor(ctx context.Context) (ReleasePolicy, error) {
	var sequence uint64
	if a.o.DB.QueryRow(ctx, `SELECT sequence_floor FROM cx_update_installed WHERE component='agent'`).Scan(&sequence) != nil {
		return ReleasePolicy{}, ErrArtifactRejected
	}
	policy := cloneReleasePolicy(a.o.Policy)
	if sequence > policy.MinimumSequence {
		policy.MinimumSequence = sequence
	}
	return policy, nil
}

type acquisitionReader struct {
	reader io.Reader
	gate   func() error
}

func (r acquisitionReader) Read(p []byte) (int, error) {
	if err := r.gate(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func (a *ReleaseAcquirer) get(ctx context.Context, t tasks.Task, name string, max int64) (io.ReadCloser, error) {
	if a.current(ctx, t) != nil {
		return nil, ErrArtifactRejected
	}
	target := *a.origin
	target.Path = "/" + a.o.Policy.Component + "/" + a.o.Policy.Architecture + "/" + a.o.Version + "/" + name
	target.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, ErrArtifactRejected
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := a.client.Do(request)
	if err != nil {
		return nil, ErrArtifactRejected
	}
	if response.StatusCode != 200 || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" || response.ContentLength > max || a.current(ctx, t) != nil {
		response.Body.Close()
		return nil, ErrArtifactRejected
	}
	return response.Body, nil
}
func (a *ReleaseAcquirer) Execute(ctx context.Context, t tasks.Task) (map[string]tasks.WorkerResult, error) {
	result := func(code tasks.NodeResultCode, message string) (map[string]tasks.WorkerResult, error) {
		return map[string]tasks.WorkerResult{t.TargetResourceID: {Code: code, Message: message}}, nil
	}
	var params map[string]string
	if a == nil || a.current(ctx, t) != nil || t.ActionID != ActionAcquireRelease || t.OperationClass != tasks.OperationClassStandard || len(t.Targets) != 1 || t.Targets[0].NodeID != a.o.AuthorityID || json.Unmarshal([]byte(t.Params), &params) != nil || len(params) != 2 || params["version"] != a.o.Version || params["sourceSha256"] != a.sourceSHA {
		return result(tasks.NodeResultFailed, "release acquisition binding rejected")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	read := func(name string, max int64) ([]byte, error) {
		body, err := a.get(ctx, t, name, max)
		if err != nil {
			return nil, err
		}
		defer body.Close()
		raw, err := io.ReadAll(io.LimitReader(acquisitionReader{body, func() error { return a.current(ctx, t) }}, max+1))
		if err != nil || int64(len(raw)) > max {
			return nil, ErrArtifactRejected
		}
		return raw, nil
	}
	raw, err := read("manifest.json", 16<<10)
	if err != nil {
		return result(tasks.NodeResultFailed, "signed release metadata unavailable")
	}
	signature, err := read("signature.bin", 64)
	if err != nil {
		return result(tasks.NodeResultFailed, "release signature unavailable")
	}
	policy, err := a.floor(ctx)
	if err != nil {
		return result(tasks.NodeResultFailed, "installed release provenance unavailable")
	}
	manifest, err := VerifyManifest(ctx, raw, signature, a.o.KeyID, policy)
	if err != nil || manifest.Version != a.o.Version {
		return result(tasks.NodeResultFailed, "signed release policy rejected")
	}
	body, err := a.get(ctx, t, "artifact", policy.MaxBytes)
	if err != nil {
		return result(tasks.NodeResultFailed, "release bytes unavailable")
	}
	staged, err := a.stager.Stage(ctx, raw, signature, a.o.KeyID, acquisitionReader{body, func() error { return a.current(ctx, t) }}, policy)
	body.Close()
	if err != nil {
		return result(tasks.NodeResultFailed, "release bytes not verified")
	}
	defer staged.Close()
	// Reserve the version exclusively. Existing or partial versions are never
	// overwritten or resumed; a lost result requires explicit reconciliation.
	if a.current(ctx, t) != nil {
		return result(tasks.NodeResultFailed, "release authorization changed")
	}
	now := time.Now().UTC()
	err = a.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, e := tx.Exec(txCtx, `INSERT INTO cx_release_acquisitions(task_id,version,source_sha256,artifact_sha256,status,created_at) VALUES(?,?,?,?,'publishing',?)`, t.ID, manifest.Version, a.sourceSHA, manifest.SHA256, storage.EncodeTime(now)); e != nil {
			return e
		}
		return a.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: t.SubmitterID, Action: ActionAcquireRelease, Result: "allowed", RequestID: t.RequestID, TaskID: t.ID, Source: audit.SourceWeb, ReasonCode: "signed_release_acquisition_reserved", At: now})
	})
	if err != nil {
		return result(tasks.NodeResultUnknown, "release receipt reservation unknown; no automatic retry")
	}
	if err = a.root.Mkdir(manifest.Version, 0700); err != nil {
		a.unknown(t)
		return result(tasks.NodeResultUnknown, "release version already exists or reservation unknown")
	}
	write := func(name string, input io.Reader, size int64) error {
		if a.current(ctx, t) != nil {
			return ErrArtifactRejected
		}
		file, e := a.root.OpenFile(manifest.Version+"/"+name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return ErrArtifactRejected
		}
		n, e := io.CopyN(file, acquisitionReader{input, func() error { return a.current(ctx, t) }}, size)
		syncErr := file.Sync()
		closeErr := file.Close()
		if e != nil || syncErr != nil || closeErr != nil || n != size {
			return ErrApplyUnknown
		}
		return nil
	}
	gate := func(ctx context.Context, m ReleaseManifest) error {
		if a.current(ctx, t) != nil || m != manifest {
			return ErrArtifactRejected
		}
		current, e := a.floor(ctx)
		if e != nil || m.Sequence <= current.MinimumSequence {
			return ErrArtifactRejected
		}
		return nil
	}
	err = staged.WithVerified(ctx, policy, gate, func(ctx context.Context, m ReleaseManifest, input io.Reader) error {
		return write("artifact", input, m.Bytes)
	})
	if err == nil {
		err = write("signature.bin", bytes.NewReader(signature), int64(len(signature)))
	}
	// Manifest is published last; partial acquisition cannot be consumed as an
	// authenticated complete version by DirectoryReleaseSource.
	if err == nil && gate(ctx, manifest) == nil {
		err = write("manifest.json", bytes.NewReader(raw), int64(len(raw)))
	} else if err == nil {
		err = ErrApplyUnknown
	}
	if err == nil {
		err = syncStagingDirectory(a.root, manifest.Version)
	}
	if err == nil {
		err = syncStagingDirectory(a.root, ".")
	}
	if err != nil {
		a.unknown(t)
		return result(tasks.NodeResultUnknown, "release publication outcome unknown; no automatic retry")
	}
	err = a.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		changed, e := tx.Exec(txCtx, `UPDATE cx_release_acquisitions SET status='ready' WHERE task_id=? AND status='publishing'`, t.ID)
		if e != nil {
			return e
		}
		n, e := changed.RowsAffected()
		if e != nil || n != 1 {
			return ErrApplyUnknown
		}
		return a.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: t.SubmitterID, Action: ActionAcquireRelease, Result: "success", RequestID: t.RequestID, TaskID: t.ID, Source: audit.SourceWeb, ReasonCode: "signed_release_acquired_not_installed", At: time.Now().UTC()})
	})
	if err != nil {
		a.unknown(t)
		return result(tasks.NodeResultUnknown, "release published but receipt commit unknown")
	}
	return result(tasks.NodeResultSucceeded, "signed release acquired; not installed")
}
func (a *ReleaseAcquirer) unknown(t tasks.Task) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.o.DB.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := tx.Exec(txCtx, `UPDATE cx_release_acquisitions SET status='unknown' WHERE task_id=? AND status='publishing'`, t.ID); err != nil {
			return err
		}
		return a.o.Audit.AppendTx(txCtx, tx, audit.Event{SubjectID: t.SubmitterID, Action: ActionAcquireRelease, Result: "failure", RequestID: t.RequestID, TaskID: t.ID, Source: audit.SourceWeb, ReasonCode: "release_acquisition_unknown_no_replay", At: time.Now().UTC()})
	})
}
func AcquisitionMigrations() []storage.Migration {
	return []storage.Migration{{Version: "cx/0014_release_acquisition", Name: "signed release acquisition task receipts", Statements: []string{`CREATE TABLE cx_release_acquisitions(task_id TEXT PRIMARY KEY,version TEXT NOT NULL,source_sha256 TEXT NOT NULL,artifact_sha256 TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL)`}}}
}
