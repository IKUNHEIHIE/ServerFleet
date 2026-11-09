package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/dns"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/notifications"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/servicegroups"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
)

// GatewayArtifactUploader is implemented by the CX file-task composition. The
// task id is mandatory so the uploader can recheck the current unified task,
// approval and plan digest before writing bytes; it must use the same Agent file
// channel and never open SSH or a second execution queue.
type GatewayArtifactUploader interface {
	Upload(ctx context.Context, taskID, nodeID, absolutePath string, content []byte, sha256Hex string) error
}

// CertificateInstallPaths are operator-owned file paths for one certificate.
// The publication bridge never receives private key material.
type CertificateInstallPaths struct {
	CertificatePath string
	PrivateKeyPath  string
}

// GatewayInstallPaths are the private Agent-side nginx roots for one gateway.
// They must match the Agent capability's Prefix/SiteDir/ArtifactDir config.
type GatewayInstallPaths struct {
	SiteDir     string
	ArtifactDir string
	HTTPListen  string
	HTTPSListen string
	ProbeCAFile string
}

// ProductionPublishingConfig binds concrete DS owners to the production
// publishing bridge. Missing dependencies keep that stage closed.
type ProductionPublishingConfig struct {
	Plans          *publishing.Repo
	DNS            *dns.Service
	Certificates   *certificates.Service
	Gateways       *gateway.Service
	Nodes          *nodes.Repo
	ServiceGroups  *servicegroups.Repo
	Tasks          *tasks.Service
	TasksRepo      *tasks.Repo
	DB             *storage.DB
	Artifacts      GatewayArtifactUploader
	NextDispatcher tasks.Dispatcher
	// Dispatcher and Registry reuse the exact gateway dispatcher the task
	// service is already constructed with. When both are supplied the bridge
	// never creates a second dispatcher or a second artifact registry.
	Dispatcher           tasks.Dispatcher
	Registry             *gatewayArtifactRegistry
	Authorizer           auth.Authorizer
	CertificatePath      map[string]CertificateInstallPaths
	NodeCertificateRoots map[string]string
	GatewayPaths         map[string]GatewayInstallPaths
	Now                  func() time.Time
	ApprovalTTL          time.Duration
	ApprovalWait         time.Duration
	ExecutionWait        time.Duration
	Poll                 time.Duration
	OnTaskSubmitted      func(string)
}

