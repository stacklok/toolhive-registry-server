package auth

import (
	"github.com/golang-jwt/jwt/v5"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// Role represents an authorization role
type Role string

const (
	// RoleSuperAdmin grants access to all operations, bypassing claim checks.
	RoleSuperAdmin Role = "superAdmin"
	// RoleManageSources grants access to source management operations.
	RoleManageSources Role = "manageSources"
	// RoleManageRegistries grants access to registry management operations.
	RoleManageRegistries Role = "manageRegistries"
	// RoleManageEntries grants access to entry management operations.
	RoleManageEntries Role = "manageEntries"
)

// ResolveRoles returns all roles the user has based on JWT claims and authz config.
// If authzCfg is nil, no roles are returned (only authenticated access is possible).
func ResolveRoles(claims jwt.MapClaims, authzCfg *config.AuthzConfig) []Role {
	if authzCfg == nil || claims == nil {
		return nil
	}

	var roles []Role

	if matchesRoleRules(claims, authzCfg.Roles.SuperAdmin) {
		roles = append(roles, RoleSuperAdmin)
	}
	if matchesRoleRules(claims, authzCfg.Roles.ManageSources) {
		roles = append(roles, RoleManageSources)
	}
	if matchesRoleRules(claims, authzCfg.Roles.ManageRegistries) {
		roles = append(roles, RoleManageRegistries)
	}
	if matchesRoleRules(claims, authzCfg.Roles.ManageEntries) {
		roles = append(roles, RoleManageEntries)
	}

	return roles
}

// AllRoles returns every role defined in the system.
// Used when no authz config is provided — authenticated users implicitly hold all permissions.
func AllRoles() []Role {
	return []Role{RoleSuperAdmin, RoleManageSources, RoleManageRegistries, RoleManageEntries}
}

// HasRole checks if the resolved roles contain the specified role.
// superAdmin grants access to everything.
func HasRole(roles []Role, required Role) bool {
	for _, r := range roles {
		if r == RoleSuperAdmin || r == required {
			return true
		}
	}
	return false
}

// matchesRoleRules checks if claims match any of the role rule maps (OR logic).
func matchesRoleRules(claims jwt.MapClaims, rules []map[string]any) bool {
	for _, ruleMap := range rules {
		if matchesClaimMap(claims, ruleMap) {
			return true
		}
	}
	return false
}

// matchesClaimMap checks if all entries in the claim map match the JWT claims (AND logic).
func matchesClaimMap(claims jwt.MapClaims, claimMap map[string]any) bool {
	if len(claimMap) == 0 {
		return false
	}
	for key, required := range claimMap {
		jwtValue, ok := claims[key]
		if !ok {
			return false
		}
		if !matchesClaimValue(jwtValue, required) {
			return false
		}
	}
	return true
}

// matchesClaimValue checks if a JWT claim value matches the required value.
// Required can be a string or []any (OR logic within array).
// JWT value can be a string or []any.
func matchesClaimValue(jwtValue, requiredValue any) bool {
	// Normalize required to a slice of strings
	requiredValues := toStringSlice(requiredValue)
	if len(requiredValues) == 0 {
		return false
	}

	// Normalize JWT value to a slice of strings
	jwtValues := toStringSlice(jwtValue)
	if len(jwtValues) == 0 {
		return false
	}

	// Check if any JWT value matches any required value (OR logic)
	for _, jv := range jwtValues {
		for _, rv := range requiredValues {
			if jv == rv {
				return true
			}
		}
	}
	return false
}

// toStringSlice converts a value to a slice of strings.
// Supports string, []string, []any (with string elements).
func toStringSlice(v any) []string {
	switch val := v.(type) {
	case string:
		return []string{val}
	case []string:
		return val
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}
