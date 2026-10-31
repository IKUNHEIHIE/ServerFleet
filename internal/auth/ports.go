// Package auth owns identity and resource permission checks. Ports are provisional.
// No implementation grants permissions in M0-P1.
package auth

import "context"

type Check struct {
	SubjectID string
	Action    string
	NodeIDs   []string
}

type Authorizer interface {
	Authorize(context.Context, Check) error
}
