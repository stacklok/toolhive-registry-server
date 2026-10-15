package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stacklok/toolhive-registry-server/internal/authorization/policydata"
)

// ExposureConfig contains only safe offline review metadata, never config values.
type ExposureConfig struct {
	Revision      string   `json:"revision"`
	RetiredFields []string `json:"retired_fields"`
}

// LoadExposureConfig loads the effective connection and exposure configuration
// without serve validation, policy evaluation, source initialization or migration.
// Errors deliberately omit file contents, paths and configuration values.
func LoadExposureConfig(path string) (*Config, ExposureConfig, error) {
	var data []byte
	cfg, err := LoadConfig(WithConfigPath(path), func(c *loaderConfig) error {
		c.exposureData = &data
		return nil
	})
	if err != nil {
		return nil, ExposureConfig{}, errors.New("exposure configuration unavailable or invalid")
	}
	if cfg.Authorization == nil {
		cfg.Authorization = &AuthorizationConfig{}
	}
	policyRevision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(policydata.Default)))
	if cfg.Authorization.PolicyFile != "" {
		policyRevision, err = exposurePolicyRevision(cfg.Authorization.PolicyFile)
		if err != nil {
			return nil, ExposureConfig{}, err
		}
	}
	review, err := cfg.currentExposureReview(policyRevision)
	return cfg, review, err
}

// ExposureReview fingerprints the final effective configuration (including CLI
// overrides) against the revision of the actual loaded policy, without rereading
// either file or the environment. It does not validate configuration or authorize
// startup. Raw retired-field presence comes from the same YAML snapshot as decoding.
func (c *Config) ExposureReview(loadedPolicyRevision string) (ExposureConfig, error) {
	if len(loadedPolicyRevision) != len("sha256:")+64 || !strings.HasPrefix(loadedPolicyRevision, "sha256:") ||
		strings.ToLower(loadedPolicyRevision) != loadedPolicyRevision {
		return ExposureConfig{}, errors.New("invalid loaded exposure policy revision")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(loadedPolicyRevision, "sha256:")); err != nil {
		return ExposureConfig{}, errors.New("invalid loaded exposure policy revision")
	}
	return c.currentExposureReview(loadedPolicyRevision)
}

func (c *Config) currentExposureReview(policyRevision string) (ExposureConfig, error) {
	review, err := c.fingerprintExposure(policyRevision)
	if err != nil {
		return ExposureConfig{}, err
	}
	// Preserve the v1 producer encoding, but do not consume pending reviews made
	// under instance-scoped read semantics: unchanged forbids can change meaning.
	digest := sha256.Sum256([]byte("registry-exposure:view-scoped-catalog-read:v2\n" + review.Revision))
	review.Revision = fmt.Sprintf("sha256:%x", digest)
	return review, nil
}

func (c *Config) fingerprintExposure(policyRevision string) (ExposureConfig, error) {
	if c == nil {
		return ExposureConfig{}, errors.New("exposure configuration unavailable")
	}
	retired := c.exposureRetired
	if retired == nil {
		retired = map[string]any{}
	}
	env := c.exposureEnv
	if env == nil {
		env = map[string]string{}
	}
	fields := make([]string, 0, len(retired))
	for field := range retired {
		fields = append(fields, field)
	}

	// Copy before omitting credentials; never change the effective connection.
	sources := append([]SourceConfig(nil), c.Sources...)
	for i := range sources {
		if sources[i].Git != nil {
			git := *sources[i].Git
			git.Auth = nil
			sources[i].Git = &git
		}
	}
	var auth *AuthConfig
	if c.Auth != nil {
		copyAuth := *c.Auth
		if c.Auth.OAuth != nil {
			oauth := *c.Auth.OAuth
			oauth.Providers = append([]OAuthProviderConfig(nil), oauth.Providers...)
			for i := range oauth.Providers {
				oauth.Providers[i].ClientSecretFile = ""
				oauth.Providers[i].AuthTokenFile = ""
			}
			copyAuth.OAuth = &oauth
		}
		auth = &copyAuth
	}
	for key, field := range map[string]string{
		"AUTH_AUTHZ": "env.auth.authz", "AUTH_ROLES": "env.auth.roles", "AUTH_PUBLICPATHS": "env.auth.publicpaths",
	} {
		if _, present := env[key]; present {
			fields = append(fields, field)
		}
	}
	sort.Strings(fields)
	projection := struct {
		Version       int
		Sources       []SourceConfig
		Views         []RegistryConfig
		Auth          *AuthConfig
		Authorization *AuthorizationConfig
		Admin         AdminConfig
		Namespace     string
		Retired       map[string]any
		Environment   map[string]string
		Policy        string
	}{1, sources, c.Registries, auth, c.Authorization, c.Admin, c.WatchNamespace, retired, env, policyRevision}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return ExposureConfig{}, errors.New("exposure configuration cannot be fingerprinted")
	}
	return ExposureConfig{Revision: fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), RetiredFields: fields}, nil
}

func retiredExposureFields(data []byte) (map[string]any, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, errors.New("invalid YAML configuration")
	}
	retired := map[string]any{}
	collectRetiredFields(raw, "", retired)
	if len(retired) == 0 {
		return nil, nil
	}
	return retired, nil
}

func exposureEnvironment() map[string]string { return exposureEnvironmentFrom(os.Environ()) }

//nolint:gocyclo // Preserve security override presence without retaining credential values.
func exposureEnvironmentFrom(environment []string) map[string]string {
	env := map[string]string{}
	for _, item := range environment {
		key, value, ok := strings.Cut(item, "=")
		key = strings.ToUpper(key)
		if !ok || !strings.HasPrefix(key, EnvPrefix+"_") {
			continue
		}
		key = strings.ReplaceAll(strings.ToUpper(strings.TrimPrefix(key, EnvPrefix+"_")), ".", "_")
		if strings.HasPrefix(key, "AUTH_") || strings.HasPrefix(key, "AUTHORIZATION_") ||
			strings.HasPrefix(key, "ADMIN_") || strings.HasPrefix(key, "SOURCES_") ||
			strings.HasPrefix(key, "REGISTRIES_") || key == "WATCH_NAMESPACE" || key == "INSECURE_URL" {
			if !strings.Contains(key, "PASSWORD") && !strings.Contains(key, "SECRET") && !strings.Contains(key, "TOKEN") {
				env[key] = value
			} else if retiredEnvironmentKey(key) {
				env[key] = "" // Preserve forbidden presence without credential values.
			}
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

func exposurePolicyRevision(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("exposure policy must be a bounded readable regular file at an absolute path")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("exposure policy unavailable")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return "", errors.New("exposure policy unavailable or oversized")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", errors.New("exposure policy unavailable")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("exposure policy must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return "", errors.New("exposure policy unreadable or oversized")
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

var retiredSourceField = regexp.MustCompile(`^(sources|registries)(\[[0-9]+\]|\.[0-9]+)\.claims($|\.)`)

func collectRetiredFields(value any, path string, fields map[string]any) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			key = strings.ToLower(key)
			next := key
			if path != "" {
				next = path + "." + key
			}
			if next == "auth.authz" || next == "auth.publicpaths" || next == "auth.roles" ||
				strings.HasPrefix(next, "auth.authz.") || strings.HasPrefix(next, "auth.roles.") ||
				strings.HasPrefix(next, "auth.publicpaths.") || retiredSourceField.MatchString(next) {
				fields[next] = child
				continue
			}
			collectRetiredFields(child, next, fields)
		}
	case []any:
		for i, child := range value {
			collectRetiredFields(child, fmt.Sprintf("%s[%d]", path, i), fields)
		}
	}
}
