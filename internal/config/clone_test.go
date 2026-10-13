package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
)

func TestConfigCloneOwnsEveryDecodedField(t *testing.T) {
	t.Parallel()
	meta, sampling := 123, 0.5
	claims := func() map[string]any {
		return map[string]any{"team": []string{"original"}, "raw": []any{map[string]any{"nested": "original"}}}
	}
	roles := func() []map[string]any { return []map[string]any{claims()} }
	filter := func() *NameFilterConfig {
		return &NameFilterConfig{Include: []string{"original"}, Exclude: []string{"original"}}
	}
	cfg := &Config{
		Sources: []SourceConfig{{Name: "original", Git: &GitConfig{Repository: "original", Auth: &GitAuthConfig{Username: "original"}},
			API: &APIConfig{Endpoint: "original"}, File: &FileConfig{Data: "original"}, Managed: &ManagedConfig{},
			Kubernetes: &KubernetesConfig{Namespaces: []string{"original"}}, SyncPolicy: &SyncPolicyConfig{Interval: "original"},
			Filter: &FilterConfig{Names: filter(), Tags: filter()}, Claims: claims()}},
		Registries: []RegistryConfig{{Name: "original", Sources: []string{"original"}, Claims: claims()}},
		Database: &DatabaseConfig{Host: "original", Password: "synthetic-app-secret", MigrationPassword: "synthetic-migration-secret",
			MaxMetaSize: &meta, DynamicAuth: &DynamicAuthConfig{AWSRDSIAM: &DynamicAuthAWSRDSIAM{Region: "original"}}},
		Auth: &AuthConfig{Mode: AuthModeOAuth, PublicPaths: []string{"original"}, OAuth: &OAuthConfig{Providers: []OAuthProviderConfig{{Name: "original"}}},
			Authz: &AuthzConfig{Roles: RolesConfig{SuperAdmin: roles(), ManageSources: roles(), ManageRegistries: roles(), ManageEntries: roles()}}},
		Authorization: &AuthorizationConfig{PolicyFile: "original", IdentityMappings: []IdentityMapping{{Issuer: "original",
			Groups: &AttributeMapping{Claim: "original"}, Scopes: &AttributeMapping{Claim: "original"}}}},
		Audit:              &AuditConfig{EventTypes: []string{"original"}, ExcludeEventTypes: []string{"original"}},
		Telemetry:          &telemetry.Config{Tracing: &telemetry.TracingConfig{Sampling: &sampling}, Metrics: &telemetry.MetricsConfig{}},
		publicPathsPresent: true, insecureAllowHTTP: true, compressResponse: true,
		exposureRetired: claims(), exposureEnv: map[string]string{"AUTH_ROLES": "original"},
	}
	baseline, clone := cfg.Clone(), cfg.Clone()
	require.True(t, reflect.DeepEqual(cfg, clone), "copy must preserve private evidence and JSON-excluded credentials")
	review, err := cfg.ExposureReview("sha256:" + strings.Repeat("a", 64))
	require.NoError(t, err)
	copiedReview, err := clone.ExposureReview("sha256:" + strings.Repeat("a", 64))
	require.NoError(t, err)
	require.Equal(t, review, copiedReview)
	mutateConfigFields(reflect.ValueOf(clone))
	clone.exposureRetired["team"].([]string)[0] = "changed"
	clone.exposureRetired["raw"].([]any)[0].(map[string]any)["nested"] = "changed"
	clone.exposureEnv["AUTH_ROLES"] = "changed"
	require.True(t, reflect.DeepEqual(cfg, baseline), "every nested field must be independently owned")
	require.ErrorContains(t, cfg.Clone().ValidateCatalogCutover(), "retired config fields")
	cfg.exposureRetired = nil
	require.ErrorContains(t, cfg.Clone().ValidateCatalogCutover(), "retired environment override")
	require.Nil(t, (*Config)(nil).Clone())
}

// Exercise every populated exported leaf so new pointer/slice fields cannot
// silently acquire aliases. Production copying remains explicit, not reflective.
//
//nolint:exhaustive // Visit only the kinds present in decoded config; other scalar kinds have no aliases.
func mutateConfigFields(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			mutateConfigFields(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				mutateConfigFields(v.Field(i))
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			mutateConfigFields(v.Index(i))
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			mutateConfigFields(v.MapIndex(key))
		}
		if v.Type().Key().Kind() == reflect.String {
			v.SetMapIndex(reflect.ValueOf("changed"), reflect.Zero(v.Type().Elem()))
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString("changed")
		}
	case reflect.Bool:
		if v.CanSet() {
			v.SetBool(!v.Bool())
		}
	case reflect.Int, reflect.Int32:
		if v.CanSet() {
			v.SetInt(v.Int() + 1)
		}
	case reflect.Float64:
		if v.CanSet() {
			v.SetFloat(v.Float() + 1)
		}
	}
}
