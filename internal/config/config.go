// Package config provides configuration loading and management for the registry server.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
)

// SourceType represents the type of registry data source
type SourceType string

const (
	// SourceTypeGit is the type for registry data stored in Git repositories
	SourceTypeGit SourceType = "git"

	// SourceTypeAPI is the type for registry data fetched from API endpoints
	SourceTypeAPI SourceType = "api"

	// SourceTypeFile is the type for registry data stored in local files
	SourceTypeFile SourceType = "file"

	// SourceTypeManaged is the type for registries directly managed via API
	// Managed registries do not sync from external sources
	SourceTypeManaged SourceType = "managed"

	// SourceTypeKubernetes is the type for registries that query Kubernetes deployments
	// Kubernetes registries discover MCP servers from running Kubernetes resources
	SourceTypeKubernetes SourceType = "kubernetes"
)

const (
	// EnvPrefix is the prefix used for environment variables that override config values.
	// For example, THV_REGISTRY_REGISTRYNAME overrides registryName in the config file.
	EnvPrefix = "THV_REGISTRY"

	// DefaultMaxMetaSize is the default maximum allowed size in bytes for
	// publisher-provided metadata extensions (_meta). 65536 bytes = 64KB.
	DefaultMaxMetaSize = 65536
)

const (
	// SourceFormatToolHive is the native ToolHive registry format
	SourceFormatToolHive = "toolhive"

	// SourceFormatUpstream is the upstream MCP registry format
	SourceFormatUpstream = "upstream"
)

// Option defines the interface for configuration options
type Option func(*loaderConfig) error

// loaderConfig defines the configuration for loading a configuration
type loaderConfig struct {
	path string
}

// WithConfigPath loads configuration from a YAML file
func WithConfigPath(path string) Option {
	return func(cfg *loaderConfig) error {
		if path == "" {
			return fmt.Errorf("path is required")
		}

		// Resolve symlinks to prevent symlink attacks.
		// Note that this calls filepath.Clean internally.
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("failed to evaluate symlinks: %w", err)
		}

		// Validate the path to prevent path traversal attacks
		if !filepath.IsAbs(realPath) {
			if !filepath.IsLocal(realPath) {
				return fmt.Errorf("path is not local or contains invalid traversal: %s", path)
			}
		}

		cfg.path = realPath
		return nil
	}
}

// Config represents the root configuration structure
type Config struct {
	// RegistryName is the name/identifier for this registry instance
	// Defaults to "default" if not specified
	RegistryName string            `yaml:"registryName,omitempty"`
	Registries   []RegistryConfig  `yaml:"registries"`
	Database     *DatabaseConfig   `yaml:"database,omitempty"`
	Auth         *AuthConfig       `yaml:"auth,omitempty"`
	Telemetry    *telemetry.Config `yaml:"telemetry,omitempty"`

	// insecureAllowHTTP allows HTTP URLs for OAuth issuer URLs (development only)
	// Can be set via THV_REGISTRY_INSECURE_URL environment variable
	// Not loaded from YAML file - environment variable only
	insecureAllowHTTP bool

	// EnableAggregatedEndpoints enables aggregated endpoints that access all
	// configured registries.
	// Can be set via THV_REGISTRY_ENABLE_AGGREGATED_ENDPOINTS environment variable
	// Not loaded from YAML file - environment variable only
	EnableAggregatedEndpoints bool

	// WatchNamespace is the namespace to watch for MCP servers.
	// Can be set via THV_REGISTRY_WATCH_NAMESPACE environment variable
	// Not loaded from YAML file - environment variable only
	WatchNamespace string

	// LeaderElectionID is the unique identifier for the leader election lease.
	// Can be set via THV_REGISTRY_LEADER_ELECTION_ID environment variable
	// Not loaded from YAML file - environment variable only
	LeaderElectionID string
}

