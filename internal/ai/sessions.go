package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"
)

var ErrSessionUnavailable = errors.New("ai: session source unavailable")
var ErrScopeConflict = errors.New("ai: scope revision conflict")

type SessionActor struct {
	SubjectID               string
	HumanWeb, CanFullAccess bool
}
type SessionAccess func(context.Context, string, string) (SessionActor, error)
type SessionAuthorityReader func(context.Context, string, string) (CurrentAuthority, error)
type SessionStore interface {
	View(context.Context, func([]byte) error) error
	Update(context.Context, func([]byte) ([]byte, error)) error
}
type RegisteredTool struct {
	Definition   ToolDefinition
	Canonicalize func(json.RawMessage) (json.RawMessage, error)
}
type ResourceGrantInput struct {
	ResourceType   string   `json:"resourceType"`
	ResourceID     string   `json:"resourceId"`
	AllowedActions []string `json:"allowedActions"`
}
type ResourceGrant struct {
	GrantID        string    `json:"grantId"`
	ResourceType   string    `json:"resourceType"`
	ResourceID     string    `json:"resourceId"`
	MemberNodeIDs  []string  `json:"memberNodeIdsSnapshot,omitempty"`
	AllowedActions []string  `json:"allowedActions"`
	GrantedAt      time.Time `json:"grantedAt"`
	GrantedBy      string    `json:"grantedBy"`
}
type ApprovalPolicy struct {
	SessionID            string     `json:"sessionId"`
	Mode                 string     `json:"mode"`
	EffectiveMode        string     `json:"effectiveMode"`
	ExpiresAt            *time.Time `json:"expiresAt"`
	ScopeRevision        uint64     `json:"scopeRevision"`
	MaxFullAccessSeconds int        `json:"maxFullAccessSeconds"`
	EnforcedRestrictions []string   `json:"enforcedRestrictions"`
}
type SessionView struct {
	ID             string          `json:"id"`
	ScopeRevision  uint64          `json:"scopeRevision"`
	ResourceGrants []ResourceGrant `json:"resourceGrants"`
	AllowedNodeIDs []string        `json:"allowedNodeIds"`
	Status         string          `json:"status"`
	CreatedBy      string          `json:"createdBy"`
	CreatedAt      time.Time       `json:"createdAt"`
	ApprovalPolicy ApprovalPolicy  `json:"approvalPolicy"`
}
type sessionRecord struct {
	Scope        Scope
	Grants       map[string]ResourceGrant
	Created      time.Time
	Closed       bool
	Conversation *conversationRecord `json:",omitempty"`
}
type sessionAudit struct {
	SubjectID, Action, SessionID string
	MessageID, Result            string `json:",omitempty"`
	At                           time.Time
	ScopeRevision                uint64
	NodeIDs                      []string
}
type sessionState struct {
	Version    int
	ClockFloor time.Time
	Sessions   map[string]sessionRecord
	Outbox     []sessionAudit
}
type SessionService struct {
	store     SessionStore
	access    SessionAccess
	authority SessionAuthorityReader
	tools     map[string]RegisteredTool
	now       func() time.Time
}

