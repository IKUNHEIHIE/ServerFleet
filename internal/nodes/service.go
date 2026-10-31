package nodes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/storage"
)

const (
	ActionNodesRead    = "nodes.read"
	ActionNodesWrite   = "nodes.write"
	ActionGroupsWrite  = "groups.write"
	ActionAgentUpdate  = "nodes.agent_update"
	ActionNodeCreate   = "nodes.create"
	ActionGroupCreate  = "groups.create"
	ActionGroupRename  = "groups.rename"
	ActionGroupMembers = "groups.members"
	ActionGroupDelete  = "groups.delete"
	ActionNodesSuspend = "nodes.suspend"
	ActionNodesRevoke  = "nodes.revoke"
)

// ServiceActions returns the audit action catalog this domain emits. Composition
// passes it to audit.NewValidator.
func ServiceActions() []string {
	return []string{
		ActionAgentUpdate, ActionGroupCreate, ActionGroupDelete, ActionGroupMembers,
		ActionGroupRename, ActionGroupsWrite, ActionNodeCreate, ActionNodesRead,
		ActionNodesRevoke, ActionNodesSuspend, ActionNodesWrite,
	}
}

var (
	ErrRequestIDRequired = errors.New("nodes: request id is required")
	ErrPolicyUnavailable = errors.New("nodes: read policy is not configured")
)

// AuditSink is the durable audit port. Mutations must write their event in the
// same transaction so an audit failure aborts the change.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// Options configures the authenticated node/group service. All fields are
// required; nil dependencies fail closed rather than defaulting to allow.
type Options struct {
	DB         *storage.DB
	Nodes      *Repo
	Groups     *GroupRepo
	Audit      AuditSink
	Authorizer auth.Authorizer
	ReadPolicy auth.PolicyReader
	Now        func() time.Time
}

type Service struct {
	db         *storage.DB
	nodes      *Repo
	groups     *GroupRepo
	audit      AuditSink
	authorizer auth.Authorizer
	readPolicy auth.PolicyReader
	now        func() time.Time
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("nodes: database is required")
	case options.Nodes == nil:
		return nil, errors.New("nodes: node repository is required")
	case options.Groups == nil:
		return nil, errors.New("nodes: group repository is required")
	case options.Audit == nil:
		return nil, errors.New("nodes: audit sink is required")
	case options.Authorizer == nil:
		return nil, ErrMissingAuthorizer
	case options.ReadPolicy == nil:
		return nil, ErrPolicyUnavailable
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: options.DB, nodes: options.Nodes, groups: options.Groups, audit: options.Audit,
		authorizer: options.Authorizer, readPolicy: options.ReadPolicy, now: now,
	}, nil
}

// ListVisible preflights the subject's read scope before reading inventory, so an
// unknown or empty-scope subject cannot list nodes even when the inventory is empty.
func (s *Service) ListVisible(ctx context.Context, subjectID string) ([]Node, error) {
	if s == nil {
		return nil, errors.New("nodes: service is not configured")
	}
	return ReadVisibleInventory(ctx, subjectID, s.readPolicy, s.nodes.List)
}

// ClassifyVisible classifies only visible nodes so bucket counts cannot leak hidden
// inventory.
func (s *Service) ClassifyVisible(ctx context.Context, subjectID string, dimension Dimension) ([]Classification, error) {
	visible, err := s.ListVisible(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	return Classify(visible, dimension)
}

// GetVisible returns one authorized node, checking the scope before the read.
func (s *Service) GetVisible(ctx context.Context, subjectID, nodeID string) (Node, error) {
	scope, err := auth.PrepareNodeReadScope(ctx, subjectID, s.readPolicy)
	if err != nil {
		return Node{}, err
	}
	if err := scope.CheckNode(ctx, nodeID); err != nil {
		return Node{}, err
	}
	return s.nodes.Get(ctx, nodeID)
}

// ListVisibleGroups preflights the scope, then hides every group containing a node
// the subject may not read. Empty groups remain visible because the scope itself
// already succeeded.
func (s *Service) ListVisibleGroups(ctx context.Context, subjectID string) ([]GroupRecord, error) {
	scope, err := auth.PrepareNodeReadScope(ctx, subjectID, s.readPolicy)
	if err != nil {
		return nil, err
	}
	groups, err := s.groups.List(ctx)
	if err != nil {
		return nil, err
	}
	return filterVisibleGroups(ctx, scope, groups)
}

func (s *Service) GetVisibleGroup(ctx context.Context, subjectID, groupID string) (GroupRecord, error) {
	scope, err := auth.PrepareNodeReadScope(ctx, subjectID, s.readPolicy)
	if err != nil {
		return GroupRecord{}, err
	}
	group, err := s.groups.Get(ctx, groupID)
	if err != nil {
		return GroupRecord{}, err
	}
	visible, err := filterVisibleGroups(ctx, scope, []GroupRecord{group})
	if err != nil {
		return GroupRecord{}, err
	}
	if len(visible) == 0 {
		return GroupRecord{}, auth.ErrDenied
	}
	return visible[0], nil
}

type CreateGroupInput struct {
	ID            string
	Name          string
	Notes         string
	MemberNodeIDs []string
}

func (s *Service) CreateGroup(ctx context.Context, subjectID, requestID string, input CreateGroupInput) (GroupRecord, error) {
	if err := s.requireRequest(requestID); err != nil {
		return GroupRecord{}, err
	}
	members, err := memberSet(input.MemberNodeIDs)
	if err != nil {
		return GroupRecord{}, err
	}
	memberIDs := sortedIDs(members)
	if err := s.authorize(ctx, subjectID, ActionGroupsWrite, requestID, memberIDs, "group_create"); err != nil {
		return GroupRecord{}, err
	}
	var record GroupRecord
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		created, err := s.groups.Create(txCtx, tx, input.ID, input.Name, input.Notes, memberIDs)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionGroupCreate, "success", requestID, memberIDs, "")); err != nil {
			return err
		}
		record = created
		return nil
	})
	return record, err
}

