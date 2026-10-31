// Package tasks owns the sole future execution pipeline. No scheduler exists yet.
package tasks

import "context"

// Request is a provisional internal seam. Authorization/approval must precede
// persistence and dispatch; adding Submitter does not implement those checks.
type Request struct {
	SubjectID string
	ActionID  string
	NodeIDs   []string
}

type Submitter interface {
	Submit(context.Context, Request) (string, error)
}
