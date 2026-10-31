package ai

import "context"

type actorContextKey struct{}

// WithActor attaches the trusted authenticated Web actor and a reauthentication
// callback to one request context. Callbacks never accept values from the body.
func WithActor(ctx context.Context, actor SessionActor, recheck func(context.Context) error) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actorObservation{actor: actor, recheck: recheck})
}

type actorObservation struct {
	actor   SessionActor
	recheck func(context.Context) error
}

// ActorFromContext returns the request actor and commit-time reauthentication
// callback. Missing values fail closed in composition callbacks.
func ActorFromContext(ctx context.Context) (SessionActor, func(context.Context) error, bool) {
	if ctx == nil {
		return SessionActor{}, nil, false
	}
	observation, ok := ctx.Value(actorContextKey{}).(actorObservation)
	if !ok {
		return SessionActor{}, nil, false
	}
	return observation.actor, observation.recheck, true
}
