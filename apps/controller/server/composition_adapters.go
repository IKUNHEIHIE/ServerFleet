package server

import (
	"context"
	"errors"
	"serverfleet.local/serverfleet/internal/certificates"
	"serverfleet.local/serverfleet/internal/files"
	"serverfleet.local/serverfleet/internal/gateway"
	"serverfleet.local/serverfleet/internal/monitoring"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/publishing"
	"serverfleet.local/serverfleet/internal/quota"
	"serverfleet.local/serverfleet/internal/servicegroups"
	"serverfleet.local/serverfleet/internal/storage"
	"serverfleet.local/serverfleet/internal/tasks"
	"strings"
)

type composedGroupResolver struct {
	groups        *nodes.GroupRepo
	serviceGroups *servicegroups.Repo
}

func (r *composedGroupResolver) ResolveTargets(ctx context.Context, targetType, groupID string) ([]string, error) {
	switch targetType {
	case "node_group":
		group, err := r.groups.Get(ctx, groupID)
		if err != nil {
			return nil, err
		}
		return append([]string(nil), group.Members...), nil
	case "service_group":
		group, err := r.serviceGroups.Get(ctx, groupID)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(group.Members))
		for _, m := range group.Members {
			out = append(out, m.NodeID)
		}
		return out, nil
	default:
		return nil, tasks.ErrInvalidTask
	}
}

func resolveOperationClass(actionID string) (tasks.OperationClass, error) {
	if strings.TrimSpace(actionID) == "" || strings.ContainsAny(actionID, " \t\r\n:") {
		return "", tasks.ErrUnknownOperationClass
	}
	switch actionID {
	case tasks.ActionSubmit, "tasks.command", files.ActionTransfer:
		return tasks.OperationClassStandard, nil
	case "host.root":
		return tasks.OperationClassRoot, nil
	default:
		return tasks.OperationClassModifying, nil
	}
}

type composedNodeLookup struct {
	nodes *nodes.Repo
}

func (l *composedNodeLookup) Online(ctx context.Context, nodeID string) (bool, error) {
	node, err := l.nodes.Get(ctx, nodeID)
	if err != nil {
		return false, err
	}
	return node.Status == nodes.StatusOnline, nil
}

func (l *composedNodeLookup) LookupNode(ctx context.Context, nodeID string) (gateway.NodeRef, bool, error) {
	node, err := l.nodes.Get(ctx, nodeID)
	if storage.IsNotFound(err) {
		return gateway.NodeRef{}, false, nil
	}
	if err != nil {
		return gateway.NodeRef{}, false, err
	}
	return gateway.NodeRef{
		ID:     node.ID,
		Online: node.Status == nodes.StatusOnline,
	}, true, nil
}

type composedServiceGroupLookup struct {
	repo *servicegroups.Repo
}

func (l *composedServiceGroupLookup) LookupServiceGroup(ctx context.Context, serviceGroupID string) (gateway.ServiceGroupRef, bool, error) {
	group, err := l.repo.Get(ctx, serviceGroupID)
	if storage.IsNotFound(err) {
		return gateway.ServiceGroupRef{}, false, nil
	}
	if err != nil {
		return gateway.ServiceGroupRef{}, false, err
	}
	return gateway.ServiceGroupRef{
		ID:       group.ID,
		Revision: group.Revision,
	}, true, nil
}

func (l *composedServiceGroupLookup) Revision(ctx context.Context, serviceGroupID string) (uint64, error) {
	group, err := l.repo.Get(ctx, serviceGroupID)
	if err != nil {
		return 0, err
	}
	return group.Revision, nil
}

func (l *composedServiceGroupLookup) Members(ctx context.Context, serviceGroupID string) ([]string, error) {
	group, err := l.repo.Get(ctx, serviceGroupID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(group.Members))
	for _, m := range group.Members {
		out = append(out, m.NodeID)
	}
	return out, nil
}

func (l *composedServiceGroupLookup) LookupGroup(ctx context.Context, serviceGroupID string) (publishing.GroupRef, bool, error) {
	group, err := l.repo.Get(ctx, serviceGroupID)
	if storage.IsNotFound(err) {
		return publishing.GroupRef{}, false, nil
	}
	if err != nil {
		return publishing.GroupRef{}, false, err
	}
	nodeIDs := make([]string, 0, len(group.Members))
	for _, m := range group.Members {
		nodeIDs = append(nodeIDs, m.NodeID)
	}
	return publishing.GroupRef{
		ID:       group.ID,
		Revision: group.Revision,
		NodeIDs:  nodeIDs,
	}, true, nil
}

