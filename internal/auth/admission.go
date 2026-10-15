package auth

import (
	"context"
	"net/http"

	"github.com/golang-jwt/jwt/v5"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

type verifiedIdentityKey struct{}
type admissionChallengeKey struct{}

// VerifiedIdentity is evidence established only by authentication, not by claims context.
type VerifiedIdentity struct {
	Issuer    string
	Subject   string
	Anonymous bool
	Claims    jwt.MapClaims
}

// VerifiedIdentityFromContext distinguishes missing middleware from explicit anonymity.
func VerifiedIdentityFromContext(ctx context.Context) (VerifiedIdentity, bool) {
	identity, ok := ctx.Value(verifiedIdentityKey{}).(VerifiedIdentity)
	return identity, ok
}

// NewAdmissionAuthMiddleware permits absent credentials only by explicit catalog opt-in.
// Supplied credentials always pass through validation; anonymous mode rejects them.
func NewAdmissionAuthMiddleware(ctx context.Context, cfg *config.AuthConfig, allowAnonymous bool,
	factory validatorFactory) (func(http.Handler) http.Handler, http.Handler, error) {
	mw, info, err := NewAuthMiddleware(ctx, cfg, factory)
	if err != nil {
		return nil, nil, err
	}
	challenge := &multiProviderMiddleware{realm: defaultRealm}
	if cfg.Mode == config.AuthModeOAuth {
		challenge.resourceURL = cfg.OAuth.ResourceURL
		if cfg.OAuth.Realm != "" {
			challenge.realm = cfg.OAuth.Realm
		}
	}
	return func(next http.Handler) http.Handler {
		authenticated := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(context.WithValue(r.Context(), admissionChallengeKey{}, challenge))
			headers, present := r.Header["Authorization"]
			if present && (len(headers) != 1 || len(headers[0]) > 65536) {
				WriteAdmissionUnauthorized(w, r, "invalid credentials")
				return
			}
			if !present && allowAnonymous {
				identity := VerifiedIdentity{Anonymous: true}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), verifiedIdentityKey{}, identity)))
				return
			}
			if cfg.Mode == config.AuthModeAnonymous {
				WriteAdmissionUnauthorized(w, r, "authentication required")
				return
			}
			authenticated.ServeHTTP(w, r)
		})
	}, info, nil
}

// NewCatalogAuthMiddleware keeps verified claims available for evaluation while
// projecting only the bounded principal ID into authentication/access/audit logs.
// The legacy CLI continues to use NewAdmissionAuthMiddleware.
func NewCatalogAuthMiddleware(ctx context.Context, cfg *config.AuthConfig, allowAnonymous bool,
	factory validatorFactory) (func(http.Handler) http.Handler, http.Handler, error) {
	mw, info, err := NewAdmissionAuthMiddleware(ctx, cfg, allowAnonymous, factory)
	if err != nil {
		return nil, nil, err
	}
	return func(next http.Handler) http.Handler {
		authenticated := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), catalogProjectionKey{}, true)
			authenticated.ServeHTTP(w, r.WithContext(ctx))
		})
	}, info, nil
}

// WriteAdmissionUnauthorized retains the configured OAuth challenge for rejections
// after optional authentication. Descriptions must be static, never claim values.
func WriteAdmissionUnauthorized(w http.ResponseWriter, r *http.Request, description string) {
	challenge, ok := r.Context().Value(admissionChallengeKey{}).(*multiProviderMiddleware)
	if !ok {
		challenge = &multiProviderMiddleware{realm: defaultRealm}
	}
	code := errorCodeInvalidRequest
	if identity, established := VerifiedIdentityFromContext(r.Context()); established && !identity.Anonymous {
		code = errorCodeInvalidToken
	}
	challenge.writeError(w, code, description)
}