// RegistryConfig defines a single registry data source configuration
type RegistryConfig struct {
	// Name is the identifier for this registry
	Name string `yaml:"name"`

	// Format specifies the data format (toolhive or upstream)
	Format string `yaml:"format"`

	// Type-specific configurations (only one should be set)
	Git        *GitConfig        `yaml:"git,omitempty"`
	API        *APIConfig        `yaml:"api,omitempty"`
	File       *FileConfig       `yaml:"file,omitempty"`
	Managed    *ManagedConfig    `yaml:"managed,omitempty"`
	Kubernetes *KubernetesConfig `yaml:"kubernetes,omitempty"`

	// Per-registry sync policy
	// Note: Not applicable for non-synced registries (managed and kubernetes) - will be ignored if set
	SyncPolicy *SyncPolicyConfig `yaml:"syncPolicy,omitempty"`

	// Per-registry filtering rules
	// Note: Not applicable for non-synced registries (managed and kubernetes) - will be ignored if set
	Filter *FilterConfig `yaml:"filter,omitempty"`
}

// GitConfig defines Git source settings
type GitConfig struct {
	// Repository is the Git repository URL (HTTP/HTTPS/SSH)
	Repository string `yaml:"repository" json:"repository"`

	// Branch is the Git branch to use (mutually exclusive with Tag and Commit)
	Branch string `yaml:"branch,omitempty" json:"branch,omitempty"`

	// Tag is the Git tag to use (mutually exclusive with Branch and Commit)
	Tag string `yaml:"tag,omitempty" json:"tag,omitempty"`

	// Commit is the Git commit SHA to use (mutually exclusive with Branch and Tag)
	Commit string `yaml:"commit,omitempty" json:"commit,omitempty"`

	// Path is the path to the registry file within the repository
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// Auth contains optional authentication for private repositories
	Auth *GitAuthConfig `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// GitAuthConfig defines authentication settings for Git repositories
type GitAuthConfig struct {
	// Username is the Git username for HTTP Basic authentication
	Username string `yaml:"username,omitempty" json:"username,omitempty"`

	// PasswordFile is the path to a file containing the Git password/token
	// Must be an absolute path; whitespace is trimmed from the content
	PasswordFile string `yaml:"passwordFile,omitempty" json:"passwordFile,omitempty"`
}

// GetPassword reads the password from PasswordFile using the secure file reader.
// Returns empty string if the receiver is nil or PasswordFile is empty.
// Returns an error if the file cannot be read.
func (a *GitAuthConfig) GetPassword() (string, error) {
	if a == nil {
		return "", nil
	}
	password, err := readSecretFromFile(a.PasswordFile)
	if err != nil {
		return "", fmt.Errorf("failed to read git password: %w", err)
	}
	return password, nil
}

// Validate validates the GitAuthConfig.
// It checks that both username and passwordFile are specified together,
// that passwordFile is an absolute path, and that the file exists and is readable.
func (a *GitAuthConfig) Validate() error {
	if a == nil {
		return nil
	}

	hasUsername := a.Username != ""
	hasPasswordFile := a.PasswordFile != ""

	// Both must be set together, or neither
	if hasUsername != hasPasswordFile {
		return fmt.Errorf("git.auth.username and git.auth.passwordFile must both be specified")
	}

	if hasPasswordFile {
		// Must be absolute path
		if !filepath.IsAbs(a.PasswordFile) {
			return fmt.Errorf("git.auth.passwordFile must be an absolute path")
		}

		// Verify the file exists and is readable
		if _, err := os.Stat(a.PasswordFile); err != nil {
			return fmt.Errorf("git.auth.passwordFile is not accessible: %w", err)
		}
	}

	return nil
}

// APIConfig defines API source configuration for upstream MCP Registry APIs
type APIConfig struct {
	// Endpoint is the base API URL (without path)
	// The registry handler will append the appropriate paths for the MCP Registry API v0.1:
	//   - /v0.1/servers - List all servers
	//   - /v0.1/servers/{name}/versions - List server versions
	//   - /v0.1/servers/{name}/versions/{version} - Get specific version
	// Example: "http://my-registry-api.default.svc.cluster.local/registry"
	Endpoint string `yaml:"endpoint" json:"endpoint"`
}

// FileConfig defines file source configuration
// Supports local files, URL-hosted files, or inline data
type FileConfig struct {
	// Path is the path to the registry.json file on the local filesystem
	// Can be absolute or relative to the working directory
	// Mutually exclusive with URL and Data - exactly one must be specified
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// URL is the HTTP/HTTPS URL to fetch the registry file from
	// Mutually exclusive with Path and Data - exactly one must be specified
	// HTTPS is required unless the host is localhost or THV_REGISTRY_INSECURE_URL=true
	URL string `yaml:"url,omitempty" json:"url,omitempty"`

	// Data is the inline registry data as a JSON string
	// Mutually exclusive with Path and URL - exactly one must be specified
	// Useful for API-created registries where the data is provided directly
	Data string `yaml:"data,omitempty" json:"data,omitempty"`

	// Timeout is the timeout for HTTP requests when using URL
	// Defaults to 30s if not specified
	// Only applicable when URL is set
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// ManagedConfig defines configuration for managed registries
// Managed registries are directly manipulated via API and do not sync from external sources
// Note: Initially empty, may be used as a placeholder for future configuration options
type ManagedConfig struct {
	// Future fields can be added here as needed
}

// KubernetesConfig defines configuration for Kubernetes-based registries
// Kubernetes registries discover MCP servers from running Kubernetes resources
// No configuration is actually needed here.
type KubernetesConfig struct{}

// SyncPolicyConfig defines synchronization settings
type SyncPolicyConfig struct {
	Interval string `yaml:"interval" json:"interval"`
}

// FilterConfig defines filtering rules for registry entries
type FilterConfig struct {
	Names *NameFilterConfig `yaml:"names,omitempty" json:"names,omitempty"`
	Tags  *TagFilterConfig  `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// NameFilterConfig defines name-based filtering
type NameFilterConfig struct {
	Include []string `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`
}

// TagFilterConfig defines tag-based filtering
type TagFilterConfig struct {
	Include []string `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`
}

// AuthMode represents the authentication mode
type AuthMode string

const (
	// AuthModeAnonymous allows unauthenticated access
	AuthModeAnonymous AuthMode = "anonymous"

	// AuthModeOAuth requires OAuth/OIDC authentication
	AuthModeOAuth AuthMode = "oauth"

	// DefaultAuthMode is the auth mode used when not explicitly configured.
	// OAuth is the default for a secure-by-default posture.
	DefaultAuthMode AuthMode = AuthModeOAuth
)

// AuthConfig defines authentication configuration for the registry server
type AuthConfig struct {
	// Mode specifies the authentication mode (anonymous or oauth)
	// Defaults to "oauth" if not specified (security-by-default).
	// Use "anonymous" to explicitly disable authentication for development.
	Mode AuthMode `yaml:"mode,omitempty"`

	// PublicPaths defines additional paths that bypass authentication
	// These extend the default public paths (health, docs, swagger, well-known)
	// Example: ["/custom/public", "/metrics"]
	PublicPaths []string `yaml:"publicPaths,omitempty"`

	// OAuth contains OAuth/OIDC specific configuration
	// Required when Mode is "oauth"
	OAuth *OAuthConfig `yaml:"oauth,omitempty"`
}

// OAuthConfig defines OAuth/OIDC specific authentication settings
type OAuthConfig struct {
	// ResourceURL is the URL identifying this protected resource (RFC 9728)
	// Used in the /.well-known/oauth-protected-resource endpoint
	ResourceURL string `yaml:"resourceUrl,omitempty"`

	// Providers defines the OAuth/OIDC providers for authentication
	// Multiple providers can be configured (e.g., Kubernetes + external IDP)
	Providers []OAuthProviderConfig `yaml:"providers,omitempty"`

	// ScopesSupported defines the OAuth scopes supported by this resource (RFC 9728)
	// Defaults to ["mcp-registry:read", "mcp-registry:write"] if not specified
	ScopesSupported []string `yaml:"scopesSupported,omitempty"`

	// Realm is the protection space identifier for WWW-Authenticate header (RFC 7235)
	// Defaults to "mcp-registry" if not specified
	Realm string `yaml:"realm,omitempty"`
}

// DefaultScopes are the default OAuth scopes for the registry when not configured
var DefaultScopes = []string{"mcp-registry:read", "mcp-registry:write"}

// GetScopes returns the configured OAuth scopes or defaults if not specified
func (o *OAuthConfig) GetScopes() []string {
	if len(o.ScopesSupported) == 0 {
		return DefaultScopes
	}
	return o.ScopesSupported
}

// OAuthProviderConfig defines configuration for an OAuth/OIDC provider
type OAuthProviderConfig struct {
	// Name is a unique identifier for this provider (e.g., "kubernetes", "keycloak")
	Name string `yaml:"name"`

	// IssuerURL is the OIDC issuer URL (e.g., https://accounts.google.com)
	// The JWKS URL will be discovered automatically from .well-known/openid-configuration
	// unless JwksUrl is explicitly specified
	IssuerURL string `yaml:"issuerUrl"`

	// JwksUrl is the URL to fetch the JSON Web Key Set (JWKS) from
	// If specified, OIDC discovery is skipped and this URL is used directly
	// Example: https://kubernetes.default.svc/openid/v1/jwks
	JwksUrl string `yaml:"jwksUrl,omitempty"`

	// Audience is the expected audience claim in the token (REQUIRED)
	// Per RFC 6749 Section 4.1.3, tokens must be validated against expected audience
	// For Kubernetes, this is typically the API server URL
	Audience string `yaml:"audience"`

	// ClientID is the OAuth client ID for token introspection (optional)
	ClientID string `yaml:"clientId,omitempty"`

	// ClientSecretFile is the path to a file containing the client secret
	// The file should contain only the secret with optional trailing whitespace
	ClientSecretFile string `yaml:"clientSecretFile,omitempty"`

	// CACertPath is the path to a CA certificate bundle for verifying the provider's TLS certificate
	// Required for Kubernetes in-cluster authentication or self-signed certificates
	CACertPath string `yaml:"caCertPath,omitempty"`

	// AuthTokenFile is the path to a file containing a bearer token for authenticating to OIDC/JWKS endpoints
	// Useful when the OIDC discovery or JWKS endpoint requires authentication
	// Example: /var/run/secrets/kubernetes.io/serviceaccount/token
	AuthTokenFile string `yaml:"authTokenFile,omitempty"`

	// IntrospectionURL is the OAuth 2.0 Token Introspection endpoint (RFC 7662)
	// Used for validating opaque (non-JWT) tokens
	// If not specified, only JWT tokens can be validated via JWKS
	IntrospectionURL string `yaml:"introspectionUrl,omitempty"`

	// AllowPrivateIP allows JWKS/OIDC endpoints on private IP addresses
	// Required when the OAuth provider (e.g., Kubernetes API server) is running on a private network
	// Example: Set to true when using https://kubernetes.default.svc as the issuer URL
	AllowPrivateIP bool `yaml:"allowPrivateIP,omitempty"`
}

// GetClientSecret returns the client secret by reading from the file specified in ClientSecretFile.
// Returns empty string if ClientSecretFile is not configured.
// Returns an error if the file cannot be read.
func (p *OAuthProviderConfig) GetClientSecret() (string, error) {
	secret, err := readSecretFromFile(p.ClientSecretFile)
	if err != nil {
		return "", fmt.Errorf("failed to read client secret: %w", err)
	}
	return secret, nil
}

// validateProvider validates a single OAuth provider configuration.
// index is used for error message formatting to identify which provider failed validation.
// insecureAllowHTTP allows HTTP URLs for development (when THV_REGISTRY_INSECURE_URL is set).
func (p *OAuthProviderConfig) validateProvider(index int, insecureAllowHTTP bool) error {
	if p.Name == "" {
		return fmt.Errorf("auth.oauth.providers[%d].name is required", index)
	}
	if p.IssuerURL == "" {
		return fmt.Errorf("auth.oauth.providers[%d].issuerUrl is required", index)
	}

	// Validate IssuerURL format
	issuerURL, err := url.Parse(p.IssuerURL)
	if err != nil {
		return fmt.Errorf("auth.oauth.providers[%d].issuerUrl is invalid: %w", index, err)
	}

	if !issuerURL.IsAbs() || issuerURL.Host == "" {
		return fmt.Errorf("auth.oauth.providers[%d].issuerUrl must be an absolute URL with host", index)
	}

	// Enforce HTTPS unless THV_REGISTRY_INSECURE_URL=true or localhost
	if issuerURL.Scheme != "https" && !insecureAllowHTTP {
		host := issuerURL.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			const msg = "must use HTTPS (set THV_REGISTRY_INSECURE_URL=true to allow HTTP)"
			return fmt.Errorf("auth.oauth.providers[%d].issuerUrl %s", index, msg)
		}
	}

	if p.Audience == "" {
		return fmt.Errorf("auth.oauth.providers[%d].audience is required", index)
	}

	return nil
}

