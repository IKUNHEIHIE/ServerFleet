package gateway

import "time"

// Status is the lifecycle state of a managed gateway.
type Status string

const (
	StatusPending     Status = "pending"
	StatusInstalling  Status = "installing"
	StatusVerifying   Status = "verifying"
	StatusReady       Status = "ready"
	StatusFailed      Status = "failed"
	StatusMaintenance Status = "maintenance"
)

// Valid reports whether the status is one of the modeled gateway states.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusInstalling, StatusVerifying, StatusReady, StatusFailed, StatusMaintenance:
		return true
	default:
		return false
	}
}

// GatewayNode is one managed Nginx gateway bound to a node. Flags and status are
// the only persisted authority; scheduling eligibility is always derived from a
// fresh node lookup and never stored.
type GatewayNode struct {
	ID          string
	NodeID      string
	Name        string
	Region      string
	CountryCode string
	Status      Status

	NginxInstalled bool
	ChecksPassed   bool
	Enabled        bool

	// LastError is redacted before persistence.
	LastError string
	// NginxVersion is the last observed installed version, not a claim of health.
	NginxVersion string

	Revision  uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SchedulingEligible derives admission from an explicit current liveness answer
// plus the persisted readiness flags. A caller that does not know whether the node
// is online must pass false.
func (g GatewayNode) SchedulingEligible(online bool) bool {
	return online &&
		g.Status == StatusReady &&
		g.NginxInstalled &&
		g.ChecksPassed &&
		g.Enabled
}

// GatewayRoute is one domain served by a gateway. Exactly one of Upstream or
// ServiceGroupID is set. ServiceGroupRevision is a snapshot of the membership
// revision observed when the route was written.
type GatewayRoute struct {
	ID        string
	GatewayID string
	Domain    string

	Upstream             string
	ServiceGroupID       string
	ServiceGroupRevision uint64

	HTTPS   bool
	Enabled bool

	Revision  uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CreateRouteInput carries the create-only id plus the shared route fields.
type CreateRouteInput struct {
	ID             string
	Domain         string
	Upstream       string
	ServiceGroupID string
	HTTPS          bool
	Enabled        bool
}

// UpdateRouteInput cannot change the owning gateway or the route id.
type UpdateRouteInput struct {
	Domain         string
	Upstream       string
	ServiceGroupID string
	HTTPS          bool
	Enabled        bool
}

// CreateGatewayInput enrolls a gateway for an existing node. It never accepts
// status or check flags: those are produced by provisioning and checks.
type CreateGatewayInput struct {
	ID          string
	NodeID      string
	Name        string
	Region      string
	CountryCode string
	Enabled     bool
}

// UpdateGatewayInput changes operator-owned metadata and the admission intent.
type UpdateGatewayInput struct {
	Name        string
	Region      string
	CountryCode string
	Enabled     bool
}

// CheckReport is the outcome of a trusted Nginx/port verification. Error text is
// redacted before persistence.
type CheckReport struct {
	NginxInstalled bool
	ChecksPassed   bool
	NginxVersion   string
	Error          string
}

// ProvisionInput controls one approved provisioning request. Adopting an
// existing site is rejected unless both TakeoverApproved is true and ApprovalRef
// is non-empty.
type ProvisionInput struct {
	ExistingSite     bool
	TakeoverApproved bool
	ApprovalRef      string
}

// ProvisionStatus is the durable state of one gateway provision task.
type ProvisionStatus string

const (
	ProvisionQueued    ProvisionStatus = "queued"
	ProvisionRunning   ProvisionStatus = "running"
	ProvisionCompleted ProvisionStatus = "completed"
	ProvisionFailed    ProvisionStatus = "failed"
)

// Valid reports whether the provision status is modeled.
func (s ProvisionStatus) Valid() bool {
	switch s {
	case ProvisionQueued, ProvisionRunning, ProvisionCompleted, ProvisionFailed:
		return true
	default:
		return false
	}
}

// Terminal reports whether no further reconciliation may change the row.
func (s ProvisionStatus) Terminal() bool {
	return s == ProvisionCompleted || s == ProvisionFailed
}

// StepStatus is the state of one reported provisioning step.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepSucceeded StepStatus = "succeeded"
	StepFailed    StepStatus = "failed"
)

// Valid reports whether the step status is modeled.
func (s StepStatus) Valid() bool {
	switch s {
	case StepPending, StepRunning, StepSucceeded, StepFailed:
		return true
	default:
		return false
	}
}

// ProvisionStep is one ordered step report. Message is redacted before storage.
type ProvisionStep struct {
	Name    string     `json:"name"`
	Status  StepStatus `json:"status"`
	Message string     `json:"message,omitempty"`
}

// Provision is the durable reconciliation row for one unified task. The task is
// the only execution channel; this row records what that task reported.
type Provision struct {
	ID         string
	GatewayID  string
	TaskID     string
	Status     ProvisionStatus
	Steps      []ProvisionStep
	Error      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
}
