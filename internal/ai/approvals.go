package ai

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"
)

var ErrApprovalExpired = errors.New("ai: approval expired or no longer pending")

type ApprovalInput struct {
	ToolName      string
	Parameters    json.RawMessage
	NodeIDs       []string
	ScopeRevision uint64
}

// ApprovalRequest is the management DTO. Internal approver facts are stored but
// not exposed as client-controlled booleans.
type ApprovalRequest struct {
	ID                     string    `json:"id"`
	SessionID              string    `json:"sessionId"`
	State                  string    `json:"state"`
	ToolName               string    `json:"toolName"`
	CanonicalArgumentsHash string    `json:"canonicalArgumentsHash"`
	TargetNodeIDsSnapshot  []string  `json:"targetNodeIdsSnapshot"`
	ScopeRevision          uint64    `json:"scopeRevision"`
	RiskLevel              string    `json:"riskLevel,omitempty"`
	ExpiresAt              time.Time `json:"expiresAt"`
	CreatedBy              string    `json:"createdBy,omitempty"`
	Note                   string    `json:"note,omitempty"`
}

type approvalRecord struct {
	ID            string    `json:"id"`
	SessionID     string    `json:"sessionId"`
	SubjectID     string    `json:"subjectId"`
	ToolName      string    `json:"toolName"`
	Digest        string    `json:"digest"`
	State         string    `json:"state"`
	RiskLevel     string    `json:"riskLevel"`
	NodeIDs       []string  `json:"nodeIds"`
	ScopeRevision uint64    `json:"scopeRevision"`
	RootShell     bool      `json:"rootShell,omitempty"`
	HumanWeb      bool      `json:"humanWeb,omitempty"`
	Administrator bool      `json:"administrator,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	ApproverID    string    `json:"approverId,omitempty"`
	Note          string    `json:"note,omitempty"`
}

func validApprovalDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

func newApprovalID() (string, error) { return sessionID() }

func approvalRisk(tool ToolDefinition) string {
	switch {
	case tool.RootShell:
		return "critical"
	case tool.ReadOnly:
		return "read"
	default:
		return "medium"
	}
}

func approvalRequestView(record approvalRecord, now time.Time) ApprovalRequest {
	state := record.State
	if state == "pending" && !now.Before(record.ExpiresAt) {
		state = "expired"
	}
	return ApprovalRequest{
		ID:                     record.ID,
		SessionID:              record.SessionID,
		State:                  state,
		ToolName:               record.ToolName,
		CanonicalArgumentsHash: record.Digest,
		TargetNodeIDsSnapshot:  slices.Clone(record.NodeIDs),
		ScopeRevision:          record.ScopeRevision,
		RiskLevel:              record.RiskLevel,
		ExpiresAt:              record.ExpiresAt,
		CreatedBy:              record.SubjectID,
		Note:                   record.Note,
	}
}

func validateApprovals(record sessionRecord) error {
	if len(record.Approvals) > 1000 {
		return ErrSessionUnavailable
	}
	for id, approval := range record.Approvals {
		if id != approval.ID || !exactID(id) || approval.SessionID != record.Scope.SessionID || approval.SubjectID != record.Scope.SubjectID || !exactID(approval.ToolName) || !validApprovalDigest(approval.Digest) || approval.ScopeRevision == 0 || approval.ScopeRevision > record.Scope.Revision || len(approval.NodeIDs) == 0 || len(approval.NodeIDs) > 200 || !validIDs(approval.NodeIDs) || approval.CreatedAt.IsZero() || approval.ExpiresAt.IsZero() || !approval.ExpiresAt.After(approval.CreatedAt) || approval.ExpiresAt.Sub(approval.CreatedAt) > 15*time.Minute || approval.RiskLevel != "read" && approval.RiskLevel != "medium" && approval.RiskLevel != "critical" || len(approval.Note) > 1024 {
			return ErrSessionUnavailable
		}
		switch approval.State {
		case "pending":
			if approval.ApproverID != "" {
				return ErrSessionUnavailable
			}
		case "approved", "denied", "expired", "revoked", "consumed":
			if !approval.HumanWeb || !exactID(approval.ApproverID) {
				return ErrSessionUnavailable
			}
		default:
			return ErrSessionUnavailable
		}
	}
	return nil
}

func cloneApproval(record approvalRecord) approvalRecord {
	record.NodeIDs = slices.Clone(record.NodeIDs)
	return record
}

// RequestApproval records a human-created pending request bound to the exact
// canonical tool invocation, target snapshot and scope revision.
func (s *SessionService) RequestApproval(ctx context.Context, sessionID string, input ApprovalInput) (ApprovalRequest, error) {
	if s == nil || !exactID(sessionID) || !exactID(input.ToolName) || input.ScopeRevision == 0 || len(input.NodeIDs) == 0 || len(input.NodeIDs) > 200 || !validIDs(input.NodeIDs) || len(input.Parameters) > 64<<10 || !json.Valid(input.Parameters) || len(bytes.TrimSpace(input.Parameters)) == 0 || bytes.TrimSpace(input.Parameters)[0] != '{' {
		return ApprovalRequest{}, ErrToolDenied
	}
	actor, err := s.actor(ctx, "ai.approvals.create", sessionID, true)
	if err != nil {
		return ApprovalRequest{}, err
	}
	view, err := s.Get(ctx, sessionID)
	if err != nil {
		return ApprovalRequest{}, err
	}
	if view.ScopeRevision != input.ScopeRevision || view.Status != "active" {
		return ApprovalRequest{}, ErrScopeConflict
	}
	call := ToolCall{ToolID: input.ToolName, NodeIDs: slices.Clone(input.NodeIDs), Parameters: bytes.Clone(input.Parameters), ScopeRevision: input.ScopeRevision}
	assessment, err := s.AssessTool(ctx, sessionID, call)
	if err != nil {
		return ApprovalRequest{}, err
	}
	tool, ok := s.tools[input.ToolName]
	if !ok {
		return ApprovalRequest{}, ErrToolDenied
	}
	nodes := slices.Clone(input.NodeIDs)
	sort.Strings(nodes)
	var result ApprovalRequest
	err = s.change(ctx, func(st *sessionState, now time.Time) error {
		if _, err := s.recheck(ctx, actor, "ai.approvals.create", sessionID, true); err != nil {
			return err
		}
		record, exists := st.Sessions[sessionID]
		if !exists || record.Closed || record.Scope.SubjectID != actor.SubjectID {
			return ErrToolDenied
		}
		if record.Scope.Revision != input.ScopeRevision {
			return ErrScopeConflict
		}
		for _, existing := range record.Approvals {
			if existing.Digest == assessment.Digest && existing.ScopeRevision == input.ScopeRevision && slices.Equal(existing.NodeIDs, nodes) && (existing.State == "pending" || existing.State == "approved") {
				result = approvalRequestView(existing, now)
				return nil
			}
		}
		if len(record.Approvals) >= 1000 {
			return ErrSessionUnavailable
		}
		id, err := newApprovalID()
		if err != nil {
			return err
		}
		approval := approvalRecord{
			ID:            id,
			SessionID:     sessionID,
			SubjectID:     actor.SubjectID,
			ToolName:      input.ToolName,
			Digest:        assessment.Digest,
			State:         "pending",
			RiskLevel:     approvalRisk(tool.Definition),
			NodeIDs:       nodes,
			ScopeRevision: input.ScopeRevision,
			RootShell:     tool.Definition.RootShell,
			CreatedAt:     now,
			ExpiresAt:     now.Add(10 * time.Minute),
		}
		record.Approvals[id] = approval
		st.Sessions[sessionID] = record
		result = approvalRequestView(approval, now)
		return s.audit(st, record, "ai.approvals.create", now)
	})
	if err != nil {
		return ApprovalRequest{}, err
	}
	return result, nil
}

// DecideApproval is the human Web decision. Every approval requires a current
// authenticated administrator; root shell execution additionally requires that
// fact at consumption time.
func (s *SessionService) DecideApproval(ctx context.Context, sessionID, approvalID, decision, note string) (ApprovalRequest, error) {
	if s == nil || !exactID(sessionID) || !exactID(approvalID) || decision != "approve" && decision != "deny" || len(note) > 1024 {
		return ApprovalRequest{}, ErrToolDenied
	}
	actor, err := s.actor(ctx, "ai.approvals.decide", sessionID, true)
	if err != nil {
		return ApprovalRequest{}, err
	}
	var result ApprovalRequest
	err = s.change(ctx, func(st *sessionState, now time.Time) error {
		if _, err := s.recheck(ctx, actor, "ai.approvals.decide", sessionID, true); err != nil {
			return err
		}
		if !actor.Administrator {
			return ErrToolDenied
		}
		record, exists := st.Sessions[sessionID]
		if !exists || record.Closed || record.Scope.SubjectID != actor.SubjectID {
			return ErrToolDenied
		}
		approval, exists := record.Approvals[approvalID]
		if !exists {
			return ErrToolDenied
		}
		if approval.State != "pending" {
			return ErrApprovalExpired
		}
		if !now.Before(approval.ExpiresAt) {
			return ErrApprovalExpired
		}
		approval.HumanWeb = true
		approval.Administrator = actor.Administrator
		approval.ApproverID = actor.SubjectID
		approval.Note = note
		action := "ai.approvals.deny"
		if decision == "approve" {
			approval.State = "approved"
			action = "ai.approvals.approve"
		} else {
			approval.State = "denied"
		}
		record.Approvals[approvalID] = approval
		st.Sessions[sessionID] = record
		result = approvalRequestView(approval, now)
		return s.audit(st, record, action, now)
	})
	if err != nil {
		return ApprovalRequest{}, err
	}
	return result, nil
}

// Execute assesses the tool again, consumes a required approval atomically and
// only then calls the sole task executor. A later task denial does not refund
// the approval; the human must approve a fresh attempt.
func (s *SessionService) Execute(ctx context.Context, sessionID string, input ToolExecutionInput, approvalID string, executor ToolExecutor) (ToolExecutionResult, error) {
	if s == nil || !exactID(sessionID) || executor == nil || input.ScopeRevision == 0 || len(input.NodeIDs) == 0 || len(input.NodeIDs) > 200 || !validIDs(input.NodeIDs) || len(input.Parameters) > 64<<10 || !json.Valid(input.Parameters) || len(bytes.TrimSpace(input.Parameters)) == 0 || bytes.TrimSpace(input.Parameters)[0] != '{' {
		return ToolExecutionResult{}, ErrToolDenied
	}
	actor, err := s.actor(ctx, "ai.tools.execute", sessionID, true)
	if err != nil {
		return ToolExecutionResult{}, err
	}
	view, err := s.Get(ctx, sessionID)
	if err != nil {
		return ToolExecutionResult{}, err
	}
	if view.ScopeRevision != input.ScopeRevision || view.Status != "active" {
		return ToolExecutionResult{}, ErrScopeConflict
	}
	call := ToolCall{ToolID: input.ToolName, NodeIDs: slices.Clone(input.NodeIDs), Parameters: bytes.Clone(input.Parameters), ScopeRevision: input.ScopeRevision}
	assessment, err := s.AssessTool(ctx, sessionID, call)
	if err != nil {
		return ToolExecutionResult{}, err
	}
	tool, ok := s.tools[input.ToolName]
	if !ok {
		return ToolExecutionResult{}, ErrToolDenied
	}
	if assessment.RequiresApproval {
		if !exactID(approvalID) {
			return ToolExecutionResult{}, ErrApprovalRequired
		}
		nodes := slices.Clone(input.NodeIDs)
		sort.Strings(nodes)
		err = s.change(ctx, func(st *sessionState, now time.Time) error {
			if _, err := s.recheck(ctx, actor, "ai.tools.execute", sessionID, true); err != nil {
				return err
			}
			record, exists := st.Sessions[sessionID]
			if !exists || record.Closed || record.Scope.SubjectID != actor.SubjectID {
				return ErrToolDenied
			}
			if record.Scope.Revision != input.ScopeRevision {
				return ErrScopeConflict
			}
			approval, exists := record.Approvals[approvalID]
			if !exists || approval.State != "approved" || approval.Digest != assessment.Digest || approval.ScopeRevision != input.ScopeRevision || !slices.Equal(approval.NodeIDs, nodes) || !now.Before(approval.ExpiresAt) || !approval.HumanWeb || !exactID(approval.ApproverID) {
				return ErrApprovalExpired
			}
			if approval.RootShell && (!approval.Administrator || !actor.Administrator) {
				return ErrApprovalRequired
			}
			approval.State = "consumed"
			record.Approvals[approvalID] = approval
			st.Sessions[sessionID] = record
			return s.audit(st, record, "ai.approvals.consume", now)
		})
		if err != nil {
			return ToolExecutionResult{}, err
		}
	}
	result, err := executor(ctx, ToolExecutionRequest{
		SessionID:     sessionID,
		ToolID:        tool.Definition.ID,
		NodeIDs:       slices.Clone(assessment.Call.NodeIDs),
		Parameters:    bytes.Clone(assessment.Call.Parameters),
		ScopeRevision: input.ScopeRevision,
		RootShell:     tool.Definition.RootShell,
	})
	if err != nil {
		if errors.Is(err, ErrToolDenied) || errors.Is(err, ErrScopeConflict) {
			return ToolExecutionResult{}, err
		}
		return ToolExecutionResult{}, ErrExecutionUnavailable
	}
	if result.TaskID == "" || result.State == "" {
		return ToolExecutionResult{}, ErrExecutionUnavailable
	}
	return result, nil
}

// ToolExecutionInput is the HTTP-facing execution request.
type ToolExecutionInput struct {
	ToolName      string
	Parameters    json.RawMessage
	NodeIDs       []string
	ScopeRevision uint64
}
