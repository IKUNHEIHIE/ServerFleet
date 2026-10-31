package nodes

import (
	"context"
	"errors"

	"serverfleet.local/serverfleet/internal/auth"
)

var (
	ErrInventoryUnavailable = errors.New("nodes: inventory source unavailable")
	ErrInvalidInventory     = errors.New("nodes: invalid inventory")
)

// InventorySource is a provisional, offline function seam, not the Reader port.
// The trusted caller supplies a current snapshot, including Tags and Metrics,
// that remains read-only until ReadVisibleInventory returns. The source must
// cooperate with ctx.
type InventorySource func(context.Context) ([]Node, error)

// ReadVisibleInventory prepares a fresh nodes.read scope before reading any
// inventory, including an empty inventory. subjectID must be bound by a trusted
// authentication boundary, not a client claim. No reader, scope or result is cached.
// Only successful, authorized output may be used for downstream Filter/Classify.
// Sequential reads do not make policy, inventory and output one atomic snapshot
// or observe revocation after preparation; no production entry point is wired.
func ReadVisibleInventory(ctx context.Context, subjectID string, policy auth.PolicyReader, inventory InventorySource) (out []Node, err error) {
	if ctx != nil {
		defer func() {
			if ctxErr := ctx.Err(); ctxErr != nil {
				out, err = nil, ctxErr
			}
		}()
	}
	// Delegate nil context, subject and policy validation to the real preflight.
	scope, err := auth.PrepareNodeReadScope(ctx, subjectID, policy)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if inventory == nil {
		return nil, ErrInventoryUnavailable
	}
	snapshot, sourceErr := inventory(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if errors.Is(sourceErr, context.Canceled) {
		return nil, context.Canceled
	}
	if errors.Is(sourceErr, context.DeadlineExceeded) {
		return nil, context.DeadlineExceeded
	}
	if sourceErr != nil {
		// Do not retain source text or its unwrap chain, even alongside partial data.
		return nil, ErrInventoryUnavailable
	}
	seen := make(map[string]struct{}, len(snapshot))
	// Capacity grows from visible nodes only; do not expose hidden inventory size.
	out = make([]Node, 0)
	for _, n := range snapshot {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, duplicate := seen[n.ID]; duplicate {
			return nil, ErrInvalidInventory
		}
		seen[n.ID] = struct{}{}
		checkErr := scope.CheckNode(ctx, n.ID)
		if checkErr != nil {
			if classifyAuthorizationError(checkErr) == authorizationErrorPureDenial {
				continue
			}
			// Invalid IDs and every non-pure denial abort, never a partial list.
			return nil, checkErr
		}
		cloned := n
		if n.Tags != nil {
			cloned.Tags = make([]string, len(n.Tags))
			for i, tag := range n.Tags {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				cloned.Tags[i] = tag
			}
		}
		if n.Metrics != nil {
			metrics := *n.Metrics
			cloned.Metrics = &metrics
		}
		out = append(out, cloned)
	}
	return out, nil
}
