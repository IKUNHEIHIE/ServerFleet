package ai

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"
)

// Output token reservation is the actual provider request ceiling, NOT a claim
// about measured token usage or currency pricing. Unknown calls are not refunded.
const ProviderMaxOutputTokens uint64 = 8192

type GenerationBudgetPolicy struct {
	OwnerID, ProfileID, WindowID                   string
	Revision                                       uint64
	StartsAt, EndsAt                               time.Time
	RequestLimit, InputByteLimit, OutputTokenLimit uint64
}
type GenerationBudgetAudit struct {
	ActorID, OwnerID, ProfileID, OperationID, WindowID, Action, Digest string
	PolicyRevision, InputBytes, OutputTokens                           uint64
	At                                                                 time.Time
}
type generationBudgetCounter struct{ Requests, InputBytes, OutputTokens uint64 }
type generationBudgetReservation struct {
	OwnerID, ProfileID, WindowID, Digest                                     string
	ProfileRevision, ScopeRevision, PolicyRevision, InputBytes, OutputTokens uint64
}
type generationBudgetState struct {
	Version    int
	ClockFloor time.Time
	Policies   map[string]GenerationBudgetPolicy
	Counters   map[string]generationBudgetCounter
	Operations map[string]generationBudgetReservation
}

// Update must reserve counters, unique operation ID and typed audit in ONE
// durable transaction, invoke fn once and never retry an ambiguous commit.
type GenerationBudgetStore interface {
	Update(context.Context, func([]byte) ([]byte, []GenerationBudgetAudit, error)) error
}

// Admin gate must be read-only and use the same coordinator DB/transaction
// authority; it must never start another write transaction or trust HTTP claims.
type GenerationBudgetAdminGate func(context.Context, string, GenerationBudgetPolicy) error
type GenerationBudgetGate func(context.Context, GenerationRequest, string) error
type PersistentGenerationBudget struct {
	store GenerationBudgetStore
	admin GenerationBudgetAdminGate
	admit GenerationBudgetGate
	now   func() time.Time
}

func NewPersistentGenerationBudget(store GenerationBudgetStore, admin GenerationBudgetAdminGate, admit GenerationBudgetGate) (*PersistentGenerationBudget, error) {
	if store == nil || admin == nil || admit == nil {
		return nil, ErrModelUnavailable
	}
	return &PersistentGenerationBudget{store, admin, admit, time.Now}, nil
}
func budgetKey(owner, profile string) string { return owner + "\x00" + profile }
func counterKey(p GenerationBudgetPolicy) string {
	return budgetKey(p.OwnerID, p.ProfileID) + "\x00" + p.WindowID
}
func validBudgetDigest(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == s
}
func validBudgetPolicy(p GenerationBudgetPolicy) bool {
	return exactID(p.OwnerID) && exactID(p.ProfileID) && exactID(p.WindowID) && p.Revision > 0 && !p.StartsAt.IsZero() && p.EndsAt.After(p.StartsAt) && p.EndsAt.Sub(p.StartsAt) <= 366*24*time.Hour && p.RequestLimit > 0 && p.RequestLimit <= 10000 && p.InputByteLimit > 0 && p.InputByteLimit <= 1<<40 && p.OutputTokenLimit > 0 && p.OutputTokenLimit <= 1<<40
}
func decodeBudget(raw []byte) (*generationBudgetState, error) {
	s := &generationBudgetState{Version: 1}
	if len(raw) > 4<<20 || len(raw) > 0 && (json.Unmarshal(raw, s) != nil || s.Version != 1) {
		return nil, ErrSessionUnavailable
	}
	if s.Policies == nil {
		s.Policies = map[string]GenerationBudgetPolicy{}
	}
	if s.Counters == nil {
		s.Counters = map[string]generationBudgetCounter{}
	}
	if s.Operations == nil {
		s.Operations = map[string]generationBudgetReservation{}
	}
	if len(s.Policies) > 1000 || len(s.Counters) > 10000 || len(s.Operations) > 10000 {
		return nil, ErrSessionUnavailable
	}
	for key, p := range s.Policies {
		if !validBudgetPolicy(p) || key != budgetKey(p.OwnerID, p.ProfileID) {
			return nil, ErrSessionUnavailable
		}
	}
	for id, o := range s.Operations {
		if !exactID(id) || !exactID(o.OwnerID) || !exactID(o.ProfileID) || !exactID(o.WindowID) || !validBudgetDigest(o.Digest) || o.ProfileRevision == 0 || o.ScopeRevision == 0 || o.PolicyRevision == 0 || o.OutputTokens != ProviderMaxOutputTokens || o.InputBytes > 256000 {
			return nil, ErrSessionUnavailable
		}
	}
	counts := map[string]generationBudgetCounter{}
	for _, o := range s.Operations {
		key := budgetKey(o.OwnerID, o.ProfileID) + "\x00" + o.WindowID
		c := counts[key]
		c.Requests++
		c.InputBytes += o.InputBytes
		c.OutputTokens += o.OutputTokens
		counts[key] = c
	}
	if len(counts) != len(s.Counters) {
		return nil, ErrSessionUnavailable
	}
	for key, c := range s.Counters {
		if counts[key] != c {
			return nil, ErrSessionUnavailable
		}
	}
	return s, nil
}
func encodeBudget(s *generationBudgetState) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > 4<<20 {
		return nil, ErrSessionUnavailable
	}
	return raw, nil
}