// NewProductionPublishingBridge is the controller composition seam. The
// returned LocalBridge holds real DNS/certificate/gateway stages, and the
// dispatcher must wrap the Agent link dispatcher so gateway artifacts are
// uploaded only after the current approval and target revision are rechecked.
func NewProductionPublishingBridge(config ProductionPublishingConfig) (publishing.LocalBridge, tasks.Dispatcher, error) {
	if config.Plans == nil || config.DNS == nil || config.Certificates == nil ||
		config.Gateways == nil || config.Nodes == nil || config.ServiceGroups == nil ||
		config.Tasks == nil || config.TasksRepo == nil || config.DB == nil ||
		config.Artifacts == nil || config.NextDispatcher == nil || config.Authorizer == nil ||
		len(config.GatewayPaths) == 0 {
		return publishing.LocalBridge{}, nil, publishing.ErrProductionUnavailable
	}
	var registry *gatewayArtifactRegistry
	authority := publicationPlanAuthority{repo: config.Plans}
	var dispatcher tasks.Dispatcher
	if config.Dispatcher != nil {
		if config.Registry == nil {
			return publishing.LocalBridge{}, nil, publishing.ErrProductionUnavailable
		}
		if _, ok := config.Dispatcher.(*gatewayTaskDispatcher); !ok {
			return publishing.LocalBridge{}, nil, publishing.ErrProductionUnavailable
		}
		registry = config.Registry
		dispatcher = config.Dispatcher
	} else {
		registry = newGatewayArtifactRegistry()
		created, err := newGatewayTaskDispatcher(gatewayExecutionConfig{
			Repo: config.TasksRepo, DB: config.DB, Plans: config.Plans,
			Artifacts: config.Artifacts, Next: config.NextDispatcher, Authorizer: config.Authorizer,
			Nodes: config.Nodes, Registry: registry, Now: config.Now, ApprovalTTL: config.ApprovalTTL,
		})
		if err != nil {
			return publishing.LocalBridge{}, nil, err
		}
		dispatcher = created
	}
	host, err := newGatewayTaskHost(config.Tasks, gatewayHostConfig{
		Registry: registry, Authority: authority, Poll: config.Poll,
		ApprovalWait: config.ApprovalWait, ExecuteWait: config.ExecutionWait,
		OnSubmitted: config.OnTaskSubmitted, Now: config.Now,
	})
	if err != nil {
		return publishing.LocalBridge{}, nil, err
	}
	roots := map[string]string{}
	for node, root := range config.NodeCertificateRoots {
		if !deploymentID.MatchString(node) || !validCertificatePath(root) {
			return publishing.LocalBridge{}, nil, ErrGatewayDeployment
		}
		roots[node] = root
	}
	proofNow := config.Now
	if proofNow == nil {
		proofNow = time.Now
	}
	nodeCertificates := &nodeCertificatePublication{db: config.DB, tasks: config.TasksRepo, gateways: config.Gateways, authorizer: config.Authorizer, roots: roots, now: proofNow}
	applier := &publicationGatewayApplier{
		gateways: config.Gateways, tasks: host, plans: publicationPlanLookup{repo: config.Plans},
		sitePaths: config.GatewayPaths, nodeCertificates: nodeCertificates,
	}
	bridge, err := publishing.NewProductionBridge(publishing.ProductionBridgeConfig{
		Plans:     publicationPlanLookup{repo: config.Plans},
		DNS:       publicationDNSWriter{service: config.DNS},
		Addresses: publicationGatewayAddresses{gateways: config.Gateways, nodes: config.Nodes},
		Certificates: publicationCertificateInspection{
			service: config.Certificates, paths: config.CertificatePath, nodeCertificates: nodeCertificates,
		},
		Upstreams: publicationServiceUpstreams{groups: config.ServiceGroups, nodes: config.Nodes},
		Gateway:   applier,
	})
	if err != nil {
		return publishing.LocalBridge{}, nil, err
	}
	return bridge, dispatcher, nil
}

type publicationPlanLookup struct{ repo *publishing.Repo }

func (l publicationPlanLookup) GetPlan(ctx context.Context, planID string) (publishing.Plan, error) {
	plan, _, err := l.repo.Get(ctx, planID)
	return plan, err
}

type publicationDNSWriter struct{ service *dns.Service }

func (w publicationDNSWriter) EnsureRecord(ctx context.Context, subjectID, requestID, zoneID, name string, recordType dns.RecordType, content string, ttl int) (string, bool, error) {
	records, err := w.service.ListRecords(ctx, subjectID, zoneID)
	if err != nil {
		return "", false, err
	}
	for _, record := range records {
		if record.Type == recordType && record.Name == name && record.Content == content {
			return record.ID, false, nil
		}
	}
	record, err := w.service.CreateRecord(ctx, subjectID, requestID, zoneID, dns.RecordInput{
		Type: recordType, Name: name, Content: content, TTL: ttl,
	})
	if err != nil {
		return "", false, err
	}
	return record.ID, true, nil
}

func (w publicationDNSWriter) DeleteRecord(ctx context.Context, subjectID, requestID, zoneID, recordID string) error {
	return w.service.DeleteRecord(ctx, subjectID, requestID, recordID)
}

type publicationGatewayAddresses struct {
	gateways *gateway.Service
	nodes    *nodes.Repo
}

func (a publicationGatewayAddresses) Addresses(ctx context.Context, subjectID string, gatewayIDs []string) ([]string, error) {
	addresses := make([]string, 0, len(gatewayIDs))
	for _, gatewayID := range gatewayIDs {
		record, err := a.gateways.GetGateway(ctx, subjectID, gatewayID)
		if err != nil {
			return nil, err
		}
		node, err := a.nodes.Get(ctx, record.NodeID)
		if err != nil {
			return nil, err
		}
		address := strings.TrimSpace(node.Address)
		if address == "" {
			return nil, fmt.Errorf("publishing: gateway %s has no address", gatewayID)
		}
		addresses = append(addresses, address)
	}
	return addresses, nil
}

type publicationCertificateInspection struct {
	nodeCertificates *nodeCertificatePublication
	service          *certificates.Service
	paths            map[string]CertificateInstallPaths
}

