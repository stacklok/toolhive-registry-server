package auth

import (
	"net/http"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// RequireRole returns middleware that enforces the specified role.
// If authzCfg is nil (no authorization configured) or claims are nil
// (anonymous mode), role checks are skipped.
func RequireRole(role Role, authzCfg *config.AuthzConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip role checks if no authz config
			if authzCfg == nil {
				next.ServeHTTP(w, r)
				return
			}

			claims := ClaimsFromContext(r.Context())
			// Skip role checks in anonymous mode (no claims)
			if claims == nil {
				next.ServeHTTP(w, r)
				return
			}

			roles := ResolveRoles(claims, authzCfg)
			if !HasRole(roles, role) {
				common.WriteErrorResponse(w, "forbidden: insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
