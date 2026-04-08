package v1

import (
	"net/http"

	"github.com/stacklok/toolhive-registry-server/internal/api/common"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
)

// meResponse is the JSON response for the GET /v1/me endpoint.
type meResponse struct {
	Subject     string         `json:"subject"`
	Roles       []string       `json:"roles"`
	Permissions permissionsMap `json:"permissions"`
}

// permissionsMap represents the effective permissions for the authenticated caller.
type permissionsMap struct {
	SuperAdmin       bool `json:"super_admin"`
	ManageSources    bool `json:"manage_sources"`
	ManageRegistries bool `json:"manage_registries"`
	ManageEntries    bool `json:"manage_entries"`
}

// getMe handles GET /v1/me
//
// @Summary		Get current user info
// @Description	Returns the authenticated caller's identity and effective permissions
// @Tags		v1
// @Produce		json
// @Success		200	{object}	meResponse		"Caller identity and permissions"
// @Failure		401	{object}	map[string]string	"Unauthorized"
// @Router		/v1/me [get]
func (*Routes) getMe(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		common.WriteErrorResponse(w, "authentication required", http.StatusUnauthorized)
		return
	}

	subject, _ := claims["sub"].(string)
	roles := auth.RolesFromContext(r.Context())

	roleStrings := make([]string, 0, len(roles))
	for _, role := range roles {
		roleStrings = append(roleStrings, string(role))
	}

	permissions := permissionsMap{
		SuperAdmin:       auth.HasRole(roles, auth.RoleSuperAdmin),
		ManageSources:    auth.HasRole(roles, auth.RoleManageSources),
		ManageRegistries: auth.HasRole(roles, auth.RoleManageRegistries),
		ManageEntries:    auth.HasRole(roles, auth.RoleManageEntries),
	}

	common.WriteJSONResponse(w, meResponse{
		Subject:     subject,
		Roles:       roleStrings,
		Permissions: permissions,
	}, http.StatusOK)
}
