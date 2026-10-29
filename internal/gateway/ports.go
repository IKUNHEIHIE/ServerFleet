package gateway

import "context"

// NodeRef is the owner-provided availability answer. It contains no inventory
// fields beyond identity and current liveness.
type NodeRef struct {
	ID     string
	Online bool
}

// NodeLookup is implemented by composition using the nodes owner service. The
// bool reports existence. A nil lookup is rejected by NewService.
type NodeLookup interface {
	LookupNode(ctx context.Context, nodeID string) (NodeRef, bool, error)
}

// ServiceGroupRef is the owner-provided existence and membership revision
// snapshot used when a route targets a service group.
type ServiceGroupRef struct {
	ID       string
	Revision uint64
}

// ServiceGroupLookup validates route targets without importing service group
// storage. The bool reports existence.
type ServiceGroupLookup interface {
	LookupServiceGroup(ctx context.Context, serviceGroupID string) (ServiceGroupRef, bool, error)
}

// RouteSpec is the exact proposed route handed to the injected checker. It
// contains no credentials.
type RouteSpec struct {
	GatewayID            string
	NodeID               string
	Domain               string
	Upstream             string
	ServiceGroupID       string
	ServiceGroupRevision uint64
	HTTPS                bool
}

// CheckResult is the checker's answer. PortConflict is reported separately so
// callers receive a fixed, distinguishable error.
type CheckResult struct {
	Passed       bool
	PortConflict bool
}

// Checker performs the health/port verification required before a route may be
// enabled. A nil Checker blocks every enable.
type Checker interface {
	CheckRoute(ctx context.Context, spec RouteSpec) (CheckResult, error)
}

// TaskRequest is the narrow view of one unified task submission. Composition
// adapts it to tasks.Submit; this package has no direct execution path.
type TaskRequest struct {
	SubjectID string
	RequestID string
	ActionID  string

	ResourceID string
	NodeIDs    []string
	Params     map[string]string

	// IdempotencyKey is stable for retries of the same API request.
	IdempotencyKey string
}

// TaskSubmitter creates exactly one unified task and returns its id. A nil
// submitter is rejected by NewService.
type TaskSubmitter interface {
	Submit(ctx context.Context, request TaskRequest) (string, error)
}

// Provision task parameter keys. Params are opaque to the task pipeline.
const (
	ParamGatewayID        = "gateway_id"
	ParamExistingSite     = "existing_site"
	ParamTakeoverApproved = "takeover_approved"
	ParamApprovalRef      = "approval_ref"
)