func (c publicationCertificateInspection) Inspect(ctx context.Context, subjectID, certificateID, domain string) (string, error) {
	if isNodeCertificateReference(certificateID) {
		return c.nodeCertificates.inspect(ctx, subjectID, certificateID, domain)
	}
	detail, err := c.service.Get(ctx, subjectID, certificateID)
	if err != nil {
		return "", err
	}
	certificate := detail.Certificate
	now := time.Now().UTC()
	if certificate.Status != certificates.StatusActive || now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
		return "", publishing.ErrProductionCertificate
	}
	if !certificateMatchesDomain(certificate.CommonName, domain) {
		return "", publishing.ErrProductionCertificate
	}
	return "serial=" + certificate.Serial + ";not_after=" + certificate.NotAfter.UTC().Format(time.RFC3339), nil
}

func (c publicationCertificateInspection) Paths(ctx context.Context, subject, certificateID string) (string, string, error) {
	if isNodeCertificateReference(certificateID) {
		return c.nodeCertificates.paths(ctx, subject, certificateID, "")
	}
	paths, ok := c.paths[certificateID]
	if !ok || !validCertificatePath(paths.CertificatePath) || !validCertificatePath(paths.PrivateKeyPath) {
		return "", "", publishing.ErrProductionCertificate
	}
	return paths.CertificatePath, paths.PrivateKeyPath, nil
}

type publicationServiceUpstreams struct {
	groups *servicegroups.Repo
	nodes  *nodes.Repo
}

func (u publicationServiceUpstreams) Upstreams(ctx context.Context, _ string, serviceGroupID string) ([]string, error) {
	group, err := u.groups.Get(ctx, serviceGroupID)
	if err != nil {
		return nil, err
	}
	port := group.Service.Port
	if port < 1 || port > 65535 {
		return nil, publishing.ErrProductionTarget
	}
	upstreams := make([]string, 0, len(group.Members))
	for _, member := range group.Members {
		if member.Paused {
			continue
		}
		node, err := u.nodes.Get(ctx, member.NodeID)
		if err != nil {
			return nil, err
		}
		host := strings.TrimSpace(node.Address)
		if parsedHost, _, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
		}
		if host == "" {
			return nil, publishing.ErrProductionTarget
		}
		upstreams = append(upstreams, net.JoinHostPort(host, strconv.Itoa(port)))
	}
	if len(upstreams) == 0 {
		return nil, publishing.ErrProductionTarget
	}
	return upstreams, nil
}

type gatewayPublicationSummary = publishing.GatewayReceipt

type gatewayRouteService interface {
	GetGateway(ctx context.Context, subjectID, gatewayID string) (gateway.GatewayNode, error)
	GetRoute(ctx context.Context, subjectID, routeID string) (gateway.GatewayRoute, error)
	CreateRoute(ctx context.Context, subjectID, requestID, gatewayID string, input gateway.CreateRouteInput) (gateway.GatewayRoute, error)
	UpdateRoute(ctx context.Context, subjectID, requestID, gatewayID, routeID string, expectedRevision uint64, input gateway.UpdateRouteInput) (gateway.GatewayRoute, error)
	DeleteRoute(ctx context.Context, subjectID, requestID, gatewayID, routeID string, expectedRevision uint64) error
}

type gatewayTaskRunner interface {
	apply(ctx context.Context, plan publishing.Plan, site gateway.NginxSite, input gateway.NginxTaskInput) (string, error)
	remove(ctx context.Context, plan publishing.Plan, site gateway.NginxSite, input gateway.NginxTaskInput) (string, error)
}

type publicationGatewayApplier struct {
	nodeCertificates *nodeCertificatePublication
	gateways         gatewayRouteService
	tasks            gatewayTaskRunner
	plans            publishing.PlanLookup
	sitePaths        map[string]GatewayInstallPaths
}