// Validate performs validation on the auth configuration.
// This method assumes Mode has already been resolved to a valid value
// (either explicitly set or defaulted by resolveAuthMode in serve.go).
// insecureAllowHTTP allows HTTP URLs for development (when THV_REGISTRY_INSECURE_URL is set).
func (a *AuthConfig) Validate(insecureAllowHTTP bool) error {
	switch a.Mode {
	case AuthModeAnonymous:
		// Anonymous mode doesn't require OAuth config
		return nil
	case AuthModeOAuth:
		// OAuth mode requires OAuth config
		if a.OAuth == nil {
			return fmt.Errorf("auth.oauth is required when mode is oauth")
		}
		if len(a.OAuth.Providers) == 0 {
			return fmt.Errorf("auth.oauth.providers is required when mode is oauth")
		}

		// Validate each provider
		for i, provider := range a.OAuth.Providers {
			if err := provider.validateProvider(i, insecureAllowHTTP); err != nil {
				return err
			}
		}

		return nil
	default:
		return fmt.Errorf("invalid auth.mode: %s (must be 'anonymous' or 'oauth')", a.Mode)
	}
}

// DynamicAuthAWSRDSIAM defines configuration for AWS RDS IAM dynamic authentication
type DynamicAuthAWSRDSIAM struct {
	// Region is the AWS region to use for authentication.
	// If "detect", the region will be automatically detected from the
	// instance metadata.
	Region string `yaml:"region,omitempty"`
}

