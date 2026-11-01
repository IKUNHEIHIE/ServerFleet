package ai

import "context"

type GenerationBudgetReader interface {
	View(context.Context, func([]byte) error) error
}
type BudgetUsage struct {
	Requests     uint64 `json:"requests"`
	InputBytes   uint64 `json:"inputBytes"`
	OutputTokens uint64 `json:"outputTokens"`
}
type BudgetPolicyView struct {
	Revision         uint64 `json:"revision"`
	WindowID         string `json:"windowId"`
	StartsAt         string `json:"startsAt"`
	EndsAt           string `json:"endsAt"`
	RequestLimit     uint64 `json:"requestLimit"`
	InputByteLimit   uint64 `json:"inputByteLimit"`
	OutputTokenLimit uint64 `json:"outputTokenLimit"`
}
type GenerationBudgetView struct {
	OwnerID                 string            `json:"ownerId"`
	ProfileID               string            `json:"profileId"`
	Configured              bool              `json:"configured"`
	Active                  bool              `json:"active"`
	Policy                  *BudgetPolicyView `json:"policy"`
	Usage                   BudgetUsage       `json:"usage"`
	Remaining               BudgetUsage       `json:"remaining"`
	OutputReservationTokens uint64            `json:"outputReservationTokens"`
}

// Program Configure callers remain supported. If a Web actor is present, its
// current session observation is checked INSIDE the same serialized writer too.
func checkBudgetWebActor(ctx context.Context, owner string, admin bool) error {
	actor, recheck, ok := ActorFromContext(ctx)
	if !ok {
		return nil
	}
	if !actor.HumanWeb || actor.SubjectID != owner || admin && !actor.Administrator || recheck == nil || recheck(ctx) != nil {
		return ErrToolDenied
	}
	return nil
}
func remainingBudget(limit, used uint64) uint64 {
	if used >= limit {
		return 0
	}
	return limit - used
}

// Read requires a private CURRENT Web actor; ownership cannot be supplied by the
// URL/body. Returns only this profile/window's metadata, never operation IDs,
// payload digests, prompt text, credentials or other owners' counters.
func (b *PersistentGenerationBudget) Read(ctx context.Context, profile string) (out GenerationBudgetView, err error) {
	actor, recheck, ok := ActorFromContext(ctx)
	if b == nil || !ok || !actor.HumanWeb || !exactID(actor.SubjectID) || !exactID(profile) || recheck == nil || recheck(ctx) != nil {
		return out, ErrToolDenied
	}
	reader, ok := b.store.(GenerationBudgetReader)
	if !ok {
		return out, ErrModelUnavailable
	}
	out = GenerationBudgetView{OwnerID: actor.SubjectID, ProfileID: profile, OutputReservationTokens: ProviderMaxOutputTokens}
	err = reader.View(ctx, func(raw []byte) error {
		if recheck(ctx) != nil {
			return ErrToolDenied
		}
		state, e := decodeBudget(raw)
		if e != nil {
			return e
		}
		p, found := state.Policies[budgetKey(actor.SubjectID, profile)]
		now := b.now().UTC()
		if now.IsZero() || now.Before(state.ClockFloor) {
			return ErrSessionUnavailable
		}
		if !found {
			return nil
		}
		c := state.Counters[counterKey(p)]
		out.Configured = true
		out.Active = !now.Before(p.StartsAt) && now.Before(p.EndsAt)
		out.Policy = &BudgetPolicyView{p.Revision, p.WindowID, p.StartsAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), p.EndsAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), p.RequestLimit, p.InputByteLimit, p.OutputTokenLimit}
		out.Usage = BudgetUsage(c)
		out.Remaining = BudgetUsage{remainingBudget(p.RequestLimit, c.Requests), remainingBudget(p.InputByteLimit, c.InputBytes), remainingBudget(p.OutputTokenLimit, c.OutputTokens)}
		return nil
	})
	if err == nil && recheck(ctx) != nil {
		return GenerationBudgetView{}, ErrToolDenied
	}
	return out, err
}
