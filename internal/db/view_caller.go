package db

import "context"

type viewCallerKey struct{}

// WithViewCaller returns a context that makes pkg/registry/postgres view reads
// filter entries by claims for a caller whose JWT claims marshal to
// callerJSON. The key is internal to this module, so external embedders of
// pkg/registry can never enable claims filtering. Empty callerJSON leaves ctx
// unchanged. Only the server's claims mode (internal/claims) sets it.
func WithViewCaller(ctx context.Context, callerJSON []byte) context.Context {
	if len(callerJSON) == 0 {
		return ctx
	}
	return context.WithValue(ctx, viewCallerKey{}, callerJSON)
}

// ViewCaller returns the caller claims set by WithViewCaller, or nil.
func ViewCaller(ctx context.Context) []byte {
	v, _ := ctx.Value(viewCallerKey{}).([]byte)
	return v
}