// DynamicAuthConfig defines configuration for dynamic database authentication
type DynamicAuthConfig struct {
	// AWSRDSIAM is configuration for AWS RDS IAM dynamic authentication
	AWSRDSIAM *DynamicAuthAWSRDSIAM `yaml:"awsRdsIam,omitempty"`
}

// DatabaseConfig defines database connection settings
type DatabaseConfig struct {
	// Host is the database server hostname or IP address
	Host string `yaml:"host"`

	// Port is the database server port
	Port int `yaml:"port"`

	// User is the database username for normal operations (SELECT, INSERT, UPDATE, DELETE)
	User string `yaml:"user"`

	// MigrationUser is the database username for schema migrations (optional)
	// This user typically has elevated privileges (CREATE, ALTER, DROP)
	// If not specified, defaults to User for backward compatibility
	MigrationUser string `yaml:"migrationUser,omitempty"`

	// DynamicAuth is configuration for dynamic database authentication
	DynamicAuth *DynamicAuthConfig `yaml:"dynamicAuth,omitempty"`

	// Database is the database name
	Database string `yaml:"database"`

	// SSLMode is the SSL mode for the connection (disable, require, verify-ca, verify-full)
	SSLMode string `yaml:"sslMode,omitempty"`

	// MaxOpenConns is the maximum number of open connections to the database
	MaxOpenConns int32 `yaml:"maxOpenConns,omitempty"`

	// MaxIdleConns is the maximum number of idle connections in the pool
	MaxIdleConns int32 `yaml:"maxIdleConns,omitempty"`

	// ConnMaxLifetime is the maximum lifetime of a connection (e.g., "1h", "30m")
	ConnMaxLifetime string `yaml:"connMaxLifetime,omitempty"`

	// MaxMetaSize is the maximum allowed size in bytes for publisher-provided
	// metadata extensions (_meta). Must be greater than zero.
	// Defaults to 65536 (64KB) if not specified.
	// Can be overridden via THV_REGISTRY_DATABASE_MAXMETASIZE environment variable.
	MaxMetaSize *int `yaml:"maxMetaSize,omitempty"`
}