func (a *publicationGatewayApplier) Apply(ctx context.Context, plan publishing.Plan, request publishing.GatewayRequest, upstreams []string, certificatePath, keyPath string) (string, error) {
	if a == nil || a.gateways == nil || a.tasks == nil || a.plans == nil {
		return "", publishing.ErrProductionUnavailable
	}
	if len(request.GatewayIDs) != 1 {
		return "", publishing.ErrProductionTarget
	}
	current, err := a.currentPlan(ctx, plan)
	if err != nil {
		return "", err
	}
	plan = current
	gatewayID := request.GatewayIDs[0]
	record, err := a.gateways.GetGateway(ctx, request.SubjectID, gatewayID)
	if err != nil {
		return "", err
	}
	expectedLeaf := ""
	if request.HTTPS && isNodeCertificateReference(request.CertificateID) {
		cp, kp, e := a.nodeCertificates.paths(ctx, request.SubjectID, request.CertificateID, gatewayID)
		if e != nil || cp != certificatePath || kp != keyPath {
			return "", publishing.ErrProductionCertificate
		}
		proof, e := a.nodeCertificates.proofForGateway(ctx, request.SubjectID, request.CertificateID, gatewayID)
		if e != nil {
			return "", e
		}
		expectedLeaf = proof.Result.LeafSHA256
	}
	sitePaths, err := a.pathsFor(gatewayID)
	if err != nil {
		return "", err
	}
	routeID := plan.ID
	if len(plan.GatewayIDs) > 1 {
		h := sha256.Sum256([]byte(plan.ID + "\x00" + gatewayID))
		routeID = "pub-route-" + hex.EncodeToString(h[:16])
	}
	route, err := a.gateways.GetRoute(ctx, request.SubjectID, routeID)
	created := false
	switch {
	case errors.Is(err, gateway.ErrRouteNotFound):
		route, err = a.gateways.CreateRoute(ctx, request.SubjectID, request.RequestID, gatewayID, gateway.CreateRouteInput{
			ID: routeID, Domain: request.Domain, ServiceGroupID: request.ServiceGroupID,
			HTTPS: request.HTTPS, Enabled: false,
		})
		created = true
	case err != nil:
		return "", err
	case route.GatewayID != gatewayID || route.Domain != request.Domain ||
		route.ServiceGroupID != request.ServiceGroupID || route.HTTPS != request.HTTPS:
		route, err = a.gateways.UpdateRoute(ctx, request.SubjectID, request.RequestID, gatewayID, routeID, route.Revision, gateway.UpdateRouteInput{
			Domain: request.Domain, ServiceGroupID: request.ServiceGroupID,
			HTTPS: request.HTTPS, Enabled: false,
		})
	}
	if err != nil {
		return "", err
	}
	site, err := gateway.RenderNginxSite(gateway.NginxSiteRequest{
		ProbeToken: gatewayPublicationProbeToken(plan.ID, plan.TargetRevision, gatewayID), GatewayID: gatewayID, Domain: request.Domain, ServiceGroupID: request.ServiceGroupID,
		ServiceGroupRevision: request.ExpectedGroupRevision, UpstreamHosts: upstreams,
		HTTPS: request.HTTPS, CertificatePath: certificatePath, PrivateKeyPath: keyPath,
		SiteDir: sitePaths.SiteDir, ArtifactDir: sitePaths.ArtifactDir,
		HTTPListen: sitePaths.HTTPListen, HTTPSListen: sitePaths.HTTPSListen,
	})
	if err != nil {
		return gatewayPublicationState(gatewayPublicationSummary{
			RouteID: routeID, GatewayID: gatewayID, NodeID: record.NodeID,
			RouteRevision: route.Revision, RouteCreated: created,
			PlanID: plan.ID, PlanTargetRevision: plan.TargetRevision,
		}), err
	}
	partial := gatewayPublicationSummary{
		RouteID: routeID, GatewayID: gatewayID, NodeID: record.NodeID,
		SitePath: site.Path, ConfigSHA256: site.Digest,
		RouteRevision: route.Revision, RouteCreated: created,
		PlanID: plan.ID, PlanTargetRevision: plan.TargetRevision,
	}
	taskID, err := a.tasks.apply(ctx, plan, site, gateway.NginxTaskInput{
		SubjectID: request.SubjectID, RequestID: request.RequestID, GatewayID: gatewayID,
		NodeID: record.NodeID, IdempotencyKey: plan.ID,
	})
	if err != nil {
		// The route and staged artifact may exist even when the task outcome is
		// failed/unknown. Preserve their identities so rollback can remove only
		// what this plan created.
		return gatewayPublicationState(partial), err
	}
	partial.TaskID = taskID
	partial.ProbeToken = site.ProbeToken
	receipt := &gatewayRouteReceipt{SubjectID: request.SubjectID, RequestID: request.RequestID, TaskID: taskID, PlanID: plan.ID, TargetRevision: plan.TargetRevision, GatewayID: gatewayID, NodeID: record.NodeID, Domain: request.Domain, GroupID: request.ServiceGroupID, GroupRevision: request.ExpectedGroupRevision, HTTPS: request.HTTPS, Site: site, ExpectedLeafSHA256: expectedLeaf}
	checked := context.WithValue(ctx, gatewayRouteReceiptKey{}, receipt)
	// The route becomes enabled only after the exact approved nginx task has
	// completed AND the active site returns its frozen plan-bound probe token.
	route, err = a.gateways.UpdateRoute(checked, request.SubjectID, request.RequestID, gatewayID, routeID, route.Revision, gateway.UpdateRouteInput{Domain: request.Domain, ServiceGroupID: request.ServiceGroupID, HTTPS: request.HTTPS, Enabled: true})
	if err != nil {
		return gatewayPublicationState(partial), err
	}
	partial.RouteRevision = route.Revision
	partial.HTTPStatus = receipt.HTTPStatus
	partial.ResponseSHA256 = receipt.ResponseSHA256
	partial.ResponseBytes = receipt.ResponseBytes
	return gatewayPublicationState(partial), nil
}

