package auth

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// NodeReadAction is the only action supported by this offline policy candidate.
// It grants neither task execution nor AI, root, plugin or global privileges.
const NodeReadAction = "nodes.read"

// NodePolicy is an explicitly supplied, trusted backend snapshot. KnownNodes
// enumerates exact node IDs. ReadGrants keys enumerate known subjects; each
// subject's set lists only the nodes that subject may read. Empty sets deny.
// Do not populate this from client authorization claims or mutate a snapshot
// while it is being evaluated. This type does not authenticate SubjectID.
type NodePolicy struct {
	KnownNodes map[string]struct{}
	ReadGrants map[string]map[string]struct{}
}

// EvaluateNodePolicy evaluates the entire request against one supplied snapshot.
// Success requires every target; there is no partial grant or wildcard matching.
// Cancellation/deadline takes precedence over a decision at function return.
func EvaluateNodePolicy(ctx context.Context, check Check, policy *NodePolicy) (err error) {
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
	if err := validateNodeCheck(check); err != nil {
		return err
	}
	if policy == nil {
		return policyError(PolicyUnavailable, "policy")
	}
	if err := validateNodePolicy(ctx, policy); err != nil {
		return err
	}
	grant, knownSubject := policy.ReadGrants[check.SubjectID]
	if !knownSubject {
		return policyError(Denied, "subject")
	}
	for _, id := range check.NodeIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, known := policy.KnownNodes[id]; !known {
			return policyError(Denied, "targets")
		}
		if _, allowed := grant[id]; !allowed {
			return policyError(Denied, "targets")
		}
	}
	return nil
}

// PolicyReader must return a current, immutable, trusted snapshot on each call
// and cooperate with ctx. No reader, snapshot or prior allow is cached here.
type PolicyReader func(context.Context) (*NodePolicy, error)

// NodePolicyAuthorizer adapts the offline evaluator to the unchanged Authorizer
// port. Its zero value and nil receiver fail closed. It is not an identity store.
type NodePolicyAuthorizer struct {
	read PolicyReader
}

func NewNodePolicyAuthorizer(read PolicyReader) *NodePolicyAuthorizer {
	return &NodePolicyAuthorizer{read: read}
}

func (a *NodePolicyAuthorizer) Authorize(ctx context.Context, check Check) (err error) {
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
	// Detach the caller's slice before invoking external policy code.
	check.NodeIDs = append([]string(nil), check.NodeIDs...)
	if err := validateNodeCheck(check); err != nil {
		return err
	}
	if a == nil || a.read == nil {
		return policyError(PolicyUnavailable, "policy_reader")
	}
	policy, readErr := a.read(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	// Preserve cancellation classification without echoing a wrapped source error.
	if errors.Is(readErr, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(readErr, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if readErr != nil {
		return policyError(PolicyUnavailable, "policy_reader")
	}
	return EvaluateNodePolicy(ctx, check, policy)
}

var _ Authorizer = (*NodePolicyAuthorizer)(nil)

func validPolicyID(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.ContainsAny(value, "*?[]") {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func validateNodeCheck(check Check) error {
	if !validPolicyID(check.SubjectID) {
		return policyError(InvalidCheck, "subject")
	}
	if !validPolicyID(check.Action) {
		return policyError(InvalidCheck, "action")
	}
	if len(check.NodeIDs) == 0 {
		return policyError(InvalidCheck, "targets")
	}
	seen := make(map[string]struct{}, len(check.NodeIDs))
	for _, id := range check.NodeIDs {
		if !validPolicyID(id) {
			return policyError(InvalidCheck, "targets")
		}
		if _, duplicate := seen[id]; duplicate {
			return policyError(InvalidCheck, "targets")
		}
		seen[id] = struct{}{}
	}
	if check.Action != NodeReadAction {
		return policyError(Denied, "action")
	}
	return nil
}

func validateNodePolicy(ctx context.Context, policy *NodePolicy) error {
	for id := range policy.KnownNodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validPolicyID(id) {
			return policyError(PolicyUnavailable, "policy")
		}
	}
	for subject, nodes := range policy.ReadGrants {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validPolicyID(subject) {
			return policyError(PolicyUnavailable, "policy")
		}
		for id := range nodes {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !validPolicyID(id) {
				return policyError(PolicyUnavailable, "policy")
			}
			if _, known := policy.KnownNodes[id]; !known {
				return policyError(PolicyUnavailable, "policy")
			}
		}
	}
	return nil
}