func NewSessionService(store SessionStore, access SessionAccess, authority SessionAuthorityReader, tools []RegisteredTool) (*SessionService, error) {
	if store == nil || access == nil || authority == nil || len(tools) == 0 || len(tools) > 256 {
		return nil, ErrSessionUnavailable
	}
	registry := map[string]RegisteredTool{}
	for _, tool := range tools {
		id := tool.Definition.ID
		if !exactID(id) || tool.Canonicalize == nil || registry[id].Definition.ID != "" {
			return nil, ErrToolDenied
		}
		registry[id] = tool
	}
	return &SessionService{store, access, authority, registry, time.Now}, nil
}
func sessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", ErrSessionUnavailable
	}
	return hex.EncodeToString(b[:]), nil
}
func decodeSessions(raw []byte) (*sessionState, error) {
	st := &sessionState{}
	if len(raw) > 8<<20 || len(raw) > 0 && (json.Unmarshal(raw, st) != nil || st.Version != 1) {
		return nil, ErrSessionUnavailable
	}
	st.Version = 1
	if st.Sessions == nil {
		st.Sessions = map[string]sessionRecord{}
	}
	if len(st.Sessions) > 1000 || len(st.Outbox) > 10000 {
		return nil, ErrSessionUnavailable
	}
	for id, record := range st.Sessions {
		scope := record.Scope
		if id != scope.SessionID || !exactID(id) || !exactID(scope.SubjectID) || scope.Revision == 0 || record.Created.IsZero() || len(record.Grants) > 1000 {
			return nil, ErrSessionUnavailable
		}
		if scope.Mode != EveryCall && scope.Mode != LowRisk && scope.Mode != FullAccess {
			return nil, ErrSessionUnavailable
		}
		if validateConversation(id, record.Conversation) != nil {
			return nil, ErrSessionUnavailable
		}
		groups := map[string]bool{}
		for key, g := range record.Grants {
			if key != g.GrantID || !exactID(key) || !exactID(g.ResourceID) || !validIDs(g.AllowedActions) || len(g.AllowedActions) == 0 || !validIDs(g.MemberNodeIDs) || len(g.MemberNodeIDs) > 1000 || g.ResourceType != "node" && g.ResourceType != "service_group" || g.GrantedBy != scope.SubjectID || g.GrantedAt.IsZero() {
				return nil, ErrSessionUnavailable
			}
			if g.ResourceType == "service_group" {
				if groups[g.ResourceID] {
					return nil, ErrSessionUnavailable
				}
				groups[g.ResourceID] = true
			}
		}
		if record.Grants == nil {
			record.Grants = map[string]ResourceGrant{}
			st.Sessions[id] = record
		}
	}
	return st, nil
}
func (s *SessionService) actor(ctx context.Context, action, id string, human bool) (SessionActor, error) {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return SessionActor{}, ErrSessionUnavailable
	}
	actor, err := s.access(ctx, action, id)
	if errors.Is(err, ErrToolDenied) {
		return SessionActor{}, ErrToolDenied
	}
	if err != nil || !exactID(actor.SubjectID) {
		return SessionActor{}, ErrSessionUnavailable
	}
	if human && !actor.HumanWeb {
		return SessionActor{}, ErrToolDenied
	}
	return actor, nil
}
func (s *SessionService) recheck(ctx context.Context, actor SessionActor, action, id string, human bool) (SessionActor, error) {
	fresh, err := s.actor(ctx, action, id, human)
	if err != nil {
		return SessionActor{}, err
	}
	if fresh.SubjectID != actor.SubjectID {
		return SessionActor{}, ErrToolDenied
	}
	return fresh, nil
}
func (s *SessionService) change(ctx context.Context, fn func(*sessionState, time.Time) error) error {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return ErrSessionUnavailable
	}
	err := s.store.Update(ctx, func(b []byte) ([]byte, error) {
		st, err := decodeSessions(b)
		if err != nil {
			return nil, err
		}
		now := s.now().UTC()
		if now.IsZero() || now.Before(st.ClockFloor) {
			return nil, ErrSessionUnavailable
		}
		st.ClockFloor = now
		if err = fn(st, now); err != nil {
			return nil, err
		}
		out, err := json.Marshal(st)
		if err != nil || len(out) > 8<<20 || ctx.Err() != nil {
			return nil, ErrSessionUnavailable
		}
		return out, nil
	})
	for _, kind := range []error{ErrToolDenied, ErrSessionUnavailable, ErrScopeConflict, ErrApprovalRequired, ErrGenerationUnknown, ErrModelUnavailable} {
		if errors.Is(err, kind) {
			return kind
		}
	}
	if err != nil || ctx.Err() != nil {
		return ErrSessionUnavailable
	}
	return nil
}
func grantNodes(record sessionRecord) []string {
	nodes := []string{}
	for _, g := range record.Grants {
		values := g.MemberNodeIDs
		if g.ResourceType == "node" {
			values = []string{g.ResourceID}
		}
		for _, node := range values {
			if !slices.Contains(nodes, node) {
				nodes = append(nodes, node)
			}
		}
	}
	sort.Strings(nodes)
	return nodes
}
func (s *SessionService) audit(st *sessionState, record sessionRecord, action string, now time.Time, previous ...string) error {
	if len(st.Outbox) >= 10000 {
		return ErrSessionUnavailable
	}
	nodes := grantNodes(record)
	for _, n := range previous {
		if !slices.Contains(nodes, n) {
			nodes = append(nodes, n)
		}
	}
	sort.Strings(nodes)
	st.Outbox = append(st.Outbox, sessionAudit{SubjectID: record.Scope.SubjectID, Action: action, SessionID: record.Scope.SessionID, Result: "success", At: now, ScopeRevision: record.Scope.Revision, NodeIDs: nodes})
	return nil
}
func recordScope(record sessionRecord) Scope {
	scope := record.Scope
	scope.Nodes = []string{}
	scope.Groups = map[string][]string{}
	scope.NodeActions = map[string][]string{}
	scope.GroupActions = map[string][]string{}
	appendActions := func(current, add []string) []string {
		for _, a := range add {
			if !slices.Contains(current, a) {
				current = append(current, a)
			}
		}
		sort.Strings(current)
		return current
	}
	for _, g := range record.Grants {
		if g.ResourceType == "node" {
			if !slices.Contains(scope.Nodes, g.ResourceID) {
				scope.Nodes = append(scope.Nodes, g.ResourceID)
			}
			scope.NodeActions[g.ResourceID] = appendActions(scope.NodeActions[g.ResourceID], g.AllowedActions)
		} else {
			for _, n := range g.MemberNodeIDs {
				if !slices.Contains(scope.Groups[g.ResourceID], n) {
					scope.Groups[g.ResourceID] = append(scope.Groups[g.ResourceID], n)
				}
			}
			scope.GroupActions[g.ResourceID] = appendActions(scope.GroupActions[g.ResourceID], g.AllowedActions)
		}
	}
	sort.Strings(scope.Nodes)
	return scope
}
func (s *SessionService) view(ctx context.Context, record sessionRecord, now time.Time) (SessionView, error) {
	scope := recordScope(record)
	mode := map[ApprovalMode]string{EveryCall: "per_action", LowRisk: "automatic", FullAccess: "full_access"}[scope.Mode]
	view := SessionView{ID: scope.SessionID, ScopeRevision: scope.Revision, ResourceGrants: []ResourceGrant{}, AllowedNodeIDs: []string{}, Status: "active", CreatedBy: scope.SubjectID, CreatedAt: record.Created, ApprovalPolicy: ApprovalPolicy{SessionID: scope.SessionID, Mode: mode, EffectiveMode: mode, ScopeRevision: scope.Revision, MaxFullAccessSeconds: 3600, EnforcedRestrictions: []string{"RESOURCE_AND_ACTION_INTERSECTION", "AI_ROOT_SHELL_HUMAN_ADMIN", "NO_PLATFORM_SECRET_EXPORT", "PLUGIN_ENABLEMENT_REQUIRED"}}}
	if scope.Mode == FullAccess {
		if now.Before(scope.FullUntil) && !now.Before(scope.FullSince) {
			expiry := scope.FullUntil
			view.ApprovalPolicy.ExpiresAt = &expiry
		} else {
			view.ApprovalPolicy.EffectiveMode = "per_action"
		}
	}
	if record.Closed {
		view.Status = "closed"
		view.ApprovalPolicy.Mode = "per_action"
		view.ApprovalPolicy.EffectiveMode = "per_action"
		view.ApprovalPolicy.ExpiresAt = nil
	}
	allowed := map[string]bool{}
	for _, grant := range record.Grants {
		grant.AllowedActions = slices.Clone(grant.AllowedActions)
		grant.MemberNodeIDs = slices.Clone(grant.MemberNodeIDs)
		view.ResourceGrants = append(view.ResourceGrants, grant)
		if record.Closed {
			continue
		}
		for _, action := range grant.AllowedActions {
			current, err := s.authority(ctx, scope.SubjectID, action)
			if err != nil || current.SubjectID != scope.SubjectID || !validIDs(current.AllowedNodes) || !validIDs(current.AllowedTools) {
				return SessionView{}, ErrSessionUnavailable
			}
			if !slices.Contains(current.AllowedTools, action) {
				continue
			}
			nodes := []string{grant.ResourceID}
			if grant.ResourceType == "service_group" {
				nodes = nil
				members := current.GroupMembers[grant.ResourceID]
				if !validIDs(members) {
					return SessionView{}, ErrSessionUnavailable
				}
				for _, n := range grant.MemberNodeIDs {
					if slices.Contains(members, n) {
						nodes = append(nodes, n)
					}
				}
			}
			for _, n := range nodes {
				if slices.Contains(current.AllowedNodes, n) {
					allowed[n] = true
				}
			}
		}
	}
	for n := range allowed {
		view.AllowedNodeIDs = append(view.AllowedNodeIDs, n)
	}
	sort.Strings(view.AllowedNodeIDs)
	sort.Slice(view.ResourceGrants, func(i, j int) bool { return view.ResourceGrants[i].GrantID < view.ResourceGrants[j].GrantID })
	return view, nil
}
func (s *SessionService) Create(ctx context.Context) (SessionView, error) {
	actor, err := s.actor(ctx, "ai.sessions.create", "", true)
	if err != nil {
		return SessionView{}, err
	}
	var result SessionView
	err = s.change(ctx, func(st *sessionState, now time.Time) error {
		if _, err := s.recheck(ctx, actor, "ai.sessions.create", "", true); err != nil {
			return err
		}
		if len(st.Sessions) >= 1000 {
			return ErrSessionUnavailable
		}
		id, err := sessionID()
		if err != nil {
			return err
		}
		scope, _ := NewScope(id, actor.SubjectID)
		record := sessionRecord{Scope: scope, Grants: map[string]ResourceGrant{}, Created: now}
		st.Sessions[id] = record
		result, err = s.view(ctx, record, now)
		if err != nil {
			return err
		}
		return s.audit(st, record, "ai.sessions.create", now)
	})
	if err != nil {
		return SessionView{}, err
	}
	return result, nil
}
func (s *SessionService) Get(ctx context.Context, id string) (SessionView, error) {
	actor, err := s.actor(ctx, "ai.sessions.read", id, false)
	if err != nil {
		return SessionView{}, err
	}
	var out SessionView
	// Persist observed expiry so a backward clock cannot revive full access.
	err = s.change(ctx, func(st *sessionState, now time.Time) error {
		if _, err := s.recheck(ctx, actor, "ai.sessions.read", id, false); err != nil {
			return err
		}
		record, ok := st.Sessions[id]
		if !ok || record.Scope.SubjectID != actor.SubjectID {
			return ErrToolDenied
		}
		var err error
		out, err = s.view(ctx, record, now)
		return err
	})
	if err != nil {
		return SessionView{}, err
	}
	return out, nil
}
func (s *SessionService) AddGrant(ctx context.Context, id string, expected uint64, input ResourceGrantInput) (SessionView, error) {
	actor, err := s.actor(ctx, "ai.sessions.grant", id, true)
	if err != nil {
		return SessionView{}, err
	}
	input.AllowedActions = slices.Clone(input.AllowedActions)
	if expected == 0 || !exactID(input.ResourceID) || input.ResourceType != "node" && input.ResourceType != "service_group" || len(input.AllowedActions) == 0 || len(input.AllowedActions) > 64 || !validIDs(input.AllowedActions) {
		return SessionView{}, ErrToolDenied
	}
	var result SessionView
	err = s.change(ctx, func(st *sessionState, now time.Time) error {
		if _, err := s.recheck(ctx, actor, "ai.sessions.grant", id, true); err != nil {
			return err
		}
		record, ok := st.Sessions[id]
		if !ok || record.Closed || record.Scope.SubjectID != actor.SubjectID {
			return ErrToolDenied
		}
		if record.Scope.Revision != expected {
			return ErrScopeConflict
		}
		if len(record.Grants) >= 1000 {
			return ErrSessionUnavailable
		}
		if input.ResourceType == "service_group" {
			for _, g := range record.Grants {
				if g.ResourceType == "service_group" && g.ResourceID == input.ResourceID {
					return ErrScopeConflict
				}
			}
		}
		snapshot := []string{}
		for i, action := range input.AllowedActions {
			tool, ok := s.tools[action]
			if !ok || tool.Definition.SecretExport {
				return ErrToolDenied
			}
			current, err := s.authority(ctx, actor.SubjectID, action)
			if err != nil || current.SubjectID != actor.SubjectID || !validIDs(current.AllowedNodes) || !validIDs(current.AllowedTools) {
				return ErrSessionUnavailable
			}
			if !slices.Contains(current.AllowedTools, action) {
				return ErrToolDenied
			}
			nodes := []string{input.ResourceID}
			if input.ResourceType == "service_group" {
				var exists bool
				nodes, exists = current.GroupMembers[input.ResourceID]
				if !exists || len(nodes) > 1000 || !validIDs(nodes) {
					return ErrToolDenied
				}
				if i == 0 {
					snapshot = slices.Clone(nodes)
				} else {
					a, b := slices.Clone(snapshot), slices.Clone(nodes)
					sort.Strings(a)
					sort.Strings(b)
					if !slices.Equal(a, b) {
						return ErrSessionUnavailable
					}
				}
			}
			for _, node := range nodes {
				if !slices.Contains(current.AllowedNodes, node) {
					return ErrToolDenied
				}
			}
		}
		grantID, err := sessionID()
		if err != nil {
			return err
		}
		grant := ResourceGrant{grantID, input.ResourceType, input.ResourceID, snapshot, input.AllowedActions, now, actor.SubjectID}
		record.Grants[grantID] = grant
		record.Scope.Revision++
		if record.Scope.Revision == 0 {
			return ErrSessionUnavailable
		}
		st.Sessions[id] = record
		result, err = s.view(ctx, record, now)
		if err != nil {
			return err
		}
		return s.audit(st, record, "ai.sessions.grant", now)
	})
	if err != nil {
		return SessionView{}, err
	}
	return result, nil
}
func (s *SessionService) RevokeGrant(ctx context.Context, id, grantID string, expected uint64) (SessionView, error) {
	return s.mutate(ctx, id, expected, "ai.sessions.revoke", func(record *sessionRecord, _ SessionActor, _ time.Time) error {
		if _, ok := record.Grants[grantID]; !ok {
			return ErrToolDenied
		}
		delete(record.Grants, grantID)
		return nil
	})
}
func (s *SessionService) SetPolicy(ctx context.Context, id string, expected uint64, mode ApprovalMode, minutes int) (SessionView, error) {
	return s.mutate(ctx, id, expected, "ai.sessions.policy", func(record *sessionRecord, actor SessionActor, now time.Time) error {
		if mode != EveryCall && mode != LowRisk && mode != FullAccess {
			return ErrToolDenied
		}
		if mode == FullAccess {
			if !actor.CanFullAccess || minutes != 15 && minutes != 30 && minutes != 60 {
				return ErrToolDenied
			}
			record.Scope.FullSince = now
			record.Scope.FullUntil = now.Add(time.Duration(minutes) * time.Minute)
		} else {
			if minutes != 0 {
				return ErrToolDenied
			}
			record.Scope.FullSince = time.Time{}
			record.Scope.FullUntil = time.Time{}
		}
		record.Scope.Mode = mode
		return nil
	})
}
func (s *SessionService) Close(ctx context.Context, id string, expected uint64) (SessionView, error) {
	return s.mutate(ctx, id, expected, "ai.sessions.close", func(record *sessionRecord, _ SessionActor, _ time.Time) error {
		record.Closed = true
		record.Scope.Mode = EveryCall
		record.Scope.FullSince = time.Time{}
		record.Scope.FullUntil = time.Time{}
		return nil
	})
}
func (s *SessionService) mutate(ctx context.Context, id string, expected uint64, action string, fn func(*sessionRecord, SessionActor, time.Time) error) (SessionView, error) {
	actor, err := s.actor(ctx, action, id, true)
	if err != nil {
		return SessionView{}, err
	}
	var out SessionView
	err = s.change(ctx, func(st *sessionState, now time.Time) error {
		fresh, err := s.recheck(ctx, actor, action, id, true)
		if err != nil {
			return err
		}
		record, ok := st.Sessions[id]
		if !ok || record.Closed || record.Scope.SubjectID != actor.SubjectID {
			return ErrToolDenied
		}
		if expected == 0 || record.Scope.Revision != expected {
			return ErrScopeConflict
		}
		previousNodes := grantNodes(record)
		if err := fn(&record, fresh, now); err != nil {
			return err
		}
		record.Scope.Revision++
		if record.Scope.Revision == 0 {
			return ErrSessionUnavailable
		}
		st.Sessions[id] = record
		out, err = s.view(ctx, record, now)
		if err != nil {
			return err
		}
		return s.audit(st, record, action, now, previousNodes...)
	})
	if err != nil {
		return SessionView{}, err
	}
	return out, nil
}