// GetPassword returns the database password for the application user.
// Returns empty string to let pgx use PGPASSFILE env var or ~/.pgpass.
// This is the recommended approach - use a pgpass file to provide credentials
// for both the application user and the migration user.
//
// The pgpass file format is: hostname:port:database:username:password
// Example:
//
//	localhost:5432:registry:db_app:app_password
//	localhost:5432:registry:db_migrator:migration_password
//
// See: https://www.postgresql.org/docs/current/libpq-pgpass.html
func (*DatabaseConfig) GetPassword() string {
	// Return empty string to allow pgx to use PGPASSFILE or default ~/.pgpass
	// The pgx driver will automatically check these if no password is provided
	return ""
}

// GetMigrationUser returns the database user for running migrations.
// If MigrationUser is not set, returns the regular User for backward compatibility.
func (d *DatabaseConfig) GetMigrationUser() string {
	if d.MigrationUser != "" {
		return d.MigrationUser
	}
	return d.User
}

// GetMigrationPassword returns the database password for the migration user.
// Returns empty string to let pgx use PGPASSFILE env var or ~/.pgpass.
// This allows different passwords for app user and migration user via pgpass.
func (*DatabaseConfig) GetMigrationPassword() string {
	// Return empty string to allow pgx to use PGPASSFILE or default ~/.pgpass
	return ""
}

