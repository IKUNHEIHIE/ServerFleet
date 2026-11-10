package ai

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrExecutionUnavailable = errors.New("ai: tool execution unavailable")

// ToolExecutionRequest is already assessed and approval-consumed by
// SessionService.Execute. The executor must still submit through the sole task
// service, which repeats authorization against current state.
type ToolExecutionRequest struct {
	InvocationID  string
	SessionID     string
	ToolID        string
	NodeIDs       []string
	Parameters    []byte
	ScopeRevision uint64
	RootShell     bool
}
type ToolExecutionResult struct {
	TaskID      string
	State       string
	Observation json.RawMessage
}
type ToolExecutor func(context.Context, ToolExecutionRequest) (ToolExecutionResult, error)
