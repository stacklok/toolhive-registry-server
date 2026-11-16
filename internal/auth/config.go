package auth

import "github.com/stacklok/toolhive/pkg/auth"

// providerConfig holds configuration for one verified token provider.
type providerConfig struct {
	Name            string
	IssuerURL       string
	ValidatorConfig auth.TokenValidatorConfig
}
