package auth

//go:generate mockgen -destination=mocks/mock_validator.go -package=mocks -source=validator.go TokenValidatorInterface

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

// TokenValidatorInterface abstracts token validation for testability.
// This allows mocking the toolhive auth.TokenValidator in tests.
type TokenValidatorInterface interface {
	ValidateToken(ctx context.Context, token string) (jwt.MapClaims, error)
}
