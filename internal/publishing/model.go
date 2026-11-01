package publishing

import "time"

// PlanStatus is the durable state of one publication plan.
type PlanStatus string

const (
	PlanQueued         PlanStatus = "queued"
	PlanRunning        PlanStatus = "running"
	PlanCompleted      PlanStatus = "completed"
	PlanPartialFailure PlanStatus = "partial_failure"
	PlanFailed         PlanStatus = "failed"
	PlanCanceled       PlanStatus = "canceled"
)

// Valid reports whether the plan status is modeled.
func (s PlanStatus) Valid() bool {
	switch s {
	case PlanQueued, PlanRunning, PlanCompleted, PlanPartialFailure, PlanFailed, PlanCanceled:
		return true
	default:
		return false
	}
}

// Terminal reports whether Run must refuse further execution.
func (s PlanStatus) Terminal() bool {
	return s == PlanCompleted || s == PlanCanceled
}

// StageKind is one fixed stage in publication order.
type StageKind string

const (
	StageDNS         StageKind = "dns"
	StageCertificate StageKind = "certificate"
	StageGateway     StageKind = "gateway"
)

// StageKinds returns the fixed execution order.
func StageKinds() []StageKind {
	return []StageKind{StageDNS, StageCertificate, StageGateway}
}

// Valid reports whether the stage is modeled.
func (k StageKind) Valid() bool {
	switch k {
	case StageDNS, StageCertificate, StageGateway:
		return true
	default:
		return false
	}
}

// StageStatus is the durable state of one stage row.
type StageStatus string

const (
	StagePending   StageStatus = "pending"
	StageRunning   StageStatus = "running"
	StageSucceeded StageStatus = "succeeded"
	StageFailed    StageStatus = "failed"
	StageSkipped   StageStatus = "skipped"
	// StageRolledBack means the owning provider compensation ran. It does not
	// imply that DNS resolvers or other caches have already expired old data.
	StageRolledBack StageStatus = "rolled_back"
)

// Valid reports whether the stage status is modeled.
func (s StageStatus) Valid() bool {
	switch s {
	case StagePending, StageRunning, StageSucceeded, StageFailed, StageSkipped, StageRolledBack:
		return true
	default:
		return false
	}
}

// Executed reports whether the stage may have produced external side effects.
func (s StageStatus) Executed() bool {
	return s == StageSucceeded || s == StageFailed
}

// Plan is one publication for one service. ExpectedGroupRevision is a snapshot and
// Run revalidates it before executing.
type Plan struct {
	ID                    string
	Name                  string
	Domain                string
	ServiceGroupID        string
	ExpectedGroupRevision uint64
	GatewayIDs            []string
	ZoneID                string
	CertificateID         string
	HTTPS                 bool
	TargetRevision        uint64
	// TargetsPersisted is a read-only compatibility flag: false means the
	// deployment has not accepted ds/0081 and target fields were not stored.
	TargetsPersisted bool
	Status           PlanStatus
	CurrentStage     StageKind
	CreatedBy        string
	IdempotencyKey   string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Stage is one durable stage row. ResultSummary and error codes are redacted;
// attempts counts executions, including retries.
type Stage struct {
	PlanID        string
	Kind          StageKind
	Status        StageStatus
	Attempts      int
	ResultSummary string
	ErrorCode     string
	StartedAt     time.Time
	FinishedAt    time.Time
}

// CreateInput is the user-supplied plan description. The service group must exist
// and its current revision must match ExpectedGroupRevision.
type CreateInput struct {
	ID                    string
	Name                  string
	Domain                string
	ServiceGroupID        string
	ExpectedGroupRevision uint64
	GatewayIDs            []string
	ZoneID                string
	CertificateID         string
	HTTPS                 bool
	TargetRevision        uint64
	IdempotencyKey        string
}

// PreviewInput is a server-side preflight request. It never creates a plan.
type PreviewInput struct {
	ServiceGroupID        string
	ExpectedGroupRevision uint64
	Domain                string
	GatewayIDs            []string
	ZoneID                string
	CertificateID         string
	HTTPS                 bool
	TargetRevision        uint64
}

// PreviewCheck is one preflight observation. Only facts the domain can verify
// locally are reported; provider health is not invented.
type PreviewCheck struct {
	Name   string
	Passed bool
	Reason string
}

// Preview is the honest publication preflight answer.
type Preview struct {
	Eligible      bool
	GroupRevision uint64
	TargetNodeIDs []string
	Checks        []PreviewCheck
	Warnings      []string
}

// PlanDetail is a plan with its persisted stage rows.
type PlanDetail struct {
	Plan   Plan
	Stages []Stage
}

// Stable stage error codes. Raw provider errors are never stored.
const (
	ErrorCodePublishError        = "publish_error"
	ErrorCodeInvalidResult       = "invalid_result"
	ErrorCodeAuthorizationDenied = "authorization_denied"
	ErrorCodeRevisionConflict    = "revision_conflict"
	ErrorCodeGroupLookupFailed   = "group_lookup_failed"
)
