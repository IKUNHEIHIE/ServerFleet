package tasks

import (
	"errors"
	"time"
)

// TaskStatus is the persisted lifecycle state of the unified task pipeline.
type TaskStatus string

const (
	StatusAwaitingApproval TaskStatus = "awaiting_approval"
	StatusQueued           TaskStatus = "queued"
	StatusRunning          TaskStatus = "running"
	StatusCancelling       TaskStatus = "cancelling"
	StatusCompleted        TaskStatus = "completed"
	StatusPartialFailure   TaskStatus = "partial_failure"
	StatusFailed           TaskStatus = "failed"
	StatusCanceled         TaskStatus = "canceled"
	StatusExpired          TaskStatus = "expired"
	StatusUnknown          TaskStatus = "unknown"
)

func (s TaskStatus) Valid() bool {
	switch s {
	case StatusAwaitingApproval, StatusQueued, StatusRunning, StatusCancelling, StatusCompleted,
		StatusPartialFailure, StatusFailed, StatusCanceled, StatusExpired, StatusUnknown:
		return true
	default:
		return false
	}
}

// Terminal reports whether no further execution may occur.
func (s TaskStatus) Terminal() bool {
	switch s {
	case StatusCompleted, StatusPartialFailure, StatusFailed, StatusCanceled, StatusExpired, StatusUnknown:
		return true
	default:
		return false
	}
}

// TargetState is the per-node execution state. Dispatched means a command was
// accepted by the transport but no trusted result exists yet.
type TargetState string

const (
	TargetPending    TargetState = "pending"
	TargetDispatched TargetState = "dispatched"
	TargetSucceeded  TargetState = "succeeded"
	TargetFailed     TargetState = "failed"
	TargetCanceled   TargetState = "canceled"
	TargetTimedOut   TargetState = "timed_out"
	TargetUnknown    TargetState = "unknown"
)

func (s TargetState) Terminal() bool {
	switch s {
	case TargetSucceeded, TargetFailed, TargetCanceled, TargetTimedOut, TargetUnknown:
		return true
	default:
		return false
	}
}

// Target is one frozen node in a task snapshot.
type Target struct {
	NodeID       string
	Position     int
	BatchIndex   int
	State        TargetState
	Code         NodeResultCode
	Message      string
	DispatchedAt time.Time
	FinishedAt   time.Time
}

// Task is the durable unified task. Params is a validated JSON object of string
// values; secrets must be referenced by ID rather than embedded.
type Task struct {
	ID                string
	Name              string
	SubmitterID       string
	ActionID          string
	OperationClass    OperationClass
	Params            string
	IdempotencyKey    string
	Revision          uint64
	Status            TaskStatus
	ApprovalRequired  bool
	PlanHash          string
	BatchIndex        int
	CancelRequested   bool
	TargetType        string
	TargetResourceID  string
	UnstartedDeadline time.Time
	RequestID         string
	FailureReason     string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	StartedAt         time.Time
	FinishedAt        time.Time
	Targets           []Target
}

// Approval is one decision bound to a task revision and plan hash.
type Approval struct {
	TaskID     string
	Revision   uint64
	ApproverID string
	Decision   string
	PlanHash   string
	Reason     string
	DecidedAt  time.Time
}

func (a Approval) Approved() bool { return a.Decision == "approved" }

// Event is one ordered SSE event. Payload is a small JSON object without secrets.
type Event struct {
	Seq     int64
	TaskID  string
	Type    string
	Payload []byte
	At      time.Time
}

// LogEntry is one redacted task log line.
type LogEntry struct {
	ID      int64
	TaskID  string
	NodeID  string
	Level   string
	Message string
	At      time.Time
}

var (
	ErrTaskNotFound      = errors.New("tasks: task not found")
	ErrTaskTerminal      = errors.New("tasks: task is terminal")
	ErrInvalidTask       = errors.New("tasks: invalid task")
	ErrInvalidStatus     = errors.New("tasks: invalid status transition")
	ErrApprovalInvalid   = errors.New("tasks: approval is missing or stale")
	ErrApprovalRequired  = errors.New("tasks: approval is required")
	ErrSelfApproval      = errors.New("tasks: submitter cannot approve own task")
	ErrTargetNotFound    = errors.New("tasks: target not found")
	ErrCursorExpired     = errors.New("tasks: event cursor expired")
	ErrDispatcherAbsent  = errors.New("tasks: transport dispatcher is not configured")
	ErrNodeLookupAbsent  = errors.New("tasks: node availability source is not configured")
	ErrRequestIDRequired = errors.New("tasks: request id is required")
)
