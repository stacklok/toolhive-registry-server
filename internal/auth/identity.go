package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

// AnonymousSubject is the placeholder value used in log fields when no
// authenticated subject is present (anonymous mode, public paths, or pre-auth
// middleware). Using a stable token rather than an empty string keeps the log
// schema uniform across authenticated and anonymous requests so aggregations
// can group by `sub` without special-casing.
const AnonymousSubject = "anonymous"

// identityHolder is a mutable carrier installed in the request context by
// outer middleware so deeper middleware (auth) can publish the resolved
// identity back to the outer scope. Go's context replacement happens in a
// child branch — without a shared holder, an outer access logger cannot see
// claims attached by inner auth middleware.
//
// Concurrency note: the holder is intentionally unsynchronised. The chi
// middleware chain runs synchronously on a single goroutine, so the writer
// (auth middleware) and the reader (LoggingMiddleware after next.ServeHTTP)
// are sequenced via the call/return of next.ServeHTTP. Introducing a
// goroutine-hopping middleware above LoggingMiddleware (e.g. http.TimeoutHandler,
// or any handler that spawns the inner chain in a goroutine) would race the
// fields below — wrap them in atomic.Pointer or a mutex if that ever lands.
type identityHolder struct {
	sub  string
	user string
	set  bool
}

type identityHolderKey struct{}

// WithIdentityHolder returns a context with a fresh, empty identity holder
// installed. The auth middleware populates this holder via SetIdentity once
// validation succeeds, allowing the outer access logger to read the
// authenticated subject after the chain returns.
func WithIdentityHolder(ctx context.Context) context.Context {
	return context.WithValue(ctx, identityHolderKey{}, &identityHolder{})
}

// SetIdentity records the authenticated subject and display name into the
// holder, if one is present in ctx. No-op when no holder is installed (e.g.,
// requests not routed through the access logger).
func SetIdentity(ctx context.Context, sub, user string) {
	if h, ok := ctx.Value(identityHolderKey{}).(*identityHolder); ok && h != nil {
		h.sub, h.user, h.set = sub, user, true
	}
}

// IdentityFromContext returns the authenticated subject (`sub` claim) and
// display name for the request. Resolution order:
//  1. The identity holder, if populated — visible to outer middleware.
//  2. JWT claims directly in ctx — for code paths below the auth middleware.
//
// Returns ("", "") when no identity is available. Use AnonymousSubject when
// emitting log fields so the schema stays uniform.
func IdentityFromContext(ctx context.Context) (sub, user string) {
	if h, ok := ctx.Value(identityHolderKey{}).(*identityHolder); ok && h != nil && h.set {
		return h.sub, h.user
	}
	return IdentityFromClaims(ClaimsFromContext(ctx))
}

// IdentityFromClaims extracts (sub, user) from raw JWT claims using the
// canonical fallback order. The `sub` claim is taken verbatim. The display
// name falls back through `name` → `preferred_username` → `email`. Returns
// ("", "") when claims is nil.
func IdentityFromClaims(claims jwt.MapClaims) (sub, user string) {
	if claims == nil {
		return "", ""
	}
	sub = claimString(claims, "sub")
	if name := claimString(claims, "name"); name != "" {
		user = name
	} else if pref := claimString(claims, "preferred_username"); pref != "" {
		user = pref
	} else if email := claimString(claims, "email"); email != "" {
		user = email
	}
	return sub, user
}

// claimString returns claims[key] as a string, or "" if missing or not a
// string. Safe on nil maps.
func claimString(claims map[string]any, key string) string {
	v, _ := claims[key].(string)
	return v
}
