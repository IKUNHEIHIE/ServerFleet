package publishing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/dns"
)

var (
	ErrProductionUnavailable = errors.New("publishing: production stage is not configured")
	ErrProductionTarget      = errors.New("publishing: publication target is invalid")
	ErrProductionCertificate = errors.New("publishing: publication certificate is missing or unusable")
)

// PlanLookup is the read-only plan view used by compensation to recover the
// current actor. It never authorizes execution by itself.
type PlanLookup interface {
	GetPlan(ctx context.Context, planID string) (Plan, error)
}

// DNSRecordWriter is the DNS owner's narrow publication seam. It must use the
// current subject and request for its own authorization and provider I/O.
type DNSRecordWriter interface {
	EnsureRecord(ctx context.Context, subjectID, requestID, zoneID, name string, recordType dns.RecordType, content string, ttl int) (recordID string, created bool, err error)
	DeleteRecord(ctx context.Context, subjectID, requestID, zoneID, recordID string) error
}

// GatewayAddresses resolves the public addresses of the selected gateway nodes.
type GatewayAddresses interface {
	Addresses(ctx context.Context, subjectID string, gatewayIDs []string) ([]string, error)
}

// CertificateInspection validates a pre-provisioned certificate and returns its
// installation paths. It never issues, renews or revokes a shared certificate.
type CertificateInspection interface {
	Inspect(ctx context.Context, subjectID, certificateID, domain string) (summary string, err error)
	Paths(ctx context.Context, subjectID, certificateID string) (certificatePath, keyPath string, err error)
}

// ServiceUpstreams resolves the actual service endpoints of a service group.
type ServiceUpstreams interface {
	Upstreams(ctx context.Context, subjectID, serviceGroupID string) ([]string, error)
}

// GatewaySiteApplier creates the route/site and submits the unified Nginx task.
// Rollback must only remove resources it created (or whose immutable summary says
// they belong to this plan).
type GatewaySiteApplier interface {
	Apply(ctx context.Context, plan Plan, request GatewayRequest, upstreams []string, certificatePath, keyPath string) (summary string, err error)
	Rollback(ctx context.Context, plan Plan, summary string) error
}

// ProductionBridgeConfig binds real domain services through narrow ports. All
// ports are mandatory; an empty bridge is never treated as success.
type ProductionBridgeConfig struct {
	Plans        PlanLookup
	DNS          DNSRecordWriter
	Addresses    GatewayAddresses
	Certificates CertificateInspection
	Upstreams    ServiceUpstreams
	Gateway      GatewaySiteApplier
}

// NewProductionBridge composes the three stage publishers from real domain
// ports. The returned LocalBridge still fails closed for any missing function.
func NewProductionBridge(config ProductionBridgeConfig) (LocalBridge, error) {
	if config.Plans == nil || config.DNS == nil || config.Addresses == nil ||
		config.Certificates == nil || config.Upstreams == nil || config.Gateway == nil {
		return LocalBridge{}, ErrProductionUnavailable
	}
	return LocalBridge{
		DNS: &productionDNS{config: config}, Certificates: &productionCertificate{config: config},
		Gateway: &productionGateway{config: config},
	}, nil
}

type productionDNS struct{ config ProductionBridgeConfig }

func (p *productionDNS) Publish(ctx context.Context, request DNSPublishRequest) (StageResult, error) {
	if p == nil || p.config.DNS == nil || p.config.Addresses == nil {
		return StageResult{}, ErrProductionUnavailable
	}
	if p.config.Plans == nil || request.ZoneID == "" || len(request.GatewayIDs) == 0 || request.TargetRevision == 0 || request.Domain == "" {
		return StageResult{}, ErrProductionTarget
	}
	plan, err := p.config.Plans.GetPlan(ctx, request.PlanID)
	if err != nil || !plan.TargetsPersisted || plan.TargetRevision != request.TargetRevision ||
		plan.ZoneID != request.ZoneID || !sameIDs(plan.GatewayIDs, request.GatewayIDs) {
		return StageResult{}, ErrProductionTarget
	}
	if len(request.GatewayIDs) > 8 {
		return StageResult{}, fmt.Errorf("%w: too many gateways", ErrProductionTarget)
	}
	addresses, err := p.config.Addresses.Addresses(ctx, request.SubjectID, request.GatewayIDs)
	if err != nil {
		return StageResult{}, ErrProductionTarget
	}
	addresses = normalizeAddresses(addresses)
	if len(addresses) == 0 || len(addresses) > 8 {
		return StageResult{}, ErrProductionTarget
	}
	created := make([]publicationRecordState, 0, len(addresses))
	for _, address := range addresses {
		recordType, content, err := publicationRecord(address)
		if err != nil {
			return p.failedStage(ctx, request, created, err)
		}
		recordID, isNew, err := p.config.DNS.EnsureRecord(ctx, request.SubjectID, request.RequestID, request.ZoneID,
			request.Domain, recordType, content, 60)
		if err != nil {
			return p.failedStage(ctx, request, created, err)
		}
		if recordID == "" {
			return p.failedStage(ctx, request, created, ErrProductionTarget)
		}
		created = append(created, publicationRecordState{id: recordID, created: isNew})
	}
	summary := dnsStageSummary(request.ZoneID, created)
	if len(summary) > 512 {
		return p.failedStage(ctx, request, created, ErrProductionTarget)
	}
	return StageResult{Summary: summary}, nil
}