type ToolAssessment struct {
	Call             ToolCall
	Digest           string
	RequiresApproval bool
}

// AssessTool is planning only, never an execution/approval consumption ticket.
// The sole task service must repeat current policy and atomically consume its
// approval at submit/dispatch. The model cannot supply ToolDefinition or scope.
func (s *SessionService) AssessTool(ctx context.Context, id string, call ToolCall) (ToolAssessment, error) {
	actor, err := s.actor(ctx, "ai.tools.assess", id, false)
	if err != nil {
		return ToolAssessment{}, err
	}
	tool, ok := s.tools[call.ToolID]
	if !ok {
		return ToolAssessment{}, ErrToolDenied
	}
	call.NodeIDs = slices.Clone(call.NodeIDs)
	call.Parameters = bytes.Clone(call.Parameters)
	canonical, err := tool.Canonicalize(call.Parameters)
	if err != nil {
		return ToolAssessment{}, ErrToolDenied
	}
	call.Parameters = bytes.Clone(canonical)
	var out ToolAssessment
	err = s.change(ctx, func(st *sessionState, now time.Time) error {
		if _, err := s.recheck(ctx, actor, "ai.tools.assess", id, false); err != nil {
			return err
		}
		record, ok := st.Sessions[id]
		if !ok || record.Closed || record.Scope.SubjectID != actor.SubjectID {
			return ErrToolDenied
		}
		scope := recordScope(record)
		current, err := s.authority(ctx, actor.SubjectID, call.ToolID)
		if err != nil {
			return ErrSessionUnavailable
		}
		decision := EvaluateTool(scope, call, tool.Definition, current, nil, now)
		if decision != nil && !errors.Is(decision, ErrApprovalRequired) {
			return ErrToolDenied
		}
		digest, err := BindCall(scope, call)
		if err != nil {
			return err
		}
		out = ToolAssessment{call, digest, errors.Is(decision, ErrApprovalRequired)}
		return nil
	})
	if err != nil {
		return ToolAssessment{}, err
	}
	return out, nil
}
