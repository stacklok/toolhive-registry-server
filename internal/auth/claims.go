package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

// claimsContextKey is the context key for storing JWT claims.
// Uses a private type to prevent collisions with other packages.
type claimsContextKey struct{}

// rolesContextKey is the context key for storing resolved authorization roles.
type rolesContextKey struct{}

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

// ContextWithRoles returns a new context with the resolved roles stored.
func ContextWithRoles(ctx context.Context, roles []Role) context.Context {
	return context.WithValue(ctx, rolesContextKey{}, roles)
}

// RolesFromContext extracts resolved roles from the context.
// Returns nil if no roles are present.
func RolesFromContext(ctx context.Context) []Role {
	roles, _ := ctx.Value(rolesContextKey{}).([]Role)
	return roles
}

// IsSuperAdmin returns true if the context contains the superAdmin role.
func IsSuperAdmin(ctx context.Context) bool {
	return HasRole(RolesFromContext(ctx), RoleSuperAdmin)
}
