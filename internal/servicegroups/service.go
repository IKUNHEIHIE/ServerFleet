package servicegroups

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"serverfleet.local/serverfleet/internal/audit"
	"serverfleet.local/serverfleet/internal/auth"
	"serverfleet.local/serverfleet/internal/nodes"
	"serverfleet.local/serverfleet/internal/storage"
)

const (
	ActionRead       = "servicegroups.read"
	ActionWrite      = "servicegroups.write"
	ActionCreate     = "servicegroups.create"
	ActionUpdate     = "servicegroups.update"
	ActionMembers    = "servicegroups.members"
	ActionScheduling = "servicegroups.scheduling"
	ActionDelete     = "servicegroups.delete"
)

// ServiceActions returns the audit action catalog this domain emits.
func ServiceActions() []string {
	return []string{ActionCreate, ActionDelete, ActionMembers, ActionRead, ActionScheduling,
		ActionUpdate, ActionWrite}
}

var (
	ErrMissingAuthorizer      = errors.New("servicegroups: authorizer is required")
	ErrPolicyUnavailable      = errors.New("servicegroups: read policy is not configured")
	ErrRequestIDRequired      = errors.New("servicegroups: request id is required")
	ErrDeleteGuardUnavailable = errors.New("servicegroups: delete reference guard is unavailable")
)

// AuditSink is the durable audit port; mutations write in the same transaction.
type AuditSink interface {
	Append(context.Context, audit.Event) error
	AppendTx(context.Context, storage.Tx, audit.Event) error
}

// Eligibility is implemented by gateway/monitoring composition. When nil, nothing
// is eligible: health or gateway readiness must never default to allowed.
type Eligibility interface {
	GatewayEligible(ctx context.Context, serviceGroupID string) (bool, error)
	NodeHealthy(ctx context.Context, nodeID string) (healthy bool, reason string, err error)
}

// DeleteGuard prevents removing a service group still referenced by gateway
// routes, certificates or AI grants. A nil guard blocks deletion.
type DeleteGuard interface {
	CheckDelete(ctx context.Context, serviceGroupID string) error
}

type Options struct {
	DB          *storage.DB
	Repo        *Repo
	Nodes       *nodes.Repo
	Audit       AuditSink
	Authorizer  auth.Authorizer
	ReadPolicy  auth.PolicyReader
	Eligibility Eligibility
	DeleteGuard DeleteGuard
	Now         func() time.Time
}

type Service struct {
	db          *storage.DB
	repo        *Repo
	nodes       *nodes.Repo
	audit       AuditSink
	authorizer  auth.Authorizer
	readPolicy  auth.PolicyReader
	eligibility Eligibility
	deleteGuard DeleteGuard
	now         func() time.Time
}

func NewService(options Options) (*Service, error) {
	switch {
	case options.DB == nil:
		return nil, errors.New("servicegroups: database is required")
	case options.Repo == nil:
		return nil, errors.New("servicegroups: repository is required")
	case options.Nodes == nil:
		return nil, errors.New("servicegroups: nodes repository is required")
	case options.Audit == nil:
		return nil, errors.New("servicegroups: audit sink is required")
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
		db: options.DB, repo: options.Repo, nodes: options.Nodes, audit: options.Audit,
		authorizer: options.Authorizer, readPolicy: options.ReadPolicy,
		eligibility: options.Eligibility, deleteGuard: options.DeleteGuard, now: now,
	}, nil
}

// ListVisible preflights the node read scope, then hides groups with any hidden
// member. An empty database never bypasses the subject check.
func (s *Service) ListVisible(ctx context.Context, subjectID string) ([]Record, error) {
	scope, err := auth.PrepareNodeReadScope(ctx, subjectID, s.readPolicy)
	if err != nil {
		return nil, err
	}
	records, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	return filterVisible(ctx, scope, records)
}

func (s *Service) GetVisible(ctx context.Context, subjectID, groupID string) (Record, error) {
	scope, err := auth.PrepareNodeReadScope(ctx, subjectID, s.readPolicy)
	if err != nil {
		return Record{}, err
	}
	record, err := s.repo.Get(ctx, groupID)
	if err != nil {
		return Record{}, err
	}
	visible, err := filterVisible(ctx, scope, []Record{record})
	if err != nil {
		return Record{}, err
	}
	if len(visible) == 0 {
		return Record{}, auth.ErrDenied
	}
	return visible[0], nil
}

