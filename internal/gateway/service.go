package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
)

const (
	ActionRead            = "gateway.read"
	ActionWrite           = "gateway.write"
	ActionCreate          = "gateway.create"
	ActionUpdate          = "gateway.update"
	ActionProvision       = "gateway.provision"
	ActionProvisionUpdate = "gateway.provision_update"
	ActionChecks          = "gateway.checks"
	ActionRouteCreate     = "gateway.route_create"
	ActionRouteUpdate     = "gateway.route_update"
	ActionRouteDelete     = "gateway.route_delete"
)

// ServiceActions returns the audit action catalog this domain emits.
func ServiceActions() []string {
	return []string{
		ActionChecks, ActionCreate, ActionProvision, ActionProvisionUpdate, ActionRead,
		ActionRouteCreate, ActionRouteDelete, ActionRouteUpdate, ActionUpdate, ActionWrite,
	}
}

// AuditSink is the durable audit port. Mutations append in the same transaction.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// Options configures the gateway service. Every port except Checker is required.
// A nil Checker is allowed but makes every route enable fail closed.
type Options struct {
	DB            *storage.DB
	Repo          *Repo
	Nodes         NodeLookup
	ServiceGroups ServiceGroupLookup
	Checker       Checker
	Tasks         TaskSubmitter
	Audit         AuditSink
	Authorizer    auth.Authorizer
	Now           func() time.Time
}

type Service struct {
	db            *storage.DB
	repo          *Repo
	nodes         NodeLookup
	serviceGroups ServiceGroupLookup
	checker       Checker
	tasks         TaskSubmitter
	audit         AuditSink
	authorizer    auth.Authorizer
	now           func() time.Time
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("gateway: database is required")
	case options.Repo == nil:
		return nil, errors.New("gateway: repository is required")
	case options.Nodes == nil:
		return nil, errors.New("gateway: node lookup is required")
	case options.ServiceGroups == nil:
		return nil, errors.New("gateway: service group lookup is required")
	case options.Tasks == nil:
		return nil, ErrTaskSubmitterUnavailable
	case options.Audit == nil:
		return nil, errors.New("gateway: audit sink is required")
	case options.Authorizer == nil:
		return nil, ErrMissingAuthorizer
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: options.DB, repo: options.Repo, nodes: options.Nodes,
		serviceGroups: options.ServiceGroups, checker: options.Checker,
		tasks: options.Tasks, audit: options.Audit, authorizer: options.Authorizer,
		now: now,
	}, nil
}

func (s *Service) CreateGateway(ctx context.Context, subjectID, requestID string, input CreateGatewayInput) (GatewayNode, error) {
	if err := s.requireRequest(requestID); err != nil {
		return GatewayNode{}, err
	}
	normalized, err := normalizeCreateGatewayInput(input)
	if err != nil {
		return GatewayNode{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, []string{normalized.NodeID}, "gateway_create"); err != nil {
		return GatewayNode{}, err
	}
	node, err := s.lookupNode(ctx, normalized.NodeID)
	if err != nil {
		return GatewayNode{}, err
	}
	now := s.now().UTC()
	gateway := GatewayNode{
		ID: normalized.ID, NodeID: node.ID, Name: normalized.Name,
		Region: normalized.Region, CountryCode: normalized.CountryCode,
		Status: StatusPending, Enabled: normalized.Enabled, Revision: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		created, err := s.repo.CreateGateway(txCtx, tx, gateway)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionCreate, "success", requestID, []string{gateway.NodeID}, "")); err != nil {
			return fmt.Errorf("gateway: audit gateway create: %w", err)
		}
		gateway = created
		return nil
	})
	return gateway, err
}

func (s *Service) UpdateGateway(ctx context.Context, subjectID, requestID, gatewayID string, expectedRevision uint64, input UpdateGatewayInput) (GatewayNode, error) {
	if err := s.requireRequest(requestID); err != nil {
		return GatewayNode{}, err
	}
	if expectedRevision == 0 {
		return GatewayNode{}, ErrInvalidRevision
	}
	normalized, err := normalizeUpdateGatewayInput(input)
	if err != nil {
		return GatewayNode{}, err
	}
	current, err := s.repo.GetGateway(ctx, s.db, gatewayID)
	if err != nil {
		return GatewayNode{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, []string{current.NodeID}, "gateway_update"); err != nil {
		return GatewayNode{}, err
	}
	updated := current
	updated.Name = normalized.Name
	updated.Region = normalized.Region
	updated.CountryCode = normalized.CountryCode
	updated.Enabled = normalized.Enabled
	updated.UpdatedAt = s.now().UTC()
	var result GatewayNode
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		record, err := s.repo.UpdateGateway(txCtx, tx, updated, expectedRevision)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionUpdate, "success", requestID, []string{record.NodeID}, "")); err != nil {
			return fmt.Errorf("gateway: audit gateway update: %w", err)
		}
		result = record
		return nil
	})
	return result, err
}