type composedTrafficRiskSource struct {
	quota *quota.Service
}

func (s *composedTrafficRiskSource) ListTrafficRisks(ctx context.Context, subjectID string) ([]monitoring.TrafficRisk, error) {
	if s == nil || s.quota == nil {
		return nil, errors.New("quota: traffic risk source is unavailable")
	}
	items, err := s.quota.ListTrafficRisks(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	out := make([]monitoring.TrafficRisk, 0, len(items))
	for _, item := range items {
		out = append(out, monitoring.TrafficRisk{
			NodeID:    item.NodeID,
			Kind:      item.Kind,
			Severity:  item.Severity,
			Estimated: item.Estimated,
		})
	}
	return out, nil
}

type composedCertTaskBridge struct {
	tasks *tasks.Service
}

func (b *composedCertTaskBridge) Submit(ctx context.Context, req certificates.DistributionTask) (string, error) {
	task, err := b.tasks.Submit(ctx, tasks.SubmitRequest{
		SubjectID:      req.SubjectID,
		RequestID:      newRequestID(),
		Name:           "certificate distribution " + req.CertificateID,
		ActionID:       certificates.ActionDistributionCreate,
		OperationClass: tasks.OperationClassStandard,
		TargetType:     "node_ids",
		TargetNodeIDs:  req.NodeIDs,
		Params: map[string]string{
			"certificate_id":   req.CertificateID,
			"service_group_id": req.ServiceGroupID,
		},
	})
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

type composedTaskBridge struct {
	tasks *tasks.Service
}

func (b *composedTaskBridge) SubmitTransfer(ctx context.Context, command files.TransferCommand) (string, error) {
	task, err := b.tasks.Submit(ctx, tasks.SubmitRequest{
		SubjectID:      command.SubjectID,
		RequestID:      command.RequestID,
		Name:           "file transfer",
		ActionID:       files.ActionTransfer,
		OperationClass: tasks.OperationClassStandard,
		TargetType:     "node_ids",
		TargetNodeIDs:  []string{command.SourceNodeID, command.DestinationNodeID},
		Params: map[string]string{
			"source_node":     command.SourceNodeID,
			"dest_node":       command.DestinationNodeID,
			"dest_dir":        command.DestinationDir,
			"conflict_policy": string(command.ConflictPolicy),
		},
	})
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

func (b *composedTaskBridge) CancelTask(ctx context.Context, taskID, subjectID, requestID string) error {
	_, err := b.tasks.Cancel(ctx, taskID, subjectID, requestID)
	return err
}

func (b *composedTaskBridge) Submit(ctx context.Context, request gateway.TaskRequest) (string, error) {
	task, err := b.tasks.Submit(ctx, tasks.SubmitRequest{
		SubjectID:      request.SubjectID,
		RequestID:      request.RequestID,
		Name:           "gateway provision " + request.ResourceID,
		ActionID:       request.ActionID,
		OperationClass: tasks.OperationClassStandard,
		TargetType:     "node_ids",
		TargetNodeIDs:  request.NodeIDs,
		Params:         request.Params,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

type composedEligibility struct {
	nodes    *nodes.Repo
	gateways *gateway.Repo
	db       *storage.DB
}

func (e *composedEligibility) GatewayEligible(ctx context.Context, _ string) (bool, error) {
	list, err := e.gateways.ListGateways(ctx, e.db)
	if err != nil {
		return false, err
	}
	for _, gw := range list {
		node, err := e.nodes.Get(ctx, gw.NodeID)
		if err != nil {
			continue
		}
		if gw.SchedulingEligible(node.Status == nodes.StatusOnline) {
			return true, nil
		}
	}
	return false, nil
}

func (e *composedEligibility) NodeHealthy(ctx context.Context, nodeID string) (bool, string, error) {
	node, err := e.nodes.Get(ctx, nodeID)
	if err != nil {
		return false, "node_not_found", err
	}
	if node.Status != nodes.StatusOnline {
		return false, "node_offline", nil
	}
	return true, "online", nil
}

type composedDeleteGuard struct {
	gateways *gateway.Repo
	db       *storage.DB
}

func (g *composedDeleteGuard) CheckDelete(ctx context.Context, serviceGroupID string) error {
	routes, err := g.gateways.ListRoutes(ctx, g.db, "")
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.ServiceGroupID == serviceGroupID {
			return errors.New("servicegroups: service group is still referenced by gateway routes")
		}
	}
	return nil
}