// VisibleSnapshot returns an immutable membership snapshot for callers that hold a
// current read grant. Removing a member advances the revision so a stale snapshot
// cannot be reused as fresh authority.
func (s *Service) VisibleSnapshot(ctx context.Context, subjectID, groupID string) (Snapshot, error) {
	if _, err := s.GetVisible(ctx, subjectID, groupID); err != nil {
		return Snapshot{}, err
	}
	return s.repo.Snapshot(ctx, groupID)
}

func (s *Service) Create(ctx context.Context, subjectID, requestID string, input Input) (Record, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Record{}, err
	}
	members, err := normalizeMembers(input.MemberNodeIDs)
	if err != nil {
		return Record{}, err
	}
	input.MemberNodeIDs = members
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, members, "service_group_create"); err != nil {
		return Record{}, err
	}
	var record Record
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		created, err := s.repo.Create(txCtx, tx, input)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionCreate, "success", requestID, members, "")); err != nil {
			return fmt.Errorf("servicegroups: audit create: %w", err)
		}
		record = created
		return nil
	})
	return record, err
}

func (s *Service) Update(ctx context.Context, subjectID, requestID, groupID string, expectedRevision uint64, input Input) (Record, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Record{}, err
	}
	current, err := s.repo.Get(ctx, groupID)
	if err != nil {
		return Record{}, err
	}
	members := memberIDs(current.Members)
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, members, "service_group_update"); err != nil {
		return Record{}, err
	}
	var record Record
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		updated, err := s.repo.Update(txCtx, tx, groupID, expectedRevision, input)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionUpdate, "success", requestID, members, "")); err != nil {
			return err
		}
		record = updated
		return nil
	})
	return record, err
}

func (s *Service) ReplaceMembers(ctx context.Context, subjectID, requestID, groupID string, expectedRevision uint64, memberNodeIDs []string) (Record, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Record{}, err
	}
	next, err := normalizeMembers(memberNodeIDs)
	if err != nil {
		return Record{}, err
	}
	current, err := s.repo.Get(ctx, groupID)
	if err != nil {
		return Record{}, err
	}
	union := append(memberIDs(current.Members), next...)
	union = uniqueSorted(union)
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, union, "service_group_members"); err != nil {
		return Record{}, err
	}
	var record Record
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		updated, err := s.repo.ReplaceMembers(txCtx, tx, groupID, expectedRevision, next)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionMembers, "success", requestID, next, "")); err != nil {
			return err
		}
		record = updated
		return nil
	})
	return record, err
}

func (s *Service) SetScheduling(ctx context.Context, subjectID, requestID, groupID, nodeID string, paused bool, reason string) (Member, uint64, error) {
	if err := s.requireRequest(requestID); err != nil {
		return Member{}, 0, err
	}
	current, err := s.repo.Get(ctx, groupID)
	if err != nil {
		return Member{}, 0, err
	}
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, memberIDs(current.Members), "service_group_scheduling"); err != nil {
		return Member{}, 0, err
	}
	var (
		member   Member
		revision uint64
	)
	err = s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		updated, nextRevision, err := s.repo.SetScheduling(txCtx, tx, groupID, nodeID, current.Revision, paused, reason)
		if err != nil {
			return err
		}
		if err := s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionScheduling, "success", requestID, []string{nodeID}, "")); err != nil {
			return err
		}
		member, revision = updated, nextRevision
		return nil
	})
	return member, revision, err
}

func (s *Service) Delete(ctx context.Context, subjectID, requestID, groupID string, expectedRevision uint64) error {
	if err := s.requireRequest(requestID); err != nil {
		return err
	}
	if s.deleteGuard == nil {
		return ErrDeleteGuardUnavailable
	}
	current, err := s.repo.Get(ctx, groupID)
	if err != nil {
		return err
	}
	if err := s.deleteGuard.CheckDelete(ctx, groupID); err != nil {
		return err
	}
	members := memberIDs(current.Members)
	if err := s.authorize(ctx, subjectID, ActionWrite, requestID, members, "service_group_delete"); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(txCtx context.Context, tx storage.Tx) error {
		if err := s.repo.Delete(txCtx, tx, groupID, expectedRevision); err != nil {
			return err
		}
		return s.audit.AppendTx(txCtx, tx, s.event(subjectID, ActionDelete, "success", requestID, members, ""))
	})
}

