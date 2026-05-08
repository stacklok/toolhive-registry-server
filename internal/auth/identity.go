package auth

import (
	"context"
	"sync"

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
// All access goes through store/load so the holder remains safe even if a
// future middleware (e.g. http.TimeoutHandler) hops the inner chain to a
// separate goroutine. Contention is essentially zero in the normal chi
// chain — one writer, one reader, sequenced by next.ServeHTTP — so the
// mutex is purely defensive.
type identityHolder struct {
	mu   sync.Mutex
	sub  string
	user string
	set  bool
}

func (h *identityHolder) store(sub, user string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sub, h.user, h.set = sub, user, true
}

func (h *identityHolder) load() (sub, user string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sub, h.user, h.set
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
		h.store(sub, user)
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
	if h, ok := ctx.Value(identityHolderKey{}).(*identityHolder); ok && h != nil {
		if sub, user, set := h.load(); set {
			return sub, user
		}
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
