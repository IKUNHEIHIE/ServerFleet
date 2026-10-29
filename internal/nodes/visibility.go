package nodes

import (
	"context"
	"errors"
	"strings"

	"serverfleet.local/serverfleet/internal/auth"
)

// readAction is provisional and internal; it is not a frozen public contract.
const readAction = "nodes.read"

var (
	ErrMissingAuthorizer = errors.New("nodes: authorizer is required")
	ErrMissingSubject    = errors.New("nodes: subject is required")
	ErrInvalidSubject    = errors.New("nodes: subject must not have surrounding whitespace")
)

// Visible filters nodes through the injected Authorizer, one node at a time, using a fixed
// read action. Only auth.ErrDenied filters an individual node. Policy-unavailable,
// invalid-check, cancellation/deadline and unknown authorization errors abort with a nil
// result, never a partial list. Subject IDs are not trimmed: blank subjects and subjects with
// surrounding whitespace are rejected before any authorization call.
func Visible(ctx context.Context, authorizer auth.Authorizer, subjectID string, nodes []Node) ([]Node, error) {
	if authorizer == nil {
		return nil, ErrMissingAuthorizer
	}
	if strings.TrimSpace(subjectID) == "" {
		return nil, ErrMissingSubject
	}
	if subjectID != strings.TrimSpace(subjectID) {
		return nil, ErrInvalidSubject
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		check := auth.Check{SubjectID: subjectID, Action: readAction, NodeIDs: []string{n.ID}}
		authErr := authorizer.Authorize(ctx, check)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if authErr == nil {
			out = append(out, n)
			continue
		}
		if errors.Is(authErr, auth.ErrDenied) {
			continue
		}
		return nil, authErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// VisibleFilter authorizes the inventory and then applies the view filter. List callers should
// use this rather than filtering an unfiltered inventory.
func VisibleFilter(ctx context.Context, authorizer auth.Authorizer, subjectID string, nodes []Node, q NodeQuery) ([]Node, error) {
	visible, err := Visible(ctx, authorizer, subjectID, nodes)
	if err != nil {
		return nil, err
	}
	return Filter(visible, q)
}

// VisibleClassify authorizes the inventory and then classifies only the visible subset, so
// bucket counts cannot leak nodes outside the subject's scope.
func VisibleClassify(ctx context.Context, authorizer auth.Authorizer, subjectID string, nodes []Node, d Dimension) ([]Classification, error) {
	visible, err := Visible(ctx, authorizer, subjectID, nodes)
	if err != nil {
		return nil, err
	}
	return Classify(visible, d)
}
