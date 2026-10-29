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
// read action. Only a pure denial filters an individual node: every branch of the error's
// unwrap tree must end at auth.ErrDenied. A denial combined with policy-unavailable,
// invalid-check, cancellation/deadline, an unknown cause or an over-deep chain aborts with a
// nil result, never a partial list. Subject IDs are not trimmed: blank subjects and subjects
// with surrounding whitespace are rejected before any authorization call.
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
		if classifyAuthorizationError(authErr) == authorizationErrorPureDenial {
			continue
		}
		return nil, authErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

const maxDenialChainDepth = 64

var fatalAuthorizationCategories = []error{
	auth.ErrInvalidCheck,
	auth.ErrPolicyUnavailable,
	context.Canceled,
	context.DeadlineExceeded,
}

type authorizationErrorClass int

const (
	authorizationErrorImpure authorizationErrorClass = iota
	authorizationErrorPureDenial
	authorizationErrorFatal
)

// classifyAuthorizationError walks at most maxDenialChainDepth single-error unwraps. Only an
// exact auth.ErrDenied or a single-chain wrapper ending at it is a pure denial. Any
// multi-branch error is conservatively impure, even if every branch is a denial, so a hidden
// cause and a recursive Is method cannot be waved through. A single error may still declare a
// known fatal category through its own Is method; that takes precedence over denial.
func classifyAuthorizationError(err error) authorizationErrorClass {
	for depth := 0; err != nil; depth++ {
		if isFatalAuthorizationSentinel(err) {
			return authorizationErrorFatal
		}
		if err == auth.ErrDenied {
			return authorizationErrorPureDenial
		}
		if depth >= maxDenialChainDepth {
			return authorizationErrorImpure
		}
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return authorizationErrorImpure
		}
		if iser, ok := err.(interface{ Is(error) bool }); ok {
			for _, fatal := range fatalAuthorizationCategories {
				if iser.Is(fatal) {
					return authorizationErrorFatal
				}
			}
		}
		single, ok := err.(interface{ Unwrap() error })
		if !ok {
			return authorizationErrorImpure
		}
		err = single.Unwrap()
	}
	return authorizationErrorImpure
}

func isFatalAuthorizationSentinel(err error) bool {
	for _, fatal := range fatalAuthorizationCategories {
		if err == fatal {
			return true
		}
	}
	return false
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