func (s *Service) GetGateway(ctx context.Context, subjectID, gatewayID string) (GatewayNode, error) {
	gateway, err := s.repo.GetGateway(ctx, s.db, gatewayID)
	if err != nil {
		return GatewayNode{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionRead, "", []string{gateway.NodeID}, "gateway_read"); err != nil {
		return GatewayNode{}, err
	}
	return gateway, nil
}

// ListGateways requires one read grant covering every gateway node. It does not
// filter silently: a denial fails the whole list instead of leaking counts.
func (s *Service) ListGateways(ctx context.Context, subjectID string) ([]GatewayNode, error) {
	gateways, err := s.repo.ListGateways(ctx, s.db)
	if err != nil {
		return nil, err
	}
	nodeIDs := make([]string, 0, len(gateways))
	seen := map[string]struct{}{}
	for _, gateway := range gateways {
		if _, ok := seen[gateway.NodeID]; ok {
			continue
		}
		seen[gateway.NodeID] = struct{}{}
		nodeIDs = append(nodeIDs, gateway.NodeID)
	}
	sort.Strings(nodeIDs)
	if err := s.authorize(ctx, subjectID, ActionRead, "", nodeIDs, "gateway_list"); err != nil {
		return nil, err
	}
	return gateways, nil
}

// SchedulingEligible recomputes admission from a fresh node lookup. The stored
// row never contains a precomputed eligible bit.
func (s *Service) SchedulingEligible(ctx context.Context, subjectID, gatewayID string) (bool, error) {
	gateway, err := s.GetGateway(ctx, subjectID, gatewayID)
	if err != nil {
		return false, err
	}
	node, err := s.lookupNode(ctx, gateway.NodeID)
	if err != nil {
		return false, err
	}
	return gateway.SchedulingEligible(node.Online), nil
}

func (s *Service) Provision(ctx context.Context, subjectID, requestID, gatewayID string, input ProvisionInput) (Provision, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Provision{}, err
	}
	normalized, err := normalizeProvisionInput(input)
	if err != nil {
		return Provision{}, err
	}
	gateway, err := s.repo.GetGateway(ctx, s.db, gatewayID)
	if err != nil {
		return Provision{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, []string{gateway.NodeID}, "gateway_provision"); err != nil {
		return Provision{}, err
	}
	node, err := s.lookupNode(ctx, gateway.NodeID)
	if err != nil {
		return Provision{}, err
	}
	active, err := s.repo.HasActiveProvision(ctx, s.db, gatewayID)
	if err != nil {
		return Provision{}, err
	}
	if active {
		return Provision{}, ErrProvisionInProgress
	}
	params := map[string]string{
		ParamGatewayID:        gatewayID,
		ParamExistingSite:     strconv.FormatBool(normalized.ExistingSite),
		ParamTakeoverApproved: strconv.FormatBool(normalized.TakeoverApproved),
	}
	if normalized.ApprovalRef != "" {
		params[ParamApprovalRef] = normalized.ApprovalRef
	}
	provisionID, err := newProvisionID()
	if err != nil {
		return Provision{}, fmt.Errorf("gateway: generate provision id: %w", err)
	}
	taskID, err := s.tasks.Submit(ctx, TaskRequest{
		SubjectID: subjectID, RequestID: requestID, ActionID: ActionProvision,
		ResourceID: gatewayID, NodeIDs: []string{node.ID},
		Params: params, IdempotencyKey: requestID,
	})
	if err != nil {
		if ctx.Err() != nil {
			return Provision{}, ctx.Err()
		}
		return Provision{}, ErrTaskSubmitFailed
	}
	if !validID(taskID) {
		return Provision{}, ErrTaskSubmitFailed
	}
	now := s.now().UTC()
	provision := Provision{
		ID: provisionID, GatewayID: gatewayID, TaskID: taskID,
		Status: ProvisionQueued, Steps: []ProvisionStep{}, CreatedAt: now, UpdatedAt: now,
	}
	gateway.Status = StatusInstalling
	gateway.ChecksPassed = false
	gateway.LastError = ""
	gateway.UpdatedAt = now
	var result Provision
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := s.repo.UpdateGatewayState(txCtx, tx, gateway, gateway.Revision); err != nil {
			return err
		}
		created, err := s.repo.CreateProvision(txCtx, tx, provision)
		if err != nil {
			return err
		}
		event := s.event(subjectID, ActionProvision, "success", requestID, []string{gateway.NodeID}, "")
		event.TaskID = taskID
		if err := s.audit.AppendTx(txCtx, tx, event); err != nil {
			return fmt.Errorf("gateway: audit provision: %w", err)
		}
		result = created
		return nil
	})
	return result, err
}