func gatewayPublicationState(summary gatewayPublicationSummary) string {
	raw, err := json.Marshal(summary)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (a *publicationGatewayApplier) pathsFor(gatewayID string) (GatewayInstallPaths, error) {
	if len(a.sitePaths) == 0 {
		// Direct unit tests and non-production seam use the renderer defaults.
		return GatewayInstallPaths{}, nil
	}
	paths, ok := a.sitePaths[gatewayID]
	if !ok || paths.SiteDir == "" || paths.ArtifactDir == "" {
		return GatewayInstallPaths{}, publishing.ErrProductionUnavailable
	}
	return paths, nil
}

func (a *publicationGatewayApplier) currentPlan(ctx context.Context, plan publishing.Plan) (publishing.Plan, error) {
	current, err := a.plans.GetPlan(ctx, plan.ID)
	if err != nil || !current.TargetsPersisted || current.TargetRevision != plan.TargetRevision ||
		current.ZoneID != plan.ZoneID || current.CertificateID != plan.CertificateID ||
		current.HTTPS != plan.HTTPS || current.ServiceGroupID != plan.ServiceGroupID ||
		!strings.EqualFold(current.Domain, plan.Domain) || !sameGatewayIDs(current.GatewayIDs, plan.GatewayIDs) {
		return publishing.Plan{}, publishing.ErrProductionTarget
	}
	return current, nil
}

func sameGatewayIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, id := range left {
		seen[id]++
	}
	for _, id := range right {
		seen[id]--
		if seen[id] < 0 {
			return false
		}
	}
	return true
}

