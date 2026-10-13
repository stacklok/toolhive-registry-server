package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

// claimsContextKey is the context key for storing JWT claims.
// Uses a private type to prevent collisions with other packages.
type claimsContextKey struct{}

// ContextWithClaims returns a new context with the JWT claims stored.
func ContextWithClaims(ctx context.Context, claims jwt.MapClaims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// ClaimsFromContext extracts JWT claims from the context.
// Returns nil if no claims are present (e.g., anonymous mode).
func ClaimsFromContext(ctx context.Context) jwt.MapClaims {
	claims, _ := ctx.Value(claimsContextKey{}).(jwt.MapClaims)
	return claims
}