// ApplyProvisionUpdate reconciles one trusted task report. It is a system callback
// for the unified task pipeline and performs no user authorization; callers must
// never expose it as a write API. Completion alone never makes a gateway ready
// unless an installed Nginx and passed checks are already recorded.
func (s *Service) ApplyProvisionUpdate(ctx context.Context, taskID, status string, steps []ProvisionStep, failure string) (Provision, error) {
	if !validID(taskID) {
		return Provision{}, fmt.Errorf("%w: task id", ErrInvalidProvision)
	}
	desired, err := provisionStatusFromTask(status)
	if err != nil {
		return Provision{}, err
	}
	provision, err := s.repo.GetProvisionByTaskID(ctx, s.db, taskID)
	if err != nil {
		return Provision{}, err
	}
	gateway, err := s.repo.GetGateway(ctx, s.db, provision.GatewayID)
	if err != nil {
		return Provision{}, err
	}
	if provision.Status.Terminal() {
		if provision.Status == desired {
			return provision, nil
		}
		return Provision{}, ErrProvisionTerminal
	}
	if provision.Status == ProvisionRunning && desired == ProvisionQueued {
		return Provision{}, ErrProvisionTransition
	}
	now := s.now().UTC()
	updated := provision
	updated.Status = desired
	updated.UpdatedAt = now
	if steps != nil {
		normalized, err := normalizeSteps(steps)
		if err != nil {
			return Provision{}, err
		}
		updated.Steps = normalized
	}
	if desired != ProvisionQueued && updated.StartedAt.IsZero() {
		updated.StartedAt = now
	}
	if desired == ProvisionCompleted || desired == ProvisionFailed {
		updated.FinishedAt = now
	} else {
		updated.FinishedAt = time.Time{}
	}
	failureMessage := sanitizeMessage(failure)
	switch desired {
	case ProvisionQueued:
		updated.Error = ""
	case ProvisionRunning:
		updated.Error = ""
		if gateway.Status != StatusMaintenance {
			gateway.Status = StatusInstalling
		}
	case ProvisionCompleted:
		updated.Error = ""
		if gateway.Status != StatusMaintenance {
			if gateway.NginxInstalled && gateway.ChecksPassed {
				gateway.Status = StatusReady
			} else {
				gateway.Status = StatusVerifying
			}
		}
	case ProvisionFailed:
		if failureMessage == "" {
			failureMessage = "provisioning failed"
		}
		updated.Error = failureMessage
		gateway.Status = StatusFailed
		gateway.Enabled = false
	}
	if desired == ProvisionFailed {
		gateway.LastError = failureMessage
	} else if desired == ProvisionCompleted {
		gateway.LastError = ""
	}
	gateway.UpdatedAt = now
	var result Provision
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if _, err := s.repo.UpdateProvision(txCtx, tx, updated, provision.Status); err != nil {
			return err
		}
		if _, err := s.repo.UpdateGatewayState(txCtx, tx, gateway, gateway.Revision); err != nil {
			return err
		}
		resultName := "success"
		if desired == ProvisionFailed {
			resultName = "failure"
		}
		event := s.event("system", ActionProvisionUpdate, resultName, taskID, []string{gateway.NodeID}, "provision_"+string(desired))
		event.TaskID = taskID
		event.Source = audit.SourceSystem
		if err := s.audit.AppendTx(txCtx, tx, event); err != nil {
			return fmt.Errorf("gateway: audit provision update: %w", err)
		}
		result = updated
		return nil
	})
	return result, err
}