// ExcludedNode explains why a member cannot currently receive traffic.
type ExcludedNode struct {
	NodeID string
	Reason string
}

// DispatchPreview computes live eligibility. Gateway eligibility is always
// required; when the eligibility port is absent nothing is eligible.
type DispatchPreview struct {
	ServiceGroupID  string
	GatewayEligible bool
	EligibleNodeIDs []string
	ExcludedNodes   []ExcludedNode
	Policy          string
}

func (s *Service) Preview(ctx context.Context, subjectID, groupID string) (DispatchPreview, error) {
	record, err := s.GetVisible(ctx, subjectID, groupID)
	if err != nil {
		return DispatchPreview{}, err
	}
	preview := DispatchPreview{
		ServiceGroupID:  groupID,
		EligibleNodeIDs: []string{},
		ExcludedNodes:   []ExcludedNode{},
		Policy:          "online && healthy && !paused && gateway_eligible",
	}
	if s.eligibility != nil {
		eligible, err := s.eligibility.GatewayEligible(ctx, groupID)
		if err != nil {
			return DispatchPreview{}, err
		}
		preview.GatewayEligible = eligible
	}
	for _, member := range record.Members {
		if err := ctx.Err(); err != nil {
			return DispatchPreview{}, err
		}
		reason := s.exclusionReason(ctx, preview.GatewayEligible, member)
		if reason != "" {
			preview.ExcludedNodes = append(preview.ExcludedNodes, ExcludedNode{NodeID: member.NodeID, Reason: reason})
			continue
		}
		preview.EligibleNodeIDs = append(preview.EligibleNodeIDs, member.NodeID)
	}
	sort.Strings(preview.EligibleNodeIDs)
	sort.Slice(preview.ExcludedNodes, func(i, j int) bool {
		return preview.ExcludedNodes[i].NodeID < preview.ExcludedNodes[j].NodeID
	})
	return preview, nil
}

func (s *Service) exclusionReason(ctx context.Context, gatewayEligible bool, member Member) string {
	if member.Paused {
		return "paused"
	}
	if !gatewayEligible {
		return "gateway_unverified"
	}
	node, err := s.nodes.Get(ctx, member.NodeID)
	if err != nil {
		return "node_unavailable"
	}
	if node.Status != nodes.StatusOnline {
		return "offline"
	}
	if s.eligibility != nil {
		healthy, reason, err := s.eligibility.NodeHealthy(ctx, member.NodeID)
		if err != nil {
			return "health_unavailable"
		}
		if !healthy {
			if reason == "" {
				reason = "unhealthy"
			}
			return reason
		}
	} else {
		return "health_unverified"
	}
	return ""
}

func (s *Service) requireRequest(requestID string) error {
	if s == nil {
		return errors.New("servicegroups: service is not configured")
	}
	if requestID == "" || len(requestID) > 128 {
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
	if isPureDenial(err) {
		denial := audit.Event{
			SubjectID: subjectID, Action: action, Result: "denied", RequestID: requestID,
			NodeIDs: append([]string(nil), nodeIDs...), Source: audit.SourceWeb,
			ReasonCode: reason, At: s.now(),
		}
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if auditErr := s.audit.Append(auditCtx, denial); auditErr != nil {
			return fmt.Errorf("servicegroups: authorization denied (denial audit failed: %w)", auditErr)
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

func filterVisible(ctx context.Context, scope *auth.NodeReadScope, records []Record) ([]Record, error) {
	out := make([]Record, 0, len(records))
	for _, record := range records {
		visible := true
		for _, member := range record.Members {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			checkErr := scope.CheckNode(ctx, member.NodeID)
			if checkErr == nil {
				continue
			}
			if isPureDenial(checkErr) {
				visible = false
				break
			}
			return nil, checkErr
		}
		if visible {
			out = append(out, record)
		}
	}
	return out, nil
}

// isPureDenial mirrors the nodes package rule: only a single denial chain filters;
// policy failures, cancellation and unknown causes abort the whole list.
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

func memberIDs(members []Member) []string {
	ids := make([]string, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.NodeID)
	}
	sort.Strings(ids)
	return ids
}

func uniqueSorted(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