type RenameGroupInput struct {
	ExpectedRevision uint64
	Name             string
	Notes            string
}

func (s *Service) RenameGroup(ctx context.Context, subjectID, requestID, groupID string, input RenameGroupInput) (GroupRecord, error) {
	if err := s.requireRequest(requestID); err != nil {
		return GroupRecord{}, err
	}
	current, err := s.groups.Get(ctx, groupID)
	if err != nil {
		return GroupRecord{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionGroupsWrite, requestID, current.Members, "group_rename"); err != nil {
		return GroupRecord{}, err
	}
	var record GroupRecord
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		updated, err := s.groups.Rename(txCtx, tx, groupID, input.ExpectedRevision, input.Name, input.Notes)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionGroupRename, "success", requestID, updated.Members, "")); err != nil {
			return err
		}
		record = updated
		return nil
	})
	return record, err
}

type ReplaceGroupMembersInput struct {
	ExpectedRevision uint64
	MemberNodeIDs    []string
}

func (s *Service) ReplaceGroupMembers(ctx context.Context, subjectID, requestID, groupID string, input ReplaceGroupMembersInput) (GroupRecord, error) {
	if err := s.requireRequest(requestID); err != nil {
		return GroupRecord{}, err
	}
	members, err := memberSet(input.MemberNodeIDs)
	if err != nil {
		return GroupRecord{}, err
	}
	memberIDs := sortedIDs(members)
	current, err := s.groups.Get(ctx, groupID)
	if err != nil {
		return GroupRecord{}, err
	}
	union := append(append([]string(nil), current.Members...), memberIDs...)
	unionSet, _ := memberSet(union)
	if err := s.authorize(ctx, subjectID, ActionGroupsWrite, requestID, sortedIDs(unionSet), "group_members"); err != nil {
		return GroupRecord{}, err
	}
	var record GroupRecord
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		updated, err := s.groups.ReplaceMembers(txCtx, tx, groupID, input.ExpectedRevision, memberIDs)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionGroupMembers, "success", requestID, memberIDs, "")); err != nil {
			return err
		}
		record = updated
		return nil
	})
	return record, err
}

func (s *Service) DeleteGroup(ctx context.Context, subjectID, requestID, groupID string, expectedRevision uint64) error {
	if err := s.requireRequest(requestID); err != nil {
		return err
	}
	current, err := s.groups.Get(ctx, groupID)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, subjectID, ActionGroupsWrite, requestID, current.Members, "group_delete"); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.groups.Delete(txCtx, tx, groupID, expectedRevision); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionGroupDelete, "success", requestID, current.Members, ""))
	})
}

// CreateNode enrolls inventory after Controller authorization. It never accepts
// metrics; samples only arrive through ApplyAgentUpdate.
func (s *Service) CreateNode(ctx context.Context, subjectID, requestID string, input NodeCreateInput) (Node, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Node{}, err
	}
	if err := s.authorize(ctx, subjectID, ActionNodesWrite, requestID, []string{input.ID}, "node_create"); err != nil {
		return Node{}, err
	}
	var node Node
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		created, err := s.nodes.Create(txCtx, tx, input)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionNodeCreate, "success", requestID, []string{created.ID}, "")); err != nil {
			return err
		}
		node = created
		return nil
	})
	return node, err
}