// failedStage attempts to remove only the records this publish created and keeps
// the identities whose deletion failed in the returned summary. A partial DNS
// cleanup failure is persisted by the stage row and retried by explicit rollback.
func (p *productionDNS) failedStage(ctx context.Context, request DNSPublishRequest, created []publicationRecordState, cause error) (StageResult, error) {
	remaining, cleanupFailures := p.cleanup(ctx, request, created)
	summary := ""
	if len(remaining) > 0 {
		summary = dnsStageSummary(request.ZoneID, remaining)
	}
	errs := make([]error, 0, 1+len(cleanupFailures))
	errs = append(errs, cause)
	errs = append(errs, cleanupFailures...)
	return StageResult{Summary: summary}, errors.Join(errs...)
}

func dnsStageSummary(zoneID string, records []publicationRecordState) string {
	var summary strings.Builder
	summary.WriteString("dns;zone=" + zoneID + ";records=")
	for index, record := range records {
		if index > 0 {
			summary.WriteByte(',')
		}
		flag := "0"
		if record.created {
			flag = "1"
		}
		summary.WriteString(record.id + ":" + flag)
	}
	return summary.String()
}

func (p *productionDNS) Rollback(ctx context.Context, planID string, result StageResult) error {
	if p == nil || p.config.DNS == nil || p.config.Plans == nil {
		return ErrProductionUnavailable
	}
	plan, err := p.config.Plans.GetPlan(ctx, planID)
	if err != nil {
		return err
	}
	zone, records, err := parseDNSCompensation(result.Summary)
	if err != nil {
		return err
	}
	var failures []error
	for _, record := range records {
		if !record.created {
			continue
		}
		if err := p.config.DNS.DeleteRecord(ctx, plan.CreatedBy, planID, zone, record.id); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

type publicationRecordState struct {
	id      string
	created bool
}

func (p *productionDNS) cleanup(ctx context.Context, request DNSPublishRequest, records []publicationRecordState) ([]publicationRecordState, []error) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	remaining := make([]publicationRecordState, 0)
	var failures []error
	for _, record := range records {
		if !record.created {
			continue
		}
		if err := p.config.DNS.DeleteRecord(cleanupCtx, request.SubjectID, request.RequestID, request.ZoneID, record.id); err != nil {
			failures = append(failures, err)
			remaining = append(remaining, record)
		}
	}
	return remaining, failures
}

type productionCertificate struct{ config ProductionBridgeConfig }

func (p *productionCertificate) Publish(ctx context.Context, request CertificateRequest) (StageResult, error) {
	if p == nil || p.config.Certificates == nil || p.config.Plans == nil {
		return StageResult{}, ErrProductionUnavailable
	}
	if request.HTTPS && request.CertificateID == "" {
		return StageResult{}, ErrProductionCertificate
	}
	plan, err := p.config.Plans.GetPlan(ctx, request.PlanID)
	if err != nil || !plan.TargetsPersisted || plan.TargetRevision != request.TargetRevision ||
		(request.HTTPS && plan.CertificateID != request.CertificateID) {
		return StageResult{}, ErrProductionTarget
	}
	if !request.HTTPS {
		return StageResult{Summary: "certificate=not_required"}, nil
	}
	summary, err := p.config.Certificates.Inspect(ctx, request.SubjectID, request.CertificateID, request.Domain)
	if err != nil || strings.TrimSpace(summary) == "" {
		return StageResult{}, ErrProductionCertificate
	}
	return StageResult{Summary: "certificate=" + request.CertificateID + ";" + summary}, nil
}

func (p *productionCertificate) Rollback(context.Context, string, StageResult) error {
	// A pre-existing certificate is shared by other plans; publication rollback
	// must never revoke it. Issuance/revocation is an explicit certificate action.
	return nil
}

type productionGateway struct{ config ProductionBridgeConfig }

func (p *productionGateway) Publish(ctx context.Context, request GatewayRequest) (StageResult, error) {
	if p == nil || p.config.Plans == nil || p.config.Upstreams == nil || p.config.Certificates == nil || p.config.Gateway == nil {
		return StageResult{}, ErrProductionUnavailable
	}
	if len(request.GatewayIDs) == 0 || len(request.GatewayIDs) > 8 {
		return StageResult{}, ErrProductionTarget
	}
	if request.TargetRevision == 0 || request.Domain == "" || request.ServiceGroupID == "" {
		return StageResult{}, ErrProductionTarget
	}
	upstreams, err := p.config.Upstreams.Upstreams(ctx, request.SubjectID, request.ServiceGroupID)
	if err != nil || len(upstreams) == 0 {
		return StageResult{}, ErrProductionTarget
	}
	var certificatePath, keyPath string
	if request.HTTPS {
		if request.CertificateID == "" {
			return StageResult{}, ErrProductionCertificate
		}
		certificatePath, keyPath, err = p.config.Certificates.Paths(ctx, request.SubjectID, request.CertificateID)
		if err != nil || certificatePath == "" || keyPath == "" {
			return StageResult{}, ErrProductionCertificate
		}
	}
	plan, err := p.config.Plans.GetPlan(ctx, request.PlanID)
	if err != nil || !plan.TargetsPersisted || plan.TargetRevision != request.TargetRevision ||
		plan.ZoneID != request.ZoneID || plan.CertificateID != request.CertificateID || !sameIDs(plan.GatewayIDs, request.GatewayIDs) {
		return StageResult{}, ErrProductionTarget
	}
	summaries := make([]string, 0, len(request.GatewayIDs))
	for _, id := range request.GatewayIDs {
		one := request
		one.GatewayIDs = []string{id}
		summary, err := p.config.Gateway.Apply(ctx, plan, one, upstreams, certificatePath, keyPath)
		if strings.TrimSpace(summary) != "" {
			summaries = append(summaries, summary)
		}
		if err != nil {
			return StageResult{Summary: gatewayBatchSummary(summaries)}, err
		}
		if strings.TrimSpace(summary) == "" {
			return StageResult{Summary: gatewayBatchSummary(summaries)}, ErrProductionTarget
		}
	}
	return StageResult{Summary: gatewayBatchSummary(summaries)}, nil
}

func (p *productionGateway) Rollback(ctx context.Context, planID string, result StageResult) error {
	if p == nil || p.config.Gateway == nil || p.config.Plans == nil {
		return ErrProductionUnavailable
	}
	plan, err := p.config.Plans.GetPlan(ctx, planID)
	if err != nil {
		return err
	}
	summaries, err := parseGatewayBatchSummary(result.Summary)
	if err != nil {
		return err
	}
	for i := len(summaries) - 1; i >= 0; i-- {
		if err := p.config.Gateway.Rollback(ctx, plan, summaries[i]); err != nil {
			return err
		}
	}
	return nil
}

func sameIDs(left, right []string) bool {
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

func normalizeAddresses(addresses []string) []string {
	seen := make(map[string]struct{}, len(addresses))
	out := make([]string, 0, len(addresses))
	for _, raw := range addresses {
		address := strings.TrimSpace(raw)
		if address == "" {
			continue
		}
		if _, duplicate := seen[address]; duplicate {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, address)
	}
	sort.Strings(out)
	return out
}

func publicationRecord(address string) (dns.RecordType, string, error) {
	host := address
	if parsedHost, _, err := net.SplitHostPort(address); err == nil {
		host = parsedHost
	}
	ip := net.ParseIP(host)
	switch {
	case ip == nil:
		return "", "", fmt.Errorf("%w: gateway address must be an IP", ErrProductionTarget)
	case ip.To4() != nil:
		return dns.RecordA, host, nil
	default:
		return dns.RecordAAAA, host, nil
	}
}

type dnsCompensationRecord struct {
	id      string
	created bool
}

func parseDNSCompensation(summary string) (string, []dnsCompensationRecord, error) {
	parts := strings.Split(summary, ";")
	if len(parts) != 3 || parts[0] != "dns" || !strings.HasPrefix(parts[1], "zone=") || !strings.HasPrefix(parts[2], "records=") {
		return "", nil, ErrProductionTarget
	}
	zone := strings.TrimPrefix(parts[1], "zone=")
	if zone == "" {
		return "", nil, ErrProductionTarget
	}
	raw := strings.TrimPrefix(parts[2], "records=")
	if raw == "" {
		return zone, nil, nil
	}
	records := make([]dnsCompensationRecord, 0)
	for _, item := range strings.Split(raw, ",") {
		id, flag, ok := strings.Cut(item, ":")
		if !ok || id == "" || (flag != "0" && flag != "1") {
			return "", nil, ErrProductionTarget
		}
		records = append(records, dnsCompensationRecord{id: id, created: flag == "1"})
	}
	return zone, records, nil
}

type gatewayBatchReceipt struct {
	Version int      `json:"gatewayBatchVersion"`
	Items   []string `json:"items"`
}

func gatewayBatchSummary(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0]
	}
	raw, err := json.Marshal(gatewayBatchReceipt{1, items})
	if err != nil || len(raw) > maxReceiptBytes {
		return ""
	}
	return string(raw)
}
func parseGatewayBatchSummary(raw string) ([]string, error) {
	if len(raw) == 0 || len(raw) > maxReceiptBytes {
		return nil, ErrProductionTarget
	}
	var batch gatewayBatchReceipt
	if json.Unmarshal([]byte(raw), &batch) == nil && batch.Version != 0 {
		if batch.Version != 1 || len(batch.Items) == 0 || len(batch.Items) > 8 {
			return nil, ErrProductionTarget
		}
		for _, item := range batch.Items {
			if len(item) == 0 || len(item) > 4096 {
				return nil, ErrProductionTarget
			}
		}
		return batch.Items, nil
	}
	return []string{raw}, nil
}
