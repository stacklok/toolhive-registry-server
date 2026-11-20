package auth

import (
	"context"
	"net/http"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

type anonymousKey struct{}

// WithAnonymousAdmission wraps authentication for Cedar mode, where an
// unauthenticated caller is an explicit, policy-checked principal:
//   - auth.mode anonymous: every request is anonymous;
//   - auth.mode oauth: a request without an Authorization header is anonymous
//     when allowAnonymous is set; any request that carries a token is always
//     validated by authenticate, so a bad token never falls back to anonymous.
func WithAnonymousAdmission(
	authenticate func(http.Handler) http.Handler, mode config.AuthMode, allowAnonymous bool,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		authenticated := authenticate(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == config.AuthModeAnonymous ||
				(allowAnonymous && r.Header.Get("Authorization") == "") {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), anonymousKey{}, true)))
				return
			}
			authenticated.ServeHTTP(w, r)
		})
	}
}

// IsAnonymous reports whether WithAnonymousAdmission admitted the request as
// an anonymous caller.
func IsAnonymous(ctx context.Context) bool {
	v, _ := ctx.Value(anonymousKey{}).(bool)
	return v
}

// AnonymousChallenge returns the WWW-Authenticate value for an anonymous
// caller refused by Cedar mode: a Bearer challenge that, under OAuth, points
// at the protected resource metadata so clients can discover how to sign in.
func AnonymousChallenge(cfg *config.AuthConfig) func(*http.Request) string {
	value := `Bearer realm="` + defaultRealm + `"`
	if cfg != nil && cfg.OAuth != nil {
		if cfg.OAuth.Realm != "" {
			value = `Bearer realm="` + sanitizeHeaderValue(cfg.OAuth.Realm) + `"`
		}
		if cfg.OAuth.ResourceURL != "" {
			value += `, resource_metadata="` + sanitizeHeaderValue(cfg.OAuth.ResourceURL) +
				`/.well-known/oauth-protected-resource"`
		}
	}
	return func(*http.Request) string { return value }
}
