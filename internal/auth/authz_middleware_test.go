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

func TestResolveRolesMiddleware(t *testing.T) {
	// NOTE: subtests run sequentially (not t.Parallel on subtests) because
	// capturedRoles is shared and reset between runs.

	var capturedRoles []Role
	capture := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedRoles = RolesFromContext(r.Context())
	})

	authzCfg := &config.AuthzConfig{
		Roles: config.RolesConfig{
			ManageSources: []map[string]any{{"role": "editor"}},
		},
	}

	tests := []struct {
		name         string
		authzCfg     *config.AuthzConfig
		claims       jwt.MapClaims
		setClaims    bool
		wantAllRoles bool
		wantRoles    []Role
		wantNilRoles bool
	}{
		{
			name:         "nil authz + authenticated stores all roles",
			authzCfg:     nil,
			claims:       jwt.MapClaims{"sub": "user-1"},
			setClaims:    true,
			wantAllRoles: true,
		},
		{
			name:         "nil authz + anonymous stores no roles",
			authzCfg:     nil,
			setClaims:    false,
			wantNilRoles: true,
		},
		{
			name:      "authz configured + matching claims resolves roles",
			authzCfg:  authzCfg,
			claims:    jwt.MapClaims{"role": "editor"},
			setClaims: true,
			wantRoles: []Role{RoleManageSources},
		},
		{
			name:         "authz configured + anonymous stores no roles",
			authzCfg:     authzCfg,
			setClaims:    false,
			wantNilRoles: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capturedRoles = nil

			handler := ResolveRolesMiddleware(tt.authzCfg)(capture)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.setClaims {
				req = req.WithContext(ContextWithClaims(context.Background(), tt.claims))
			}
			handler.ServeHTTP(httptest.NewRecorder(), req)

			switch {
			case tt.wantAllRoles:
				assert.Equal(t, AllRoles(), capturedRoles)
			case tt.wantNilRoles:
				assert.Nil(t, capturedRoles)
			default:
				assert.Equal(t, tt.wantRoles, capturedRoles)
			}
		})
	}
}

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
		{
			name:           "nil authz config + authenticated user passes any role check",
			authzCfg:       nil,
			requiredRole:   RoleManageRegistries,
			claims:         jwt.MapClaims{"sub": "user-1"},
			setClaims:      true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "nil authz config + authenticated user passes superAdmin role check",
			authzCfg:       nil,
			requiredRole:   RoleSuperAdmin,
			claims:         jwt.MapClaims{"sub": "user-1"},
			setClaims:      true,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Chain ResolveRolesMiddleware -> RequireRole, matching production setup.
			handler := ResolveRolesMiddleware(tt.authzCfg)(RequireRole(tt.requiredRole, tt.authzCfg)(okHandler))

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