// GetMaxMetaSize returns the configured maximum meta size in bytes.
// Returns DefaultMaxMetaSize (64KB) if not explicitly configured.
// The returned value is always positive — validation rejects non-positive values at startup.
func (d *DatabaseConfig) GetMaxMetaSize() int {
	if d == nil || d.MaxMetaSize == nil {
		return DefaultMaxMetaSize
	}
	return *d.MaxMetaSize
}

// GetConnectionString builds a PostgreSQL connection string for the application user.
// The connection string omits the password, allowing pgx to look up the password
// from PGPASSFILE or ~/.pgpass.
func (d *DatabaseConfig) GetConnectionString() string {
	sslMode := d.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}

	// No password in connection string - pgx will use PGPASSFILE or ~/.pgpass
	return fmt.Sprintf(
		"postgres://%s@%s:%d/%s?sslmode=%s",
		d.User,
		d.Host,
		d.Port,
		d.Database,
		sslMode,
	)
}

// GetMigrationConnectionString builds a PostgreSQL connection string for the migration user.
// Uses GetMigrationUser() which defaults to User if MigrationUser is not set.
// The connection string omits the password, allowing pgx to look up the password
// from PGPASSFILE or ~/.pgpass.
func (d *DatabaseConfig) GetMigrationConnectionString() string {
	sslMode := d.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}

	migrationUser := d.GetMigrationUser()

	// No password in connection string - pgx will use PGPASSFILE or ~/.pgpass
	return fmt.Sprintf(
		"postgres://%s@%s:%d/%s?sslmode=%s",
		migrationUser,
		d.Host,
		d.Port,
		d.Database,
		sslMode,
	)
}

