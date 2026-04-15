package auth

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// ResolveRolesMiddleware resolves the caller's roles from JWT claims and stores
// them in the request context. This must run after the auth middleware (which
// populates claims) and before any RequireRole or claim-checking code.
//
// If authzCfg is nil, authenticated requests receive all roles (so that
// downstream role checks remain a no-op) and anonymous requests receive none.
// If authzCfg is non-nil, roles are resolved from the JWT claims via ResolveRoles;
// anonymous requests (nil claims) are passed through without roles and a
// one-time warning is logged.
func ResolveRolesMiddleware(authzCfg *config.AuthzConfig) func(http.Handler) http.Handler {
	if authzCfg == nil {
		// No authz config: any authenticated user implicitly holds all permissions
		// (RequireRole is also a pass-through when authzCfg == nil). Store all roles
		// in context so downstream code — including GET /v1/me — reflects reality.
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if claims := ClaimsFromContext(r.Context()); claims != nil {
					r = r.WithContext(ContextWithRoles(r.Context(), AllRoles()))
				}
				next.ServeHTTP(w, r)
			})
		}
	}
	var warnOnce sync.Once
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				warnOnce.Do(func() {
					slog.Warn("Authorization roles configured but auth is disabled (anonymous mode); role checks are skipped")
				})
				next.ServeHTTP(w, r)
				return
			}

			roles := ResolveRoles(claims, authzCfg)
			r = r.WithContext(ContextWithRoles(r.Context(), roles))
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole returns middleware that enforces the specified role.
// It expects roles to already be resolved in the context by ResolveRolesMiddleware.
// If authzCfg is nil, a pass-through middleware is returned immediately.
// If claims are nil (anonymous mode), role checks are skipped.
func RequireRole(role Role, authzCfg *config.AuthzConfig) func(http.Handler) http.Handler {
	if authzCfg == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			// Skip role checks in anonymous mode (no claims).
			if claims == nil {
				next.ServeHTTP(w, r)
				return
			}

			roles := RolesFromContext(r.Context())
			if !HasRole(roles, role) {
				common.WriteErrorResponse(w, "forbidden: insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
