package server

import (
	"context"
	"crypto/rand"
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

// GatewayArtifactUploader is implemented by the CX file-task composition. It
// must upload bytes only under the accepted task authorization; a nil uploader
// keeps the gateway stage closed.
type GatewayArtifactUploader interface {
	Upload(ctx context.Context, nodeID, absolutePath string, content []byte, sha256Hex string) error
}

// CertificateInstallPaths are operator-owned file paths for one certificate.
// The publication bridge never receives private key material.
type CertificateInstallPaths struct {
	CertificatePath string
	PrivateKeyPath  string
}

// ProductionPublishingConfig binds concrete DS owners to the production
// publishing bridge. Missing dependencies keep that stage closed.
type ProductionPublishingConfig struct {
	Plans           *publishing.Repo
	DNS             *dns.Service
	Certificates    *certificates.Service
	Gateways        *gateway.Service
	Nodes           *nodes.Repo
	ServiceGroups   *servicegroups.Repo
	Tasks           *tasks.Service
	TasksRepo       *tasks.Repo
	DB              *storage.DB
	Artifacts       GatewayArtifactUploader
	NextDispatcher  tasks.Dispatcher
	Authorizer      auth.Authorizer
	CertificatePath map[string]CertificateInstallPaths
	Now             func() time.Time
	ApprovalTTL     time.Duration
	ApprovalWait    time.Duration
	ExecutionWait   time.Duration
	Poll            time.Duration
	OnTaskSubmitted func(string)
}

// NewProductionPublishingBridge is the controller composition seam. The
// returned LocalBridge holds real DNS/certificate/gateway stages, and the
// dispatcher must wrap the Agent link dispatcher so gateway artifacts are
// uploaded only after the current approval and target revision are rechecked.
func NewProductionPublishingBridge(config ProductionPublishingConfig) (publishing.LocalBridge, tasks.Dispatcher, error) {
	if config.Plans == nil || config.DNS == nil || config.Certificates == nil ||
		config.Gateways == nil || config.Nodes == nil || config.ServiceGroups == nil ||
		config.Tasks == nil || config.TasksRepo == nil || config.DB == nil ||
		config.Artifacts == nil || config.NextDispatcher == nil || config.Authorizer == nil {
		return publishing.LocalBridge{}, nil, publishing.ErrProductionUnavailable
	}
	registry := newGatewayArtifactRegistry()
	authority := publicationPlanAuthority{repo: config.Plans}
	dispatcher, err := newGatewayTaskDispatcher(gatewayExecutionConfig{
		Repo: config.TasksRepo, DB: config.DB, Plans: config.Plans,
		Artifacts: config.Artifacts, Next: config.NextDispatcher, Authorizer: config.Authorizer,
		Nodes: config.Nodes, Registry: registry, Now: config.Now, ApprovalTTL: config.ApprovalTTL,
	})
	if err != nil {
		return publishing.LocalBridge{}, nil, err
	}
	host, err := newGatewayTaskHost(config.Tasks, gatewayHostConfig{
		Registry: registry, Authority: authority, Poll: config.Poll,
		ApprovalWait: config.ApprovalWait, ExecuteWait: config.ExecutionWait,
		OnSubmitted: config.OnTaskSubmitted, Now: config.Now,
	})
	if err != nil {
		return publishing.LocalBridge{}, nil, err
	}
	applier := &publicationGatewayApplier{gateways: config.Gateways, tasks: host, plans: config.Plans}
	bridge, err := publishing.NewProductionBridge(publishing.ProductionBridgeConfig{
		Plans:     publicationPlanLookup{repo: config.Plans},
		DNS:       publicationDNSWriter{service: config.DNS},
		Addresses: publicationGatewayAddresses{gateways: config.Gateways, nodes: config.Nodes},
		Certificates: publicationCertificateInspection{
			service: config.Certificates, paths: config.CertificatePath,
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
	service *certificates.Service
	paths   map[string]CertificateInstallPaths
}

func (c publicationCertificateInspection) Inspect(ctx context.Context, subjectID, certificateID, domain string) (string, error) {
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

func (c publicationCertificateInspection) Paths(_ context.Context, _, certificateID string) (string, string, error) {
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

type gatewayPublicationSummary struct {
	RouteID            string `json:"routeId"`
	GatewayID          string `json:"gatewayId"`
	NodeID             string `json:"nodeId"`
	SitePath           string `json:"sitePath"`
	ConfigSHA256       string `json:"configSha256"`
	TaskID             string `json:"taskId"`
	RouteRevision      uint64 `json:"routeRevision"`
	RouteCreated       bool   `json:"routeCreated"`
	PlanID             string `json:"planId"`
	PlanTargetRevision uint64 `json:"planTargetRevision"`
}

type publicationGatewayApplier struct {
	gateways *gateway.Service
	tasks    *gatewayTaskHost
	plans    *publishing.Repo
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
	routeID := plan.ID
	route, err := a.gateways.GetRoute(ctx, request.SubjectID, routeID)
	created := false
	switch {
	case errors.Is(err, gateway.ErrRouteNotFound):
		route, err = a.gateways.CreateRoute(ctx, request.SubjectID, request.RequestID, gatewayID, gateway.CreateRouteInput{
			ID: routeID, Domain: request.Domain, ServiceGroupID: request.ServiceGroupID,
			HTTPS: request.HTTPS, Enabled: true,
		})
		created = true
	case err != nil:
		return "", err
	case route.GatewayID != gatewayID || route.Domain != request.Domain ||
		route.ServiceGroupID != request.ServiceGroupID || route.HTTPS != request.HTTPS || !route.Enabled:
		route, err = a.gateways.UpdateRoute(ctx, request.SubjectID, request.RequestID, gatewayID, routeID, route.Revision, gateway.UpdateRouteInput{
			Domain: request.Domain, ServiceGroupID: request.ServiceGroupID,
			HTTPS: request.HTTPS, Enabled: true,
		})
	}
	if err != nil {
		return "", err
	}
	site, err := gateway.RenderNginxSite(gateway.NginxSiteRequest{
		GatewayID: gatewayID, Domain: request.Domain, ServiceGroupID: request.ServiceGroupID,
		ServiceGroupRevision: request.ExpectedGroupRevision, UpstreamHosts: upstreams,
		HTTPS: request.HTTPS, CertificatePath: certificatePath, PrivateKeyPath: keyPath,
	})
	if err != nil {
		return "", err
	}
	taskID, err := a.tasks.apply(ctx, plan, site, gateway.NginxTaskInput{
		SubjectID: request.SubjectID, RequestID: request.RequestID, GatewayID: gatewayID,
		NodeID: record.NodeID, IdempotencyKey: plan.ID,
	})
	if err != nil {
		return "", err
	}
	summary, err := json.Marshal(gatewayPublicationSummary{
		RouteID: routeID, GatewayID: gatewayID, NodeID: record.NodeID,
		SitePath: site.Path, ConfigSHA256: site.Digest, TaskID: taskID,
		RouteRevision: route.Revision, RouteCreated: created,
		PlanID: plan.ID, PlanTargetRevision: plan.TargetRevision,
	})
	if err != nil {
		return "", publishing.ErrProductionTarget
	}
	return string(summary), nil
}

func (a *publicationGatewayApplier) currentPlan(ctx context.Context, plan publishing.Plan) (publishing.Plan, error) {
	current, _, err := a.plans.Get(ctx, plan.ID)
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
	if len(summary) == 0 || len(summary) > 2048 || json.Unmarshal([]byte(summary), &state) != nil {
		return publishing.ErrProductionTarget
	}
	if state.PlanID != plan.ID || state.PlanTargetRevision != plan.TargetRevision ||
		state.GatewayID == "" || state.NodeID == "" || state.SitePath == "" || state.ConfigSHA256 == "" {
		return publishing.ErrProductionTarget
	}
	if _, err := a.currentPlan(ctx, plan); err != nil {
		return err
	}
	site := gateway.NginxSite{GatewayID: state.GatewayID, Domain: plan.Domain, Path: state.SitePath, Digest: state.ConfigSHA256}
	if _, err := a.tasks.remove(ctx, plan, site, gateway.NginxTaskInput{
		SubjectID: plan.CreatedBy, RequestID: plan.ID, GatewayID: state.GatewayID,
		NodeID: state.NodeID, IdempotencyKey: plan.ID,
	}); err != nil {
		return err
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

type providerSecretBorrower struct {
	credentials *auth.CredentialService
	lookup      ProviderCredentialLookup
}

func newProviderSecretBorrower(credentials *auth.CredentialService, lookup ProviderCredentialLookup) (*providerSecretBorrower, error) {
	if credentials == nil || lookup == nil {
		return nil, publishing.ErrProductionUnavailable
	}
	return &providerSecretBorrower{credentials: credentials, lookup: lookup}, nil
}

func (b *providerSecretBorrower) withSecret(ctx context.Context, ref, operation string, planDigest, subjectID, requestID string, fn func(context.Context, string) error) error {
	if b == nil || b.credentials == nil || b.lookup == nil || ref == "" || operation == "" || fn == nil {
		return publishing.ErrProductionUnavailable
	}
	meta, ok, err := b.lookup.LookupProviderCredential(ctx, ref)
	if err != nil || !ok || meta.CredentialID == "" || meta.Owner == "" || meta.Revision == 0 {
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
func NewDNSPublisherSecretBorrower(credentials *auth.CredentialService, lookup ProviderCredentialLookup) (dns.SecretBorrower, error) {
	borrower, err := newProviderSecretBorrower(credentials, lookup)
	if err != nil {
		return nil, err
	}
	return dnsSecretBorrower{borrower: borrower}, nil
}

// NewNotificationSecretBorrower exposes the callback-scoped notification
// provider port. SMTP and QQ channel secretRefs must resolve through lookup.
func NewNotificationSecretBorrower(credentials *auth.CredentialService, lookup ProviderCredentialLookup) (notifications.SecretBorrower, error) {
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
	return b.borrower.withSecret(ctx, use.Ref, use.Operation, use.PlanDigest, use.SubjectID, use.RequestID, fn)
}

type notificationsSecretBorrower struct{ borrower *providerSecretBorrower }

func (b notificationsSecretBorrower) WithSecret(ctx context.Context, use notifications.SecretUse, fn func(context.Context, string) error) error {
	if b.borrower == nil {
		return publishing.ErrProductionUnavailable
	}
	return b.borrower.withSecret(ctx, use.Ref, use.Operation, use.PlanDigest, use.SubjectID, use.RequestID, fn)
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
		return certificates.ErrCleanupUnavailable
	}
	records, err := c.dns.ListRecords(ctx, subjectID, "")
	if err != nil {
		return err
	}
	matches := 0
	for _, record := range records {
		if record.ID == recordID && record.Name == fqdn && record.Content == expectedValue {
			matches++
		}
	}
	if matches != 1 {
		return certificates.ErrCleanupNotFound
	}
	return c.dns.DeleteRecord(ctx, subjectID, requestID, recordID)
}
