package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestRequireRole(t *testing.T) {
	t.Parallel()

	// A simple authz config where:
	// - superAdmin requires claim "role" == "admin"
	// - manageSources requires claim "role" == "editor"
	authzCfg := &config.AuthzConfig{
		Roles: config.RolesConfig{
			SuperAdmin: []map[string]any{
				{"role": "admin"},
			},
			ManageSources: []map[string]any{
				{"role": "editor"},
			},
		},
	}

	// okHandler is the inner handler that writes 200 if the middleware passes through.
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name           string
		authzCfg       *config.AuthzConfig
		requiredRole   Role
		claims         jwt.MapClaims
		setClaims      bool
		expectedStatus int
	}{
		{
			name:           "nil authz config passes through",
			authzCfg:       nil,
			requiredRole:   RoleManageSources,
			setClaims:      false,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "nil claims (anonymous mode) passes through",
			authzCfg:       authzCfg,
			requiredRole:   RoleManageSources,
			setClaims:      false,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "claims present and user has required role",
			authzCfg:       authzCfg,
			requiredRole:   RoleManageSources,
			claims:         jwt.MapClaims{"role": "editor"},
			setClaims:      true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "claims present but user lacks required role",
			authzCfg:       authzCfg,
			requiredRole:   RoleManageSources,
			claims:         jwt.MapClaims{"role": "viewer"},
			setClaims:      true,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "superAdmin bypasses any required role",
			authzCfg:       authzCfg,
			requiredRole:   RoleManageSources,
			claims:         jwt.MapClaims{"role": "admin"},
			setClaims:      true,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := RequireRole(tt.requiredRole, tt.authzCfg)(okHandler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.setClaims {
				ctx := ContextWithClaims(context.Background(), tt.claims)
				req = req.WithContext(ctx)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)
		})
	}
}