// ApplyAgentUpdate applies a trusted transport event. It is not a user-facing
// operation and performs no user authorization: the transport layer must only call
// it after authenticating the agent connection. Every applied update is audited.
func (s *Service) ApplyAgentUpdate(ctx context.Context, agentSubject, requestID string, update AgentUpdate) (Node, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Node{}, err
	}
	if !validText(agentSubject, 128) {
		return Node{}, fmt.Errorf("%w: agent subject", ErrInvalidNode)
	}
	var node Node
	err := s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		updated, applied, err := s.nodes.ApplyAgentUpdate(txCtx, tx, update)
		if err != nil {
			return err
		}
		if !applied {
			node = updated
			return nil
		}
		event := audit.Event{
			SubjectID: agentSubject,
			Action:    ActionAgentUpdate,
			Result:    "success",
			RequestID: requestID,
			NodeIDs:   []string{update.NodeID},
			Source:    audit.SourceAgent,
			At:        s.now(),
		}
		if err := s.audit.AppendTx(txCtx, tx, event); err != nil {
			return err
		}
		node = updated
		return nil
	})
	return node, err
}

// Suspend marks a node administratively unavailable. Agent samples no longer
// override the suspended status. Repeating the suspension is an idempotent no-op
// and does not emit a second audit event.
func (s *Service) Suspend(ctx context.Context, subjectID, requestID, nodeID, reason string) (Node, error) {
	return s.changeLifecycle(ctx, subjectID, requestID, nodeID, StatusSuspended, ActionNodesSuspend, "node_suspend", reason)
}

// Revoke marks a node identity as revoked. Revocation is terminal for agent
// updates; repeating it is an idempotent no-op.
func (s *Service) Revoke(ctx context.Context, subjectID, requestID, nodeID, reason string) (Node, error) {
	return s.changeLifecycle(ctx, subjectID, requestID, nodeID, StatusRevoked, ActionNodesRevoke, "node_revoke", reason)
}

func (s *Service) changeLifecycle(ctx context.Context, subjectID, requestID, nodeID string, status Status, action, reasonCode, reason string) (Node, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Node{}, err
	}
	if !validText(reason, 300) {
		return Node{}, fmt.Errorf("%w: reason", ErrInvalidNode)
	}
	if !validToken(nodeID, 128) {
		return Node{}, fmt.Errorf("%w: id", ErrInvalidNode)
	}
	if err := s.authorize(ctx, subjectID, action, requestID, []string{nodeID}, reasonCode); err != nil {
		return Node{}, err
	}
	current, err := s.nodes.Get(ctx, nodeID)
	if err != nil {
		return Node{}, err
	}
	if current.Status == status {
		return current, nil
	}
	if current.Status == StatusRevoked {
		return Node{}, ErrNodeRevoked
	}
	if reasonCode != "" && validToken(reason, audit.DefaultLimits().MaxReasonCodeBytes) {
		reasonCode = reason
	}
	var updated Node
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		record, err := s.nodes.UpdateStatus(txCtx, tx, nodeID, status, s.now())
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, action, "success", requestID, []string{nodeID}, reasonCode)); err != nil {
			return err
		}
		updated = record
		return nil
	})
	if err != nil {
		return Node{}, err
	}
	return updated, nil
}

func (s *Service) requireRequest(requestID string) error {
	if s == nil {
		return errors.New("nodes: service is not configured")
	}
	if !validText(requestID, audit.DefaultLimits().MaxRequestIDBytes) {
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
	if classifyAuthorizationError(err) == authorizationErrorPureDenial {
		denial := audit.Event{
			SubjectID: subjectID, Action: action, Result: "denied", RequestID: requestID,
			NodeIDs: append([]string(nil), nodeIDs...), Source: audit.SourceWeb,
			ReasonCode: reason, At: s.now(),
		}
		if auditErr := s.audit.Append(context.WithoutCancel(ctx), denial); auditErr != nil {
			return fmt.Errorf("nodes: authorization denied (denial audit failed: %w)", auditErr)
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

func filterVisibleGroups(ctx context.Context, scope *auth.NodeReadScope, groups []GroupRecord) ([]GroupRecord, error) {
	out := make([]GroupRecord, 0, len(groups))
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		visible := true
		for _, nodeID := range group.Members {
			checkErr := scope.CheckNode(ctx, nodeID)
			if checkErr == nil {
				continue
			}
			if classifyAuthorizationError(checkErr) == authorizationErrorPureDenial {
				visible = false
				break
			}
			return nil, checkErr
		}
		if visible {
			out = append(out, group)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}
