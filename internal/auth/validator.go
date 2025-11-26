package auth

//go:generate mockgen -destination=mocks/mock_validator.go -package=mocks -source=validator.go tokenValidatorInterface

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

// tokenValidatorInterface abstracts token validation for testability.
// This allows mocking the toolhive auth.TokenValidator in tests.
type tokenValidatorInterface interface {
	ValidateToken(ctx context.Context, token string) (jwt.MapClaims, error)
}
