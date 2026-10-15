package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExposureConfigStableProjectionAndRetiredPresence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.cedar")
	require.NoError(t, os.WriteFile(policy, []byte("private-policy-sentinel"), 0600))
	path := filepath.Join(dir, "config.yaml")
	content := exposureConfigFixture(policy)
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	cfg, review, err := LoadExposureConfig(path)
	require.NoError(t, err)
	require.Equal(t, "private-password-sentinel", cfg.Database.Password)
	require.Equal(t, []string{"auth.authz", "auth.publicpaths", "registries[0].claims", "sources[0].claims"}, review.RetiredFields)
	_, again, err := LoadExposureConfig(path)
	require.NoError(t, err)
	require.Equal(t, review, again)
	serveCfg, err := LoadConfig(WithConfigPath(path))
	require.NoError(t, err)
	require.ErrorContains(t, serveCfg.ValidateAdmission(), "auth.publicPaths", "serve preflight must reject retired bypass fields")
	encoded, err := json.Marshal(review)
	require.NoError(t, err)
	for _, secret := range []string{"private-policy-sentinel", "private-password-sentinel", "private-claim-sentinel"} {
		require.NotContains(t, string(encoded), secret)
	}
	for _, mutation := range []struct {
		old, replacement string
		invalidates      bool
	}{
		{"claims: null", "claims: {}", true},
		{"authz: null", "authz: {}", true},
		{"publicPaths: null", "publicPaths: []", true},
		{"publicPaths: null", "", true},
		{"allowAnonymousCatalog: false", "allowAnonymousCatalog: true", true},
		{"enabled: false", "enabled: true", true},
		{"sources: [future]", "sources: []", true},
		{"managed: {}", "file: {data: '{}'}", true},
		{"mode: anonymous", "mode: oauth", true},
		{"private-password-sentinel", "different-credential", false},
	} {
		changedContent := strings.Replace(content, mutation.old, mutation.replacement, 1)
		require.NoError(t, os.WriteFile(path, []byte(changedContent), 0600))
		_, changed, err := LoadExposureConfig(path)
		require.NoError(t, err)
		if mutation.invalidates {
			require.NotEqual(t, review.Revision, changed.Revision, mutation.old)
		} else {
			require.Equal(t, review.Revision, changed.Revision)
		}
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	require.NoError(t, os.WriteFile(policy, []byte("changed-policy-at-same-path"), 0600))
	_, changed, err := LoadExposureConfig(path)
	require.NoError(t, err)
	require.NotEqual(t, review.Revision, changed.Revision, "policy CONTENTS, not only file path, bind review")
}

//nolint:paralleltest // Environment overrides are process-global.
func TestExposureConfigEnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.cedar")
	require.NoError(t, os.WriteFile(policy, []byte("policy"), 0600))
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(exposureConfigFixture(policy)), 0600))
	_, before, err := LoadExposureConfig(path)
	require.NoError(t, err)
	t.Setenv("THV_REGISTRY_ADMIN_ENABLED", "true")
	cfg, after, err := LoadExposureConfig(path)
	require.NoError(t, err)
	require.True(t, cfg.Admin.Enabled)
	require.NotEqual(t, before.Revision, after.Revision)
	t.Setenv("THV_REGISTRY_AUTH_PUBLICPATHS", "[]")
	_, present, err := LoadExposureConfig(path)
	require.NoError(t, err)
	require.Contains(t, present.RetiredFields, "env.auth.publicpaths")
	require.NotEqual(t, after.Revision, present.Revision)
	otherPolicy := filepath.Join(dir, "other.cedar")
	require.NoError(t, os.WriteFile(otherPolicy, []byte("other-policy"), 0600))
	t.Setenv("THV_REGISTRY_AUTHORIZATION_POLICYFILE", otherPolicy)
	cfg, override, err := LoadExposureConfig(path)
	require.NoError(t, err)
	require.Equal(t, otherPolicy, cfg.Authorization.PolicyFile)
	require.NotEqual(t, present.Revision, override.Revision)
	require.NoError(t, os.WriteFile(otherPolicy, []byte("edited-override-policy"), 0600))
	_, edited, err := LoadExposureConfig(path)
	require.NoError(t, err)
	require.NotEqual(t, override.Revision, edited.Revision)
}

func TestExposureConfigSafeFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, data string }{
		{"parse", "database: [private-secret-sentinel"},
		{"decode", "database: private-secret-sentinel"},
		{"missing DB", "auth: {publicPaths: null}"},
		{"policy unavailable", exposureConfigFixture("/private-secret-sentinel/missing")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.data), 0600))
			_, _, err := LoadExposureConfig(path)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-secret-sentinel")
		})
	}
	_, _, err := LoadExposureConfig("/private-secret-sentinel/nonexistent")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-secret-sentinel")
}

func TestExposureReviewUsesLoadedSnapshotsAndFinalOverrides(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.cedar")
	policyBytes := []byte("loaded-policy")
	require.NoError(t, os.WriteFile(policy, policyBytes, 0600))
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(exposureConfigFixture(policy)), 0600))
	cfg, offline, err := LoadExposureConfig(path)
	require.NoError(t, err)
	loadedRevision := fmt.Sprintf("sha256:%x", sha256.Sum256(policyBytes))
	loaded, err := cfg.ExposureReview(loadedRevision)
	require.NoError(t, err)
	require.Equal(t, offline, loaded, "offline and loaded-policy fingerprints use the same encoding")

	require.NoError(t, os.WriteFile(policy, []byte("different-policy"), 0600))
	require.NoError(t, os.WriteFile(path, []byte("invalid: ["), 0600))
	stillLoaded, err := cfg.ExposureReview(loadedRevision)
	require.NoError(t, err)
	require.Equal(t, offline, stillLoaded, "never reread either file to bind startup evidence")

	cfg.Admin.Enabled = true
	cfg.WatchNamespace = "final-cli-override"
	changed, err := cfg.ExposureReview(loadedRevision)
	require.NoError(t, err)
	require.NotEqual(t, offline.Revision, changed.Revision)
	require.Equal(t, offline.RetiredFields, changed.RetiredFields)
	cfg.Database.Password = "changed-credential"
	same, err := cfg.ExposureReview(loadedRevision)
	require.NoError(t, err)
	require.Equal(t, changed, same)
	for _, revision := range []string{"", "absent", "sha256:short", "sha256:" + strings.Repeat("A", 64),
		"sha256:" + strings.Repeat("z", 64)} {
		_, err := cfg.ExposureReview(revision)
		require.Error(t, err)
	}
	_, err = (*Config)(nil).ExposureReview(loadedRevision)
	require.Error(t, err)
}

//nolint:paralleltest // Environment overrides are process-global.
func TestExposureReviewKeepsLoadedEnvironment(t *testing.T) {
	dir := t.TempDir()
	policy := filepath.Join(dir, "policy.cedar")
	require.NoError(t, os.WriteFile(policy, []byte("policy"), 0600))
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(exposureConfigFixture(policy)), 0600))
	t.Setenv("THV_REGISTRY_ADMIN_ENABLED", "true")
	cfg, offline, err := LoadExposureConfig(path)
	require.NoError(t, err)
	t.Setenv("THV_REGISTRY_ADMIN_ENABLED", "false")
	loaded, err := cfg.ExposureReview(fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("policy"))))
	require.NoError(t, err)
	require.Equal(t, offline, loaded)
	require.True(t, cfg.Admin.Enabled)
}

func exposureConfigFixture(policy string) string {
	return fmt.Sprintf(`database:
 host: localhost
 port: 5432
 user: registry
 database: registry
 password: private-password-sentinel
sources:
 - name: future
   managed: {}
   claims: null
registries:
 - name: future-view
   sources: [future]
   claims: {team: private-claim-sentinel}
auth:
 mode: anonymous
 authz: null
 publicPaths: null
admin:
 enabled: false
authorization:
 policyFile: %s
 allowAnonymousCatalog: false
`, policy)
}
