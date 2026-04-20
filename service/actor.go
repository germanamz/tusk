package service

import "context"

type actorKey struct{}

// WithActor returns a context carrying the given player ID. An empty playerID
// returns ctx unchanged — ActorFromContext will surface nil, matching the "no
// actor" case.
func WithActor(ctx context.Context, playerID string) context.Context {
	if playerID == "" {
		return ctx
	}
	return context.WithValue(ctx, actorKey{}, playerID)
}

// ActorFromContext returns the actor attached to ctx, or nil if none. The
// returned *string is safe to pass directly into an Event.PlayerID field
// (which is *string, NULL-safe).
func ActorFromContext(ctx context.Context) *string {
	v, ok := ctx.Value(actorKey{}).(string)
	if !ok || v == "" {
		return nil
	}
	return &v
}
