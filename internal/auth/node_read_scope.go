package auth

import (
	"context"
	"errors"
)

// NodeReadScope is an immutable, request-local snapshot of one subject's exact
// nodes.read grants. It is neither an identity nor a reusable authorization token.
// Prepare a new scope for each request; a captured scope cannot observe later
// revocation or make inventory reads/response serialization atomic with policy.
// Its zero value and nil receiver fail closed. Copies share only private read-only
// state; no accessor exposes the captured set and no policy/reader is retained.
type NodeReadScope struct {
	nodes map[string]struct{}
}

// PrepareNodeReadScope validates a trusted subject and one current trusted policy
// before any inventory is needed. The caller must bind subjectID through a trusted
// authentication boundary; a client claim is not sufficient. The PolicyReader must
// cooperate with ctx and keep its returned maps immutable until preparation ends.
// A known subject with no grants gets a valid empty scope, never all nodes.
func PrepareNodeReadScope(ctx context.Context, subjectID string, read PolicyReader) (scope *NodeReadScope, err error) {
	if ctx == nil {
		return nil, policyError(InvalidCheck, "context")
	}
	defer func() {
		if ctx.Err() != nil {
			scope, err = nil, ctx.Err()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validPolicyID(subjectID) {
		return nil, policyError(InvalidCheck, "subject")
	}
	if read == nil {
		return nil, policyError(PolicyUnavailable, "policy_reader")
	}
	policy, readErr := read(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Normalize wrapped context errors without retaining sensitive source text.
	if errors.Is(readErr, context.Canceled) {
		return nil, context.Canceled
	}
	if errors.Is(readErr, context.DeadlineExceeded) {
		return nil, context.DeadlineExceeded
	}
	if readErr != nil {
		return nil, policyError(PolicyUnavailable, "policy_reader")
	}
	if policy == nil {
		return nil, policyError(PolicyUnavailable, "policy")
	}
	if err := validateNodePolicy(ctx, policy); err != nil {
		return nil, err
	}
	grant, knownSubject := policy.ReadGrants[subjectID]
	if !knownSubject {
		return nil, policyError(Denied, "subject")
	}
	// A non-nil private set distinguishes successful zero-resource preparation
	// from an unprepared zero value. Copy only this subject's validated grants.
	nodes := make(map[string]struct{}, len(grant))
	for id := range grant {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nodes[id] = struct{}{}
	}
	return &NodeReadScope{nodes: nodes}, nil
}

// CheckNode checks one exact node ID against this captured nodes.read scope. It
// takes no subject/action override, performs no policy refresh and grants no other
// capability. Reusing it in another request would reuse stale permission.
func (s *NodeReadScope) CheckNode(ctx context.Context, nodeID string) (err error) {
	if ctx == nil {
		return policyError(InvalidCheck, "context")
	}
	defer func() {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.nodes == nil {
		return policyError(PolicyUnavailable, "read_scope")
	}
	if !validPolicyID(nodeID) {
		return policyError(InvalidCheck, "targets")
	}
	if _, allowed := s.nodes[nodeID]; !allowed {
		return policyError(Denied, "targets")
	}
	return nil
}