// LoadConfig loads and parses configuration from a YAML file with environment variable support.
// Configuration values can be overridden using environment variables with the THV_REGISTRY_ prefix.
// Nested keys use underscores as separators (e.g., THV_REGISTRY_DATABASE_HOST for database.host).
func LoadConfig(opts ...Option) (*Config, error) {
	loaderCfg := &loaderConfig{}
	for _, opt := range opts {
		if err := opt(loaderCfg); err != nil {
			return nil, err
		}
	}

	// As of now, this is required because there's no other options to load
	// configuration. Once we add more options, we can remove this check.
	if loaderCfg.path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Create a new Viper instance (don't use global)
	v := viper.New()

	// Set config file path
	v.SetConfigFile(loaderCfg.path)

	// Configure environment variable support
	// All env vars use the THV_REGISTRY_ prefix (e.g., THV_REGISTRY_REGISTRYNAME, THV_REGISTRY_DATABASE_HOST)
	v.SetEnvPrefix(EnvPrefix)

	// Enable automatic environment variable binding
	// This allows any config value to be overridden via environment variables
	v.AutomaticEnv()

	// Replace dots with underscores for nested keys
	// e.g., database.host becomes THV_REGISTRY_DATABASE_HOST
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Unmarshal into struct
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Set insecureAllowHTTP from environment variable (THV_REGISTRY_INSECURE_URL)
	// This is not loaded from YAML - environment variable only for security
	config.insecureAllowHTTP = v.GetBool("insecure_url")

	// Set enableAggregatedEndpoints from environment variable (THV_REGISTRY_ENABLE_AGGREGATED_ENDPOINTS)
	// This is not loaded from YAML - environment variable only
	config.EnableAggregatedEndpoints = v.GetBool("enable_aggregated_endpoints")

	// Set watchNamespace from environment variable (THV_REGISTRY_WATCH_NAMESPACE)
	// This is not loaded from YAML - environment variable only
	config.WatchNamespace = v.GetString("watch_namespace")

	// Set leaderElectionID from environment variable (THV_REGISTRY_LEADER_ELECTION_ID)
	// This is not loaded from YAML - environment variable only
	config.LeaderElectionID = v.GetString("leader_election_id")

	// Validate the config
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// GetRegistryName returns the registry name, using "default" if not specified
func (c *Config) GetRegistryName() string {
	if c.RegistryName == "" {
		return "default"
	}
	return c.RegistryName
}

// Validate performs validation on the configuration
func (c *Config) validate() error {
	if c == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Validate at least one registry is configured
	if len(c.Registries) == 0 {
		return fmt.Errorf("at least one registry must be configured")
	}

	// Validate each registry configuration
	registryNames := make(map[string]bool)
	for i, reg := range c.Registries {
		// Validate registry name
		if reg.Name == "" {
			return fmt.Errorf("registry[%d]: name is required", i)
		}

		// Check for duplicate registry names
		if registryNames[reg.Name] {
			return fmt.Errorf("registry[%d]: duplicate registry name '%s'", i, reg.Name)
		}
		registryNames[reg.Name] = true

		// Validate registry-specific configuration
		if err := c.validateRegistryConfig(&reg, i); err != nil {
			return err
		}
	}

	// Validate storage configuration
	if err := c.validateStorageConfig(); err != nil {
		return err
	}

	// Validate auth configuration if present
	return c.validateAuth()
}

// validateRegistryConfig validates a single registry configuration
func (*Config) validateRegistryConfig(reg *RegistryConfig, index int) error {
	prefix := fmt.Sprintf("registry[%d] (%s)", index, reg.Name)

	// Validate exactly one source type is configured
	if err := validateSourceTypeCount(reg, prefix); err != nil {
		return err
	}

	// Non-synced registries (managed and kubernetes) don't require sync policy or filter
	// If syncPolicy or filter are set for these registries, they will be silently ignored
	if reg.IsNonSyncedRegistry() {
		return nil
	}

	// Synced registries require sync policy
	if err := validateSyncPolicy(reg.SyncPolicy, prefix); err != nil {
		return err
	}

	// Validate type-specific settings
	return validateSourceSpecificConfig(reg, prefix)
}

// validateSyncPolicy validates the sync policy configuration
func validateSyncPolicy(policy *SyncPolicyConfig, prefix string) error {
	if policy == nil || policy.Interval == "" {
		return fmt.Errorf("%s: syncPolicy.interval is required", prefix)
	}

	// Try to parse the interval to ensure it's valid
	if _, err := time.ParseDuration(policy.Interval); err != nil {
		return fmt.Errorf("%s: syncPolicy.interval must be a valid duration (e.g., '30m', '1h'): %w", prefix, err)
	}

	return nil
}

// validateSourceTypeCount ensures exactly one source type is configured
func validateSourceTypeCount(reg *RegistryConfig, prefix string) error {
	configCount := 0
	if reg.Git != nil {
		configCount++
	}
	if reg.API != nil {
		configCount++
	}
	if reg.File != nil {
		configCount++
	}
	if reg.Managed != nil {
		configCount++
	}
	if reg.Kubernetes != nil {
		configCount++
	}

	if configCount == 0 {
		return fmt.Errorf("%s: one of git, api, file, managed, or kubernetes configuration must be specified", prefix)
	}
	if configCount > 1 {
		return fmt.Errorf("%s: only one of git, api, file, managed, or kubernetes configuration may be specified", prefix)
	}

	return nil
}

// validateSourceSpecificConfig validates the configuration for each source type
func validateSourceSpecificConfig(reg *RegistryConfig, prefix string) error {
	if reg.Git != nil {
		return validateGitConfig(reg.Git, prefix)
	}

	if reg.API != nil {
		return validateAPIConfig(reg.API, reg.Format, prefix)
	}

	if reg.File != nil {
		return validateFileConfig(reg.File, prefix)
	}

	return nil
}

// validateGitConfig validates Git-specific configuration
func validateGitConfig(git *GitConfig, prefix string) error {
	if git.Repository == "" {
		return fmt.Errorf("%s: git.repository is required", prefix)
	}

	// Validate auth if present
	if git.Auth != nil {
		if err := git.Auth.Validate(); err != nil {
			return fmt.Errorf("%s: %w", prefix, err)
		}
	}
	return nil
}

// validateAPIConfig validates API-specific configuration
func validateAPIConfig(api *APIConfig, format string, prefix string) error {
	if api.Endpoint == "" {
		return fmt.Errorf("%s: api.endpoint is required", prefix)
	}
	if format != "" && format != SourceFormatUpstream {
		return fmt.Errorf("%s: format must be either empty or %s when using api, got %s", prefix, SourceFormatUpstream, format)
	}
	return nil
}

// validateFileConfig validates File-specific configuration
func validateFileConfig(file *FileConfig, prefix string) error {
	// Exactly one of Path or URL must be specified
	hasPath := file.Path != ""
	hasURL := file.URL != ""

	if !hasPath && !hasURL {
		return fmt.Errorf("%s: file.path or file.url is required", prefix)
	}
	if hasPath && hasURL {
		return fmt.Errorf("%s: file.path and file.url are mutually exclusive", prefix)
	}

	// Validate URL if specified
	if hasURL {
		if err := validateFileURL(file.URL, prefix); err != nil {
			return err
		}

		// Validate timeout if specified
		if file.Timeout != "" {
			if _, err := time.ParseDuration(file.Timeout); err != nil {
				return fmt.Errorf("%s: file.timeout must be a valid duration (e.g., '30s', '1m'): %w", prefix, err)
			}
		}
	}

	return nil
}

// validateFileURL validates the URL for file sources
func validateFileURL(rawURL string, prefix string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%s: file.url is invalid: %w", prefix, err)
	}

	if !parsedURL.IsAbs() || parsedURL.Host == "" {
		return fmt.Errorf("%s: file.url must be an absolute URL with host", prefix)
	}

	// Only allow HTTP and HTTPS schemes
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("%s: file.url must use http or https scheme", prefix)
	}

	return nil
}