// Configure is a program-registered admin operation, never model output. New
// window IDs are explicit; reconfiguring an existing window retains its usage.
func (b *PersistentGenerationBudget) Configure(ctx context.Context, actor string, expected uint64, p GenerationBudgetPolicy) error {
	if b == nil || ctx == nil || !exactID(actor) || !validBudgetPolicy(p) || p.Revision != expected+1 || expected == math.MaxUint64 || b.admin(ctx, actor, p) != nil {
		return ErrToolDenied
	}
	err := b.store.Update(ctx, func(raw []byte) ([]byte, []GenerationBudgetAudit, error) {
		s, err := decodeBudget(raw)
		if err != nil {
			return nil, nil, err
		}
		key := budgetKey(p.OwnerID, p.ProfileID)
		if s.Policies[key].Revision != expected {
			return nil, nil, ErrModelRevisionConflict
		}
		if b.admin(ctx, actor, p) != nil {
			return nil, nil, ErrToolDenied
		}
		now := b.now().UTC()
		if now.IsZero() || now.Before(s.ClockFloor) {
			return nil, nil, ErrSessionUnavailable
		}
		s.ClockFloor = now
		if len(s.Policies) >= 1000 && s.Policies[key].Revision == 0 {
			return nil, nil, ErrSessionUnavailable
		}
		// No hidden day rollover or deletion of old operation fences.
		s.Policies[key] = p
		next, err := encodeBudget(s)
		event := GenerationBudgetAudit{ActorID: actor, OwnerID: p.OwnerID, ProfileID: p.ProfileID, WindowID: p.WindowID, Action: "ai.budget.configure", PolicyRevision: p.Revision, At: now}
		return next, []GenerationBudgetAudit{event}, err
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil || b.admin(ctx, actor, p) != nil {
		return ErrGenerationUnknown
	}
	return nil
}

// Admit is directly compatible with server.Config.AIGenerationQuota. A unique
// operation is charged BEFORE HTTP/secret I/O; quota rejection performs no I/O.
// The gate requires Conversation's private live generating observation. No
// plaintext messages or credentials are stored in quota or audit records.
func (b *PersistentGenerationBudget) Admit(ctx context.Context, r GenerationRequest) (result error) {
	if b == nil || ctx == nil || ctx.Err() != nil {
		return ErrToolDenied
	}
	defer func() {
		if recover() != nil {
			result = ErrGenerationUnknown
		}
	}()
	digest, err := generationDigest(r)
	if err != nil || b.admit(ctx, r, digest) != nil {
		return ErrToolDenied
	}
	var input uint64
	for _, m := range r.Messages {
		input += uint64(len(m.RedactedContent))
	}
	err = b.store.Update(ctx, func(raw []byte) ([]byte, []GenerationBudgetAudit, error) {
		s, err := decodeBudget(raw)
		if err != nil {
			return nil, nil, err
		}
		if _, seen := s.Operations[r.OperationID]; seen {
			return nil, nil, ErrGenerationUnknown
		}
		if len(s.Operations) >= 10000 {
			return nil, nil, ErrSessionUnavailable
		}
		p, ok := s.Policies[budgetKey(r.SubjectID, r.Profile.ID)]
		now := b.now().UTC()
		if now.IsZero() || now.Before(s.ClockFloor) {
			return nil, nil, ErrSessionUnavailable
		}
		s.ClockFloor = now
		if !ok || now.Before(p.StartsAt) || !now.Before(p.EndsAt) {
			return nil, nil, ErrToolDenied
		}
		key := counterKey(p)
		c := s.Counters[key]
		if c.Requests >= p.RequestLimit || c.InputBytes > p.InputByteLimit || input > p.InputByteLimit-c.InputBytes || c.OutputTokens > p.OutputTokenLimit || ProviderMaxOutputTokens > p.OutputTokenLimit-c.OutputTokens {
			return nil, nil, ErrToolDenied
		}
		if len(s.Counters) >= 10000 && c.Requests == 0 {
			return nil, nil, ErrSessionUnavailable
		}
		c.Requests++
		c.InputBytes += input
		c.OutputTokens += ProviderMaxOutputTokens
		s.Counters[key] = c
		s.Operations[r.OperationID] = generationBudgetReservation{r.SubjectID, r.Profile.ID, p.WindowID, digest, r.Profile.Revision, r.ScopeRevision, p.Revision, input, ProviderMaxOutputTokens}
		next, err := encodeBudget(s)
		event := GenerationBudgetAudit{ActorID: r.SubjectID, OwnerID: r.SubjectID, ProfileID: r.Profile.ID, OperationID: r.OperationID, WindowID: p.WindowID, Action: "ai.budget.reserve", Digest: digest, PolicyRevision: p.Revision, InputBytes: input, OutputTokens: ProviderMaxOutputTokens, At: now}
		return next, []GenerationBudgetAudit{event}, err
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil || b.admit(ctx, r, digest) != nil {
		return ErrGenerationUnknown
	}
	return nil
}
