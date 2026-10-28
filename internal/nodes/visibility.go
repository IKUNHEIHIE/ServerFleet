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
)

// Visible filters nodes through the injected Authorizer, one node at a time, using a fixed
// read action. It fails closed: a missing authorizer or subject is an error, and any per-node
// authorization error excludes that node. Callers must apply this before listing or counting.
func Visible(ctx context.Context, authorizer auth.Authorizer, subjectID string, nodes []Node) ([]Node, error) {
	if authorizer == nil {
		return nil, ErrMissingAuthorizer
	}
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return nil, ErrMissingSubject
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
		if err := authorizer.Authorize(ctx, check); err != nil {
			continue
		}
		out = append(out, n)
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
