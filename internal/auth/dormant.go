package auth

import (
	"context"
	"sync"

	"github.com/golang-jwt/jwt/v5"
	thvauth "github.com/stacklok/toolhive/pkg/auth"
)

// DormantValidatorFactory delays provider IO/refresh construction until the first
// authenticated request. The lifetime context belongs to the application; no
// authentication worker can start while CatalogApp is merely being prepared.
var DormantValidatorFactory validatorFactory = func(
	ctx context.Context, cfg thvauth.TokenValidatorConfig,
) (tokenValidatorInterface, error) {
	return &dormantValidator{lifetime: ctx, config: cfg}, nil
}

type dormantValidator struct {
	lifetime  context.Context
	config    thvauth.TokenValidatorConfig
	mu        sync.Mutex
	validator tokenValidatorInterface
}

func (v *dormantValidator) ValidateToken(ctx context.Context, token string) (jwt.MapClaims, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v.mu.Lock()
	if v.validator == nil {
		validator, err := DefaultValidatorFactory(v.lifetime, v.config)
		if err != nil {
			v.mu.Unlock()
			return nil, err
		}
		v.validator = validator
	}
	validator := v.validator
	v.mu.Unlock()
	return validator.ValidateToken(ctx, token)
}
