package config

import (
	"context"
	"errors"
	"strings"

	"github.com/spf13/viper"
)

// LoadCatalogConfig uses the same YAML/env snapshot decoder without the legacy
// source/view monolith's validation (which rejects public inline definitions).
// NewCatalogApp validates final source/admission settings after overrides, before
// assembly IO. The loader still validates database settings before those overrides.
func LoadCatalogConfig(opts ...Option) (*Config, error) {
	var snapshot []byte
	opts = append(opts, func(c *loaderConfig) error { c.exposureData = &snapshot; return nil })
	return LoadConfig(opts...)
}

// ValidateCatalogCutover is used only by the public-component assembly. The
// legacy loader retains its fields for the CLI and offline exposure inventory.
func (c *Config) ValidateCatalogCutover() error {
	if c == nil {
		return errors.New("config is required")
	}
	if len(c.exposureRetired) != 0 {
		return errors.New("retired config fields present; remove auth.authz/auth.roles/auth.publicPaths " +
			"and source/registry claims before cutover")
	}
	for key := range c.exposureEnv {
		if retiredEnvironmentKey(key) {
			return errors.New("retired environment override present; unset reserved authorization/claims variables before cutover")
		}
	}
	if c.Auth != nil && (c.Auth.Authz != nil || c.Auth.PublicPaths != nil) {
		return errors.New("retired auth configuration is not supported by catalog assembly")
	}
	for _, s := range c.Sources {
		if s.Claims != nil {
			return errors.New("source claims are retired; remove the field before cutover")
		}
	}
	for _, v := range c.Registries {
		if v.Claims != nil {
			return errors.New("registry claims are retired; remove the field before cutover")
		}
	}
	if err := c.ValidateAdmission(); err != nil {
		return err
	}
	if err := c.validateAudit(); err != nil {
		return err
	}
	return c.validateStorageConfig()
}

func freezeCatalogEnvironment(v *viper.Viper, environment []string) {
	values := map[string]string{}
	for _, item := range environment {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	for _, key := range v.AllKeys() {
		envKey := EnvPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		if value := values[envKey]; value != "" {
			v.Set(key, value)
		}
	}
	// These switches are env-only, even if unknown YAML keys use their spelling.
	for _, key := range []string{"insecure_url", "compress_response", "watch_namespace", "leader_election_id"} {
		v.Set(key, values[EnvPrefix+"_"+strings.ToUpper(key)])
	}
}

func retiredEnvironmentKey(key string) bool {
	key = strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	return strings.HasPrefix(key, "AUTH_AUTHZ") || strings.HasPrefix(key, "AUTH_ROLES") ||
		strings.HasPrefix(key, "AUTH_PUBLICPATHS") ||
		((strings.HasPrefix(key, "SOURCES_") || strings.HasPrefix(key, "REGISTRIES_")) && strings.Contains(key, "CLAIMS"))
}

// LoadSecret resolves the canonical secure file reference and checks cancellation
// around the filesystem operation. Errors deliberately omit paths and contents.
func LoadSecret(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, err := readSecretFromFile(path)
	if err != nil {
		return "", errors.New("secret reference unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return value, nil
}