// validateStorageConfig validates the storage configuration
func (c *Config) validateStorageConfig() error {
	if c.Database == nil {
		return fmt.Errorf("database configuration is required")
	}

	if c.Database.Host == "" {
		return fmt.Errorf("database.host is required")
	}
	if c.Database.Port == 0 {
		return fmt.Errorf("database.port is required")
	}
	if c.Database.User == "" {
		return fmt.Errorf("database.user is required")
	}
	if c.Database.Database == "" {
		return fmt.Errorf("database.database is required")
	}
	if c.Database.MaxMetaSize != nil && *c.Database.MaxMetaSize <= 0 {
		return fmt.Errorf("database.maxMetaSize must be greater than zero")
	}

	return nil
}

// readSecretFromFile reads a secret from a file with proper path validation.
// It resolves symlinks, requires absolute paths, and trims whitespace from content.
func readSecretFromFile(filePath string) (string, error) {
	if filePath == "" {
		return "", nil
	}

	// Resolve symlinks to get real path
	realPath, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	// Require absolute paths for security
	if !filepath.IsAbs(realPath) {
		return "", fmt.Errorf("path must be absolute: %s", filePath)
	}

	data, err := os.ReadFile(realPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

// GetType returns the inferred type of the registry config based on which field is present
func (r *RegistryConfig) GetType() SourceType {
	if r.Git != nil {
		return SourceTypeGit
	}
	if r.API != nil {
		return SourceTypeAPI
	}
	if r.File != nil {
		return SourceTypeFile
	}
	if r.Managed != nil {
		return SourceTypeManaged
	}
	if r.Kubernetes != nil {
		return SourceTypeKubernetes
	}
	return ""
}

// IsNonSyncedRegistry returns true if the registry type doesn't sync from external sources.
// This includes managed registries (manipulated via API) and kubernetes registries (query live deployments).
// Non-synced registries do not require sync policy configuration and skip the sync loop.
func (r *RegistryConfig) IsNonSyncedRegistry() bool {
	regType := r.GetType()
	return regType == SourceTypeManaged || regType == SourceTypeKubernetes
}

// validateAuth validates the auth configuration if present
func (c *Config) validateAuth() error {
	if c.Auth == nil {
		return errors.New("auth configuration is required")
	}
	if err := c.Auth.Validate(c.insecureAllowHTTP); err != nil {
		return fmt.Errorf("invalid auth configuration: %w", err)
	}

	return nil
}
