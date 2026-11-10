package ai

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"serverfleet.local/serverfleet/internal/auth"
)

type generationObservationKey struct{}
type generationObservation struct {
	request GenerationRequest
	digest  string
	nodes   []string
	closed  atomic.Bool
}

// CheckGenerationBudget reuses the private live generation fence and current
// owner/scope/profile checks. It does not reserve/borrow a credential; the real
// credential revision is independently resolved and checked at provider I/O.
func (c *ConversationService) CheckGenerationBudget(ctx context.Context, r GenerationRequest, digest string) error {
	expected, err := generationDigest(r)
	if err != nil || expected != digest {
		return ErrToolDenied
	}
	return c.CheckCredentialOperation(ctx, auth.CredentialOperation{SubjectID: r.SubjectID, CredentialID: r.Profile.CredentialRef, OperationID: r.OperationID, PlanDigest: digest, Module: "ai", Source: "ai", Action: "use", ExpectedRevision: 1})
}

func observeGeneration(ctx context.Context, r GenerationRequest, nodes []string) (context.Context, func(), error) {
	digest, err := generationDigest(r)
	if err != nil {
		return nil, nil, err
	}
	r.Messages = append([]Message{}, r.Messages...)
	for i := range r.Messages {
		r.Messages[i].TaskIDs = []string{}
	}
	o := &generationObservation{request: r, digest: digest, nodes: append([]string{}, nodes...)}
	return context.WithValue(ctx, generationObservationKey{}, o), func() { o.closed.Store(true) }, nil
}

// CheckCredentialOperation is the AI-specific part of a registered auth gate.
// It accepts ONLY an observation privately created by Generate after the durable
// generating fence and final filtering. Neither HTTP claims nor calling a
// provider with a plausible GenerationRequest can create that authority. Auth
// must additionally validate the current identity/credential revision/purpose.
// This gate grants no node/tool permission and exposes no key or observation.
func (c *ConversationService) CheckCredentialOperation(ctx context.Context, op auth.CredentialOperation) (result error) {
	if c == nil || ctx == nil || ctx.Err() != nil {
		return ErrToolDenied
	}
	defer func() {
		if recover() != nil {
			result = ErrToolDenied
		}
	}()
	o, ok := ctx.Value(generationObservationKey{}).(*generationObservation)
	if !ok || o == nil || o.closed.Load() {
		return ErrToolDenied
	}
	r := o.request
	if op.SubjectID != r.SubjectID || op.CredentialID != r.Profile.CredentialRef || op.OperationID != r.OperationID || op.PlanDigest != o.digest || op.Module != "ai" || op.Source != "ai" || op.Action != "use" || op.ChangeID != "" || op.ExpectedRevision == 0 {
		return ErrToolDenied
	}
	actor, err := c.sessions.actor(ctx, "ai.messages.generate", r.SessionID, false)
	if err != nil || actor.SubjectID != r.SubjectID {
		return ErrToolDenied
	}
	view, err := c.sessions.Get(ctx, r.SessionID)
	if err != nil || view.Status != "active" || view.ScopeRevision != r.ScopeRevision || !slices.Equal(view.AllowedNodeIDs, o.nodes) {
		return ErrToolDenied
	}
	profile, err := c.profile(ctx, r.SubjectID)
	if err != nil || profile != r.Profile {
		return ErrToolDenied
	}
	err = c.sessions.change(ctx, func(st *sessionState, _ time.Time) error {
		if _, err := c.sessions.recheck(ctx, actor, "ai.messages.generate", r.SessionID, false); err != nil {
			return ErrToolDenied
		}
		record, exists := st.Sessions[r.SessionID]
		if !exists || record.Closed || record.Scope.SubjectID != r.SubjectID || record.Scope.Revision != r.ScopeRevision || record.Conversation == nil {
			return ErrToolDenied
		}
		run, exists := record.Conversation.Runs[r.MessageID]
		if !exists || run.State != "generating" || run.ScopeRevision != r.ScopeRevision || run.Profile != r.Profile || o.closed.Load() {
			return ErrToolDenied
		}
		if r.Agent != nil && agentGenerationCurrent(record, r, time.Now().UTC()) != nil {
			return ErrToolDenied
		}
		return nil
	})
	if err != nil || ctx.Err() != nil || o.closed.Load() {
		return ErrToolDenied
	}
	return nil
}
