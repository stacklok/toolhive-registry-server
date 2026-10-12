package config

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
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
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, ExposureConfig{}, errors.New("exposure configuration invalid")
	}
	retired := map[string]any{}
	collectRetiredFields(raw, "", retired)
	fields := make([]string, 0, len(retired))
	for field := range retired {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	// Copy before omitting credentials; never change the effective connection.
	sources := append([]SourceConfig(nil), cfg.Sources...)
	for i := range sources {
		if sources[i].Git != nil {
			git := *sources[i].Git
			git.Auth = nil
			sources[i].Git = &git
		}
	}
	var auth *AuthConfig
	if cfg.Auth != nil {
		copyAuth := *cfg.Auth
		if cfg.Auth.OAuth != nil {
			oauth := *cfg.Auth.OAuth
			oauth.Providers = append([]OAuthProviderConfig(nil), oauth.Providers...)
			for i := range oauth.Providers {
				oauth.Providers[i].ClientSecretFile = ""
				oauth.Providers[i].AuthTokenFile = ""
			}
			copyAuth.OAuth = &oauth
		}
		auth = &copyAuth
	}
	policyRevision := "absent"
	if cfg.Authorization != nil && cfg.Authorization.PolicyFile != "" {
		policyRevision, err = exposurePolicyRevision(cfg.Authorization.PolicyFile)
		if err != nil {
			return nil, ExposureConfig{}, err
		}
	}
	env := exposureEnvironment()
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
	}{1, sources, cfg.Registries, auth, cfg.Authorization, cfg.Admin, cfg.WatchNamespace, retired, env, policyRevision}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return nil, ExposureConfig{}, errors.New("exposure configuration cannot be fingerprinted")
	}
	return cfg, ExposureConfig{Revision: fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), RetiredFields: fields}, nil
}

func exposureEnvironment() map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok || !strings.HasPrefix(key, EnvPrefix+"_") {
			continue
		}
		key = strings.TrimPrefix(key, EnvPrefix+"_")
		if strings.HasPrefix(key, "AUTH_") || strings.HasPrefix(key, "AUTHORIZATION_") ||
			strings.HasPrefix(key, "ADMIN_") || strings.HasPrefix(key, "SOURCES_") ||
			strings.HasPrefix(key, "REGISTRIES_") || key == "WATCH_NAMESPACE" || key == "INSECURE_URL" {
			if !strings.Contains(key, "PASSWORD") && !strings.Contains(key, "SECRET") && !strings.Contains(key, "TOKEN") {
				env[key] = value
			}
		}
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
				key == "claims" && strings.HasSuffix(path, "]") &&
					(strings.HasPrefix(path, "sources[") || strings.HasPrefix(path, "registries[")) && !strings.Contains(path, ".") {
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