func (s *Service) RecordChecks(ctx context.Context, subjectID, requestID, gatewayID string, expectedRevision uint64, report CheckReport) (GatewayNode, error) {
	if err := s.requireRequest(requestID); err != nil {
		return GatewayNode{}, err
	}
	if expectedRevision == 0 {
		return GatewayNode{}, ErrInvalidRevision
	}
	normalized, err := normalizeCheckReport(report)
	if err != nil {
		return GatewayNode{}, err
	}
	current, err := s.repo.GetGateway(ctx, s.db, gatewayID)
	if err != nil {
		return GatewayNode{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, []string{current.NodeID}, "gateway_checks"); err != nil {
		return GatewayNode{}, err
	}
	now := s.now().UTC()
	updated := current
	updated.NginxInstalled = normalized.NginxInstalled
	updated.ChecksPassed = normalized.ChecksPassed
	if normalized.NginxVersion != "" {
		updated.NginxVersion = normalized.NginxVersion
	}
	reason := ""
	if !normalized.ChecksPassed || !normalized.NginxInstalled {
		updated.Status = StatusFailed
		updated.Enabled = false
		updated.LastError = normalized.Error
		if updated.LastError == "" {
			updated.LastError = "checks failed"
		}
		reason = "checks_failed"
	} else {
		updated.LastError = ""
		if updated.Status != StatusMaintenance && updated.Status != StatusInstalling {
			updated.Status = StatusReady
		}
	}
	updated.UpdatedAt = now
	var result GatewayNode
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		record, err := s.repo.UpdateGatewayState(txCtx, tx, updated, expectedRevision)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionChecks, "success", requestID, []string{record.NodeID}, reason)); err != nil {
			return fmt.Errorf("gateway: audit checks: %w", err)
		}
		result = record
		return nil
	})
	return result, err
}

func (s *Service) CreateRoute(ctx context.Context, subjectID, requestID, gatewayID string, input CreateRouteInput) (GatewayRoute, error) {
	if err := s.requireRequest(requestID); err != nil {
		return GatewayRoute{}, err
	}
	normalized, err := normalizeCreateRouteInput(input)
	if err != nil {
		return GatewayRoute{}, err
	}
	gateway, err := s.repo.GetGateway(ctx, s.db, gatewayID)
	if err != nil {
		return GatewayRoute{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, []string{gateway.NodeID}, "gateway_route_create"); err != nil {
		return GatewayRoute{}, err
	}
	if _, err := s.lookupNode(ctx, gateway.NodeID); err != nil {
		return GatewayRoute{}, err
	}
	var groupRevision uint64
	if normalized.ServiceGroupID != "" {
		group, err := s.lookupServiceGroup(ctx, normalized.ServiceGroupID)
		if err != nil {
			return GatewayRoute{}, err
		}
		groupRevision = group.Revision
	}
	now := s.now().UTC()
	route := GatewayRoute{
		ID: normalized.ID, GatewayID: gatewayID, Domain: normalized.Domain,
		Upstream: normalized.Upstream, ServiceGroupID: normalized.ServiceGroupID,
		ServiceGroupRevision: groupRevision, HTTPS: normalized.HTTPS, Enabled: normalized.Enabled,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if route.Enabled {
		if err := s.verifyRoute(ctx, gateway, route); err != nil {
			return GatewayRoute{}, err
		}
	}
	var result GatewayRoute
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		created, err := s.repo.CreateRoute(txCtx, tx, route)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRouteCreate, "success", requestID, []string{gateway.NodeID}, "")); err != nil {
			return fmt.Errorf("gateway: audit route create: %w", err)
		}
		result = created
		return nil
	})
	return result, err
}

