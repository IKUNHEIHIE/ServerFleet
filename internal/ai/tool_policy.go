// Package ai owns AI resource and approval policy; it has no Agent connection.
package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrToolDenied = errors.New("ai: tool denied")
var ErrApprovalRequired = errors.New("ai: human approval required")

type ApprovalMode string

const (
	EveryCall  ApprovalMode = "each"
	LowRisk    ApprovalMode = "auto"
	FullAccess ApprovalMode = "full"
)

// Scope is a server-owned saved snapshot. NewScope always starts empty. Never
// deserialize a model's JSON into this authoritative value. Persistent mutations
// and approval consumption must share the reviewed task/store transaction.
type Scope struct {
	SessionID string
	SubjectID string
	Revision  uint64
	Mode      ApprovalMode
	FullUntil time.Time
	FullSince time.Time
	Nodes     []string
	Groups    map[string][]string
}

func NewScope(sessionID, subjectID string) (Scope, error) {
	if !exactID(sessionID) || !exactID(subjectID) {
		return Scope{}, ErrToolDenied
	}
	return Scope{SessionID: sessionID, SubjectID: subjectID, Revision: 1, Mode: EveryCall, Groups: map[string][]string{}}, nil
}

// ToolDefinition must come from a fixed server registry, including classification
// and plugin identity. User/model declarations cannot downgrade risk or turn a
// secret export into a normal tool. No tool can expose platform root secrets.
type ToolDefinition struct {
	ID            string
	ReadOnly      bool
	AutoAllowed   bool
	RootShell     bool
	RequiresHuman bool
	SecretExport  bool
	PluginID      string
}
type ToolCall struct {
	ToolID        string
	NodeIDs       []string
	Parameters    json.RawMessage
	ScopeRevision uint64
}
type CurrentAuthority struct {
	SubjectID string
	// Already revalidated current user action permissions and exact target grants.
	AllowedNodes   []string
	AllowedTools   []string
	EnabledPlugins []string
	GroupMembers   map[string][]string
}
type Approval struct {
	ID            string
	SessionID     string
	SubjectID     string
	Digest        string
	ScopeRevision uint64
	Expires       time.Time
	Consumed      bool
	// These facts MUST be resolved by the trusted human approval service from a
	// current authenticated Web actor, never accepted as client boolean claims.
	HumanWeb      bool
	Administrator bool
}

func exactID(id string) bool {
	return id != "" && len(id) <= 128 && utf8.ValidString(id) && !strings.ContainsAny(id, "*?[]") && !strings.ContainsFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}
func validIDs(ids []string) bool {
	seen := map[string]bool{}
	for _, id := range ids {
		if !exactID(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// BindCall hashes the full typed invocation and frozen target set. Node order
// does not change its meaning; parameter bytes must already be canonicalized by
// the registered schema, not supplied as a claim of canonicality by the model.
func BindCall(scope Scope, call ToolCall) (string, error) {
	if !exactID(scope.SessionID) || !exactID(scope.SubjectID) || scope.Revision == 0 || !exactID(call.ToolID) || len(call.NodeIDs) == 0 || !validIDs(call.NodeIDs) || len(call.Parameters) > 64<<10 || !json.Valid(call.Parameters) {
		return "", ErrToolDenied
	}
	nodes := slices.Clone(call.NodeIDs)
	slices.Sort(nodes)
	data, err := json.Marshal(struct {
		Session, Subject, Tool string
		Revision               uint64
		Nodes                  []string
		Parameters             []byte
	}{scope.SessionID, scope.SubjectID, call.ToolID, scope.Revision, nodes, call.Parameters})
	if err != nil {
		return "", ErrToolDenied
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// EvaluateTool is a side-effect-free policy component, not an execution ticket.
// Call it at planning AND every dispatch/stage with current authoritative facts.
// A success must be followed by atomic approval consumption and the sole task
// pipeline. It grants no direct transport/DB/HTTP/SSH access.
func EvaluateTool(scope Scope, call ToolCall, tool ToolDefinition, current CurrentAuthority, approval *Approval, now time.Time) error {
	if now.IsZero() || current.SubjectID != scope.SubjectID || call.ScopeRevision != scope.Revision || call.ToolID != tool.ID || tool.SecretExport || !slices.Contains(current.AllowedTools, tool.ID) || !validIDs(current.AllowedTools) || !validIDs(current.AllowedNodes) || !validIDs(scope.Nodes) {
		return ErrToolDenied
	}
	if tool.PluginID != "" && !slices.Contains(current.EnabledPlugins, tool.PluginID) {
		return ErrToolDenied
	}
	digest, err := BindCall(scope, call)
	if err != nil {
		return err
	}
	effective := map[string]bool{}
	for _, id := range scope.Nodes {
		effective[id] = true
	}
	for group, snapshot := range scope.Groups {
		members, ok := current.GroupMembers[group]
		if !exactID(group) || !ok || !validIDs(snapshot) || !validIDs(members) {
			return ErrToolDenied
		}
		for _, id := range snapshot {
			if slices.Contains(members, id) {
				effective[id] = true
			}
		}
	}
	for _, id := range call.NodeIDs {
		if !effective[id] || !slices.Contains(current.AllowedNodes, id) {
			return ErrToolDenied
		}
	}
	needsApproval := true
	switch scope.Mode {
	case EveryCall:
	case LowRisk:
		needsApproval = !(tool.ReadOnly && tool.AutoAllowed)
	case FullAccess:
		// Full access is only a server-recorded, bounded confirmation. Expiry
		// restores per-call approval, never widens the authorization intersection.
		if !scope.FullSince.IsZero() && !now.Before(scope.FullSince) && now.Before(scope.FullUntil) && scope.FullUntil.Sub(scope.FullSince) > 0 && scope.FullUntil.Sub(scope.FullSince) <= time.Hour {
			needsApproval = false
		}
	default:
		return ErrToolDenied
	}
	if tool.RootShell || tool.RequiresHuman {
		needsApproval = true
	}
	if !needsApproval {
		return nil
	}
	if approval == nil || !exactID(approval.ID) || approval.Consumed || !approval.HumanWeb || approval.SessionID != scope.SessionID || approval.SubjectID != scope.SubjectID || approval.ScopeRevision != scope.Revision || approval.Digest != digest || !now.Before(approval.Expires) {
		return ErrApprovalRequired
	}
	if tool.RootShell && !approval.Administrator {
		return ErrApprovalRequired
	}
	return nil
}
