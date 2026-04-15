package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
)

func TestGetMe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		claims          jwt.MapClaims
		roles           []auth.Role
		expectedStatus  int
		expectedSubject string
		expectedRoles   []string
	}{
		{
			name:           "anonymous returns 401",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:            "authenticated with no roles",
			claims:          jwt.MapClaims{"sub": "user-1"},
			roles:           []auth.Role{},
			expectedStatus:  http.StatusOK,
			expectedSubject: "user-1",
			expectedRoles:   []string{},
		},
		{
			name:            "single role",
			claims:          jwt.MapClaims{"sub": "user-1"},
			roles:           []auth.Role{auth.RoleManageSources},
			expectedStatus:  http.StatusOK,
			expectedSubject: "user-1",
			expectedRoles:   []string{"manageSources"},
		},
		{
			name:            "multiple roles",
			claims:          jwt.MapClaims{"sub": "user-1"},
			roles:           []auth.Role{auth.RoleManageSources, auth.RoleManageEntries},
			expectedStatus:  http.StatusOK,
			expectedSubject: "user-1",
			expectedRoles:   []string{"manageSources", "manageEntries"},
		},
		{
			name:            "super admin",
			claims:          jwt.MapClaims{"sub": "admin"},
			roles:           []auth.Role{auth.RoleSuperAdmin},
			expectedStatus:  http.StatusOK,
			expectedSubject: "admin",
			expectedRoles:   []string{"superAdmin"},
		},
		{
			name:            "missing sub claim",
			claims:          jwt.MapClaims{"iss": "https://auth.example.com"},
			roles:           []auth.Role{auth.RoleManageSources},
			expectedStatus:  http.StatusOK,
			expectedSubject: "",
			expectedRoles:   []string{"manageSources"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			routes := &Routes{}

			r := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			if tt.claims != nil {
				r = r.WithContext(auth.ContextWithClaims(r.Context(), tt.claims))
			}
			if tt.roles != nil {
				r = r.WithContext(auth.ContextWithRoles(r.Context(), tt.roles))
			}

			w := httptest.NewRecorder()
			routes.getMe(w, r)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusUnauthorized {
				var errResp map[string]string
				require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
				assert.Equal(t, "authentication required", errResp["error"])
				return
			}

			var resp meResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.Equal(t, tt.expectedSubject, resp.Subject)
			assert.Equal(t, tt.expectedRoles, resp.Roles)
		})
	}
}