func (s *Service) UpdateRoute(ctx context.Context, subjectID, requestID, gatewayID, routeID string, expectedRevision uint64, input UpdateRouteInput) (GatewayRoute, error) {
	if err := s.requireRequest(requestID); err != nil {
		return GatewayRoute{}, err
	}
	if expectedRevision == 0 {
		return GatewayRoute{}, ErrInvalidRevision
	}
	normalized, err := normalizeUpdateRouteInput(input)
	if err != nil {
		return GatewayRoute{}, err
	}
	current, err := s.repo.GetRoute(ctx, s.db, routeID)
	if err != nil {
		return GatewayRoute{}, err
	}
	if current.GatewayID != gatewayID {
		return GatewayRoute{}, ErrRouteNotFound
	}
	gateway, err := s.repo.GetGateway(ctx, s.db, gatewayID)
	if err != nil {
		return GatewayRoute{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, []string{gateway.NodeID}, "gateway_route_update"); err != nil {
		return GatewayRoute{}, err
	}
	if _, err := s.lookupNode(ctx, gateway.NodeID); err != nil {
		return GatewayRoute{}, err
	}
	var groupRevision uint64
	if normalized.ServiceGroupID != "" {
		group, err := s.lookupServiceGroup(ctx, normalized.ServiceGroupID)
		if err != nil {
			return GatewayRoute{}, err
		}
		groupRevision = group.Revision
	}
	updated := current
	updated.Domain = normalized.Domain
	updated.Upstream = normalized.Upstream
	updated.ServiceGroupID = normalized.ServiceGroupID
	updated.ServiceGroupRevision = groupRevision
	updated.HTTPS = normalized.HTTPS
	updated.Enabled = normalized.Enabled
	updated.UpdatedAt = s.now().UTC()
	if updated.Enabled {
		if err := s.verifyRoute(ctx, gateway, updated); err != nil {
			return GatewayRoute{}, err
		}
	}
	var result GatewayRoute
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		record, err := s.repo.UpdateRoute(txCtx, tx, updated, expectedRevision)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRouteUpdate, "success", requestID, []string{gateway.NodeID}, "")); err != nil {
			return fmt.Errorf("gateway: audit route update: %w", err)
		}
		result = record
		return nil
	})
	return result, err
}

func (s *Service) DeleteRoute(ctx context.Context, subjectID, requestID, gatewayID, routeID string, expectedRevision uint64) error {
	if err := s.requireRequest(requestID); err != nil {
		return err
	}
	if expectedRevision == 0 {
		return ErrInvalidRevision
	}
	route, err := s.repo.GetRoute(ctx, s.db, routeID)
	if err != nil {
		return err
	}
	if route.GatewayID != gatewayID {
		return ErrRouteNotFound
	}
	gateway, err := s.repo.GetGateway(ctx, s.db, gatewayID)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, []string{gateway.NodeID}, "gateway_route_delete"); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.DeleteRoute(txCtx, tx, gatewayID, routeID, expectedRevision); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionRouteDelete, "success", requestID, []string{gateway.NodeID}, ""))
	})
}

func (s *Service) GetRoute(ctx context.Context, subjectID, routeID string) (GatewayRoute, error) {
	route, err := s.repo.GetRoute(ctx, s.db, routeID)
	if err != nil {
		return GatewayRoute{}, err
	}
	gateway, err := s.repo.GetGateway(ctx, s.db, route.GatewayID)
	if err != nil {
		return GatewayRoute{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionRead, "", []string{gateway.NodeID}, "gateway_route_read"); err != nil {
		return GatewayRoute{}, err
	}
	return route, nil
}

func (s *Service) ListRoutes(ctx context.Context, subjectID, gatewayID string) ([]GatewayRoute, error) {
	gateway, err := s.repo.GetGateway(ctx, s.db, gatewayID)
	if err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, subjectID, ActionRead, "", []string{gateway.NodeID}, "gateway_route_list"); err != nil {
		return nil, err
	}
	return s.repo.ListRoutes(ctx, s.db, gatewayID)
}

func (s *Service) GetProvision(ctx context.Context, subjectID, provisionID string) (Provision, error) {
	provision, err := s.repo.GetProvision(ctx, s.db, provisionID)
	if err != nil {
		return Provision{}, err
	}
	gateway, err := s.repo.GetGateway(ctx, s.db, provision.GatewayID)
	if err != nil {
		return Provision{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionRead, "", []string{gateway.NodeID}, "gateway_provision_read"); err != nil {
		return Provision{}, err
	}
	return provision, nil
}

func (s *Service) ListProvisions(ctx context.Context, subjectID, gatewayID string) ([]Provision, error) {
	gateway, err := s.repo.GetGateway(ctx, s.db, gatewayID)
	if err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, subjectID, ActionRead, "", []string{gateway.NodeID}, "gateway_provision_list"); err != nil {
		return nil, err
	}
	return s.repo.ListProvisions(ctx, s.db, gatewayID)
}