func (a *publicationGatewayApplier) Rollback(ctx context.Context, plan publishing.Plan, summary string) error {
	if a == nil || a.gateways == nil || a.tasks == nil || a.plans == nil {
		return publishing.ErrProductionUnavailable
	}
	var state gatewayPublicationSummary
	if len(summary) == 0 || len(summary) > 4096 || json.Unmarshal([]byte(summary), &state) != nil {
		return publishing.ErrProductionTarget
	}
	if state.PlanID != plan.ID || state.PlanTargetRevision != plan.TargetRevision ||
		state.GatewayID == "" || state.NodeID == "" ||
		(state.SitePath == "") != (state.ConfigSHA256 == "") ||
		state.SitePath != "" && !validGatewayDigest(state.ConfigSHA256) ||
		state.RouteCreated && (state.RouteID == "" || state.RouteRevision == 0) {
		return publishing.ErrProductionTarget
	}
	member := false
	for _, id := range plan.GatewayIDs {
		if id == state.GatewayID {
			member = true
		}
	}
	if !member {
		return publishing.ErrProductionTarget
	}
	if _, err := a.currentPlan(ctx, plan); err != nil {
		return err
	}
	if state.SitePath != "" {
		site := gateway.NginxSite{GatewayID: state.GatewayID, Domain: plan.Domain, Path: state.SitePath, Digest: state.ConfigSHA256}
		if _, err := a.tasks.remove(ctx, plan, site, gateway.NginxTaskInput{
			SubjectID: plan.CreatedBy, RequestID: plan.ID, GatewayID: state.GatewayID,
			NodeID: state.NodeID, IdempotencyKey: plan.ID,
		}); err != nil {
			return err
		}
	}
	if !state.RouteCreated {
		return nil
	}
	route, err := a.gateways.GetRoute(ctx, plan.CreatedBy, state.RouteID)
	if errors.Is(err, gateway.ErrRouteNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if route.Revision != state.RouteRevision {
		return publishing.ErrProductionTarget
	}
	return a.gateways.DeleteRoute(ctx, plan.CreatedBy, plan.ID, state.GatewayID, state.RouteID, route.Revision)
}

func certificateMatchesDomain(commonName, domain string) bool {
	if strings.EqualFold(commonName, domain) {
		return true
	}
	if strings.HasPrefix(commonName, "*.") && strings.HasSuffix(strings.ToLower(domain), strings.ToLower(commonName[1:])) {
		prefix := strings.TrimSuffix(strings.ToLower(domain), strings.ToLower(commonName[1:]))
		prefix = strings.TrimSuffix(prefix, ".")
		return prefix != "" && !strings.Contains(prefix, ".")
	}
	return false
}

func validCertificatePath(path string) bool {
	return strings.HasPrefix(path, "/") && len(path) <= 1024 && !strings.ContainsAny(path, "\\\x00") &&
		!strings.Contains(path, "..") && strings.TrimSpace(path) == path
}

// ProviderCredential is the current owner/ref revision/purpose snapshot for one
// opaque provider reference. It contains no secret value.
type ProviderCredential struct {
	CredentialID string
	Owner        string
	Module       string
	Purpose      string
	Revision     uint64
}

// ProviderCredentialLookup maps a DS account/channel credential_ref to current
// credential metadata. A missing or revoked reference must return false, not an
// empty credential.
type ProviderCredentialLookup interface {
	LookupProviderCredential(ctx context.Context, ref string) (ProviderCredential, bool, error)
}

// CredentialUseService is the current owner/revision-bound secret use port. The
// auth owner reauthorizes before and after provider I/O and rejects replay.
type CredentialUseService interface {
	UseForService(ctx context.Context, operation auth.CredentialOperation, consume func(context.Context, []byte) error) error
}

type providerSecretBorrower struct {
	credentials CredentialUseService
	lookup      ProviderCredentialLookup
}

func newProviderSecretBorrower(credentials CredentialUseService, lookup ProviderCredentialLookup) (*providerSecretBorrower, error) {
	if credentials == nil || lookup == nil {
		return nil, publishing.ErrProductionUnavailable
	}
	return &providerSecretBorrower{credentials: credentials, lookup: lookup}, nil
}

func (b *providerSecretBorrower) withSecret(ctx context.Context, ref, operation, module, purpose, planDigest, subjectID, requestID string, fn func(context.Context, string) error) error {
	if b == nil || b.credentials == nil || b.lookup == nil || ref == "" || operation == "" ||
		module == "" || purpose == "" || planDigest == "" || fn == nil {
		return publishing.ErrProductionUnavailable
	}
	meta, ok, err := b.lookup.LookupProviderCredential(ctx, ref)
	if err != nil || !ok || meta.CredentialID == "" || meta.Owner == "" || meta.Revision == 0 ||
		meta.Module != module || meta.Purpose != purpose {
		return publishing.ErrProductionUnavailable
	}
	operationID, err := newProviderOperationID(operation)
	if err != nil {
		return publishing.ErrProductionUnavailable
	}
	op := auth.CredentialOperation{
		SubjectID: meta.Owner, CredentialID: meta.CredentialID, Module: meta.Module,
		Source: "system", OperationID: operationID, PlanDigest: planDigest,
		Action: "use", ExpectedRevision: meta.Revision,
	}
	return b.credentials.UseForService(ctx, op, func(borrowCtx context.Context, secret []byte) error {
		defer clear(secret)
		return fn(borrowCtx, string(secret))
	})
}

// NewDNSPublisherSecretBorrower exposes the callback-scoped DNS provider port
// to the controller composition. The lookup maps account credential_ref values
// to current owner/revision/module metadata; it never returns a token.
func NewDNSPublisherSecretBorrower(credentials CredentialUseService, lookup ProviderCredentialLookup) (dns.SecretBorrower, error) {
	borrower, err := newProviderSecretBorrower(credentials, lookup)
	if err != nil {
		return nil, err
	}
	return dnsSecretBorrower{borrower: borrower}, nil
}

// NewNotificationSecretBorrower exposes the callback-scoped notification
// provider port. SMTP and QQ channel secretRefs must resolve through lookup.
func NewNotificationSecretBorrower(credentials CredentialUseService, lookup ProviderCredentialLookup) (notifications.SecretBorrower, error) {
	borrower, err := newProviderSecretBorrower(credentials, lookup)
	if err != nil {
		return nil, err
	}
	return notificationsSecretBorrower{borrower: borrower}, nil
}

type dnsSecretBorrower struct{ borrower *providerSecretBorrower }

func (b dnsSecretBorrower) WithSecret(ctx context.Context, use dns.SecretUse, fn func(context.Context, string) error) error {
	if b.borrower == nil {
		return publishing.ErrProductionUnavailable
	}
	return b.borrower.withSecret(ctx, use.Ref, use.Operation, use.Module, use.Purpose, use.PlanDigest, use.SubjectID, use.RequestID, fn)
}

type notificationsSecretBorrower struct{ borrower *providerSecretBorrower }

func (b notificationsSecretBorrower) WithSecret(ctx context.Context, use notifications.SecretUse, fn func(context.Context, string) error) error {
	if b.borrower == nil {
		return publishing.ErrProductionUnavailable
	}
	return b.borrower.withSecret(ctx, use.Ref, use.Operation, use.Module, use.Purpose, use.PlanDigest, use.SubjectID, use.RequestID, fn)
}

func newProviderOperationID(operation string) (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return operation + "-" + hex.EncodeToString(buffer), nil
}

// publicationCleanupCleaner deletes only a record that still matches the
// journal-owned provider ID, FQDN and expected TXT value.
type publicationCleanupCleaner struct{ dns *dns.Service }

// NewPublicationCleanupCleaner is the controller composition seam for durable
// ACME cleanup. It never deletes a record that does not match all three fields.
func NewPublicationCleanupCleaner(service *dns.Service) (certificates.DNSRecordCleaner, error) {
	if service == nil {
		return nil, certificates.ErrCleanupUnavailable
	}
	return publicationCleanupCleaner{dns: service}, nil
}

func (c publicationCleanupCleaner) DeleteChallengeRecord(ctx context.Context, subjectID, requestID, fqdn, recordID, expectedValue string) error {
	if c.dns == nil || fqdn == "" || recordID == "" || expectedValue == "" {
		return errors.Join(certificates.ErrCleanupRetryable, certificates.ErrCleanupUnavailable)
	}
	records, err := c.dns.ListRecords(ctx, subjectID, "")
	if err != nil {
		// Read-only failure occurred before any delete could be issued.
		return errors.Join(certificates.ErrCleanupRetryable, err)
	}
	matches := 0
	for _, record := range records {
		if record.ID == recordID && record.Name == fqdn && record.Content == expectedValue && string(record.Type) == "TXT" {
			matches++
		}
	}
	if matches != 1 {
		return certificates.ErrCleanupNotFound
	}
	err = c.dns.DeleteRecord(ctx, subjectID, requestID, recordID)
	if err == nil {
		return nil
	}
	// Only the DNS service's explicit definite failure is safe to retry. A
	// timeout, transport uncertainty or local commit failure after remote I/O
	// remains unknown; no automatic deletion of a re-created TXT record.
	if errors.Is(err, dns.ErrProviderOutcomeUnknown) {
		return errors.Join(certificates.ErrCleanupUnknown, err)
	}
	if errors.Is(err, dns.ErrProviderFailed) {
		return errors.Join(certificates.ErrCleanupRetryable, err)
	}
	return errors.Join(certificates.ErrCleanupUnknown, err)
}

func (c publicationCertificateInspection) PathsForGateway(ctx context.Context, subject, certificateID, gatewayID string) (string, string, error) {
	if isNodeCertificateReference(certificateID) {
		return c.nodeCertificates.paths(ctx, subject, certificateID, gatewayID)
	}
	return c.Paths(ctx, subject, certificateID)
}