func (s *Service) verifyRoute(ctx context.Context, gateway GatewayNode, route GatewayRoute) error {
	if s.checker == nil {
		return ErrCheckerUnavailable
	}
	result, err := s.checker.CheckRoute(ctx, RouteSpec{
		GatewayID: gateway.ID, NodeID: gateway.NodeID, Domain: route.Domain,
		Upstream: route.Upstream, ServiceGroupID: route.ServiceGroupID,
		ServiceGroupRevision: route.ServiceGroupRevision, HTTPS: route.HTTPS,
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrRouteCheckFailed
	}
	if result.PortConflict {
		return ErrRoutePortConflict
	}
	if !result.Passed {
		return ErrRouteCheckFailed
	}
	return nil
}

func (s *Service) lookupNode(ctx context.Context, nodeID string) (NodeRef, error) {
	if !validID(nodeID) {
		return NodeRef{}, ErrNodeNotFound
	}
	node, exists, err := s.nodes.LookupNode(ctx, nodeID)
	if err != nil {
		if ctx.Err() != nil {
			return NodeRef{}, ctx.Err()
		}
		return NodeRef{}, ErrNodeLookupFailed
	}
	if !exists {
		return NodeRef{}, ErrNodeNotFound
	}
	if node.ID != "" && node.ID != nodeID {
		return NodeRef{}, ErrNodeLookupFailed
	}
	node.ID = nodeID
	return node, nil
}

func (s *Service) lookupServiceGroup(ctx context.Context, serviceGroupID string) (ServiceGroupRef, error) {
	if !validID(serviceGroupID) {
		return ServiceGroupRef{}, ErrServiceGroupNotFound
	}
	group, exists, err := s.serviceGroups.LookupServiceGroup(ctx, serviceGroupID)
	if err != nil {
		if ctx.Err() != nil {
			return ServiceGroupRef{}, ctx.Err()
		}
		return ServiceGroupRef{}, ErrServiceGroupLookupFailed
	}
	if !exists {
		return ServiceGroupRef{}, ErrServiceGroupNotFound
	}
	if group.Revision == 0 {
		return ServiceGroupRef{}, ErrServiceGroupLookupFailed
	}
	if group.ID != "" && group.ID != serviceGroupID {
		return ServiceGroupRef{}, ErrServiceGroupLookupFailed
	}
	group.ID = serviceGroupID
	return group, nil
}

func provisionStatusFromTask(status string) (ProvisionStatus, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case string(ProvisionQueued):
		return ProvisionQueued, nil
	case string(ProvisionRunning):
		return ProvisionRunning, nil
	case string(ProvisionCompleted):
		return ProvisionCompleted, nil
	case string(ProvisionFailed), "canceled", "partial_failure", "expired", "unknown":
		return ProvisionFailed, nil
	default:
		return "", fmt.Errorf("%w: task status", ErrInvalidProvision)
	}
}

func newProvisionID() (string, error) {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return "", err
	}
	return "prov_" + hex.EncodeToString(buffer[:]), nil
}

func (s *Service) requireRequest(requestID string) error {
	if s == nil {
		return errors.New("gateway: service is not configured")
	}
	if !validText(requestID, 128) {
		return ErrRequestIDRequired
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, subjectID, action, requestID string, nodeIDs []string, reason string) error {
	check := auth.Check{SubjectID: subjectID, Action: action, NodeIDs: append([]string(nil), nodeIDs...)}
	err := s.authorizer.Authorize(ctx, check)
	if err == nil {
		return nil
	}
	// Read paths carry no request id; only mutations have a durable denial event.
	if isPureDenial(err) && requestID != "" {
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if auditErr := s.audit.Append(auditCtx, s.event(subjectID, action, "denied", requestID, nodeIDs, reason)); auditErr != nil {
			return fmt.Errorf("gateway: authorization denied (denial audit failed: %w)", auditErr)
		}
	}
	return err
}

func (s *Service) event(subjectID, action, result, requestID string, nodeIDs []string, reason string) audit.Event {
	return audit.Event{
		SubjectID: subjectID, Action: action, Result: result, RequestID: requestID,
		NodeIDs: append([]string(nil), nodeIDs...), Source: audit.SourceWeb,
		ReasonCode: reason, At: s.now(),
	}
}

// isPureDenial only treats a single denial chain as a filterable denial.
func isPureDenial(err error) bool {
	for depth := 0; err != nil; depth++ {
		if err == auth.ErrDenied {
			return true
		}
		if depth >= 64 {
			return false
		}
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return false
		}
		single, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = single.Unwrap()
	}
	return false
}
