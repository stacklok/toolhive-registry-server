package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestValidateRegistryConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     *RegistryCreateRequest
		wantErr bool
		errMsg  string
	}{
		// Nil request
		{
			name:    "nil_request_returns_error",
			req:     nil,
			wantErr: true,
			errMsg:  "config is required",
		},

		// No source type specified
		{
			name:    "no_source_type_returns_error",
			req:     &RegistryCreateRequest{},
			wantErr: true,
			errMsg:  "one of git, api, file, managed, or kubernetes must be specified",
		},

		// Multiple source types specified
		{
			name: "multiple_source_types_git_and_api_returns_error",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			wantErr: true,
			errMsg:  "only one source type may be specified",
		},
		{
			name: "multiple_source_types_git_and_file_returns_error",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
			},
			wantErr: true,
			errMsg:  "only one source type may be specified",
		},
		{
			name: "multiple_source_types_file_and_managed_returns_error",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
				Managed: &config.ManagedConfig{},
			},
			wantErr: true,
			errMsg:  "only one source type may be specified",
		},
		{
			name: "multiple_source_types_all_five_returns_error",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
				Managed:    &config.ManagedConfig{},
				Kubernetes: &config.KubernetesConfig{},
			},
			wantErr: true,
			errMsg:  "only one source type may be specified",
		},

		// Valid Git source configurations
		{
			name: "valid_git_source_with_sync_policy",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: false,
		},
		{
			name: "valid_git_source_with_branch",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
					Branch:     "main",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			wantErr: false,
		},
		{
			name: "valid_git_source_with_tag",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
					Tag:        "v1.0.0",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			wantErr: false,
		},
		{
			name: "valid_git_source_with_commit",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
					Commit:     "abc123def456",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "1h",
				},
			},
			wantErr: false,
		},

		// Valid API source configurations
		{
			name: "valid_api_source_with_sync_policy",
			req: &RegistryCreateRequest{
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "15m",
				},
			},
			wantErr: false,
		},

		// Valid File source configurations
		{
			name: "valid_file_source_with_path",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: false,
		},
		{
			name: "valid_file_source_with_url",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					URL: "https://example.com/registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: false,
		},
		{
			name: "valid_file_source_with_inline_data_no_sync_policy_required",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Data: `{"version":"1.0.0","last_updated":"2025-01-15T10:30:00Z","servers":{"test-server":{"name":"test-server","description":"A test server","image":"test/image:latest","tier":"Community","status":"Active","transport":"stdio","tools":["test_tool"]}}}`,
				},
			},
			wantErr: false,
		},

		// Valid Managed source configurations
		{
			name: "valid_managed_source_no_sync_policy_required",
			req: &RegistryCreateRequest{
				Managed: &config.ManagedConfig{},
			},
			wantErr: false,
		},

		// Valid Kubernetes source configurations
		{
			name: "valid_kubernetes_source_no_sync_policy_required",
			req: &RegistryCreateRequest{
				Kubernetes: &config.KubernetesConfig{},
			},
			wantErr: false,
		},

		// Sync policy validation for synced types
		{
			name: "git_source_missing_sync_policy_returns_error",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
			},
			wantErr: true,
			errMsg:  "syncPolicy.interval is required",
		},
		{
			name: "git_source_empty_sync_interval_returns_error",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "",
				},
			},
			wantErr: true,
			errMsg:  "syncPolicy.interval is required",
		},
		{
			name: "git_source_invalid_sync_interval_returns_error",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid sync interval",
		},
		{
			name: "api_source_missing_sync_policy_returns_error",
			req: &RegistryCreateRequest{
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			wantErr: true,
			errMsg:  "syncPolicy.interval is required",
		},
		{
			name: "file_source_with_path_missing_sync_policy_returns_error",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
			},
			wantErr: true,
			errMsg:  "syncPolicy.interval is required",
		},
		{
			name: "file_source_with_url_missing_sync_policy_returns_error",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					URL: "https://example.com/registry.json",
				},
			},
			wantErr: true,
			errMsg:  "syncPolicy.interval is required",
		},

		// Format validation
		{
			name: "valid_toolhive_format",
			req: &RegistryCreateRequest{
				Format: "toolhive",
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: false,
		},
		{
			name: "valid_upstream_format",
			req: &RegistryCreateRequest{
				Format: "upstream",
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: false,
		},
		{
			name: "valid_empty_format",
			req: &RegistryCreateRequest{
				Format: "",
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid_format_returns_error",
			req: &RegistryCreateRequest{
				Format: "invalid-format",
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: true,
			errMsg:  "format must be 'toolhive' or 'upstream'",
		},

		// Managed and Kubernetes ignore sync policy if provided
		{
			name: "managed_with_sync_policy_is_valid_but_ignored",
			req: &RegistryCreateRequest{
				Managed: &config.ManagedConfig{},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: false,
		},
		{
			name: "kubernetes_with_sync_policy_is_valid_but_ignored",
			req: &RegistryCreateRequest{
				Kubernetes: &config.KubernetesConfig{},
				SyncPolicy: &config.SyncPolicyConfig{
					Interval: "30m",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRegistryConfig(tt.req)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateGitConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.GitConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil_config_returns_error",
			cfg:     nil,
			wantErr: true,
			errMsg:  "git config is required",
		},
		{
			name:    "empty_repository_returns_error",
			cfg:     &config.GitConfig{},
			wantErr: true,
			errMsg:  "git.repository is required",
		},
		{
			name: "valid_repository_only",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
			},
			wantErr: false,
		},
		{
			name: "valid_repository_with_branch",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Branch:     "main",
			},
			wantErr: false,
		},
		{
			name: "valid_repository_with_tag",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Tag:        "v1.0.0",
			},
			wantErr: false,
		},
		{
			name: "valid_repository_with_commit",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Commit:     "abc123def456",
			},
			wantErr: false,
		},
		{
			name: "valid_repository_with_path",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Path:       "registry/servers.json",
			},
			wantErr: false,
		},
		{
			name: "branch_and_tag_mutually_exclusive_returns_error",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Branch:     "main",
				Tag:        "v1.0.0",
			},
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "branch_and_commit_mutually_exclusive_returns_error",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Branch:     "main",
				Commit:     "abc123def456",
			},
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "tag_and_commit_mutually_exclusive_returns_error",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Tag:        "v1.0.0",
				Commit:     "abc123def456",
			},
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "all_three_refs_mutually_exclusive_returns_error",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Branch:     "main",
				Tag:        "v1.0.0",
				Commit:     "abc123def456",
			},
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "auth_username_only_returns_error",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Auth: &config.GitAuthConfig{
					Username: "user",
				},
			},
			wantErr: true,
			errMsg:  "git.auth.username and git.auth.passwordFile must both be specified",
		},
		{
			name: "auth_password_file_only_returns_error",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Auth: &config.GitAuthConfig{
					PasswordFile: "/secrets/password",
				},
			},
			wantErr: true,
			errMsg:  "git.auth.username and git.auth.passwordFile must both be specified",
		},
		{
			name: "auth_relative_path_returns_error",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Auth: &config.GitAuthConfig{
					Username:     "user",
					PasswordFile: "relative/path",
				},
			},
			wantErr: true,
			errMsg:  "git.auth.passwordFile must be an absolute path",
		},
		{
			name: "auth_nonexistent_file_returns_error",
			cfg: &config.GitConfig{
				Repository: "https://github.com/example/repo.git",
				Auth: &config.GitAuthConfig{
					Username:     "user",
					PasswordFile: "/nonexistent/path/password.txt",
				},
			},
			wantErr: true,
			errMsg:  "git.auth.passwordFile is not accessible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateGitConfig(tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateAPIConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *config.APIConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil_config_returns_error",
			cfg:     nil,
			wantErr: true,
			errMsg:  "api config is required",
		},
		{
			name:    "empty_endpoint_returns_error",
			cfg:     &config.APIConfig{},
			wantErr: true,
			errMsg:  "api.endpoint is required",
		},
		{
			name: "valid_endpoint",
			cfg: &config.APIConfig{
				Endpoint: "https://api.example.com",
			},
			wantErr: false,
		},
		{
			name: "valid_endpoint_with_path",
			cfg: &config.APIConfig{
				Endpoint: "https://api.example.com/registry",
			},
			wantErr: false,
		},
		{
			name: "valid_http_endpoint",
			cfg: &config.APIConfig{
				Endpoint: "http://localhost:8080",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateAPIConfig(tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateFileConfig(t *testing.T) {
	t.Parallel()

	// Valid JSON for inline data tests - using a real entry from /examples/toolhive-registry.json
	validToolhiveJSON := `{"$schema":"https://raw.githubusercontent.com/stacklok/toolhive/main/pkg/registry/data/toolhive-legacy-registry.schema.json","version":"1.0.0","last_updated":"2025-11-26T00:27:46Z","servers":{"fetch":{"description":"Allows you to fetch content from the web","tier":"Community","status":"Active","transport":"streamable-http","tools":["fetch"],"metadata":{"stars":20,"pulls":12390,"last_updated":"2025-11-13T02:33:35Z"},"repository_url":"https://github.com/stackloklabs/gofetch","tags":["content","html","markdown","fetch","fetching","get","wget","json","curl","modelcontextprotocol"],"image":"ghcr.io/stackloklabs/gofetch/server:1.0.1","target_port":8080,"permissions":{"network":{"outbound":{"insecure_allow_all":true,"allow_port":[443]}}},"provenance":{"sigstore_url":"tuf-repo-cdn.sigstore.dev","repository_uri":"https://github.com/StacklokLabs/gofetch","signer_identity":"/.github/workflows/release.yml","runner_environment":"github-hosted","cert_issuer":"https://token.actions.githubusercontent.com"}}}}`

	tests := []struct {
		name    string
		cfg     *config.FileConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil_config_returns_error",
			cfg:     nil,
			wantErr: true,
			errMsg:  "file config is required",
		},
		{
			name:    "empty_config_returns_error",
			cfg:     &config.FileConfig{},
			wantErr: true,
			errMsg:  "file.path, file.url, or file.data is required",
		},
		{
			name: "valid_path",
			cfg: &config.FileConfig{
				Path: "/data/registry.json",
			},
			wantErr: false,
		},
		{
			name: "valid_relative_path",
			cfg: &config.FileConfig{
				Path: "./data/registry.json",
			},
			wantErr: false,
		},
		{
			name: "valid_url",
			cfg: &config.FileConfig{
				URL: "https://example.com/registry.json",
			},
			wantErr: false,
		},
		{
			name: "valid_http_url",
			cfg: &config.FileConfig{
				URL: "http://localhost:8080/registry.json",
			},
			wantErr: false,
		},
		{
			name: "valid_data",
			cfg: &config.FileConfig{
				Data: validToolhiveJSON,
			},
			wantErr: false,
		},
		{
			name: "path_and_url_mutually_exclusive_returns_error",
			cfg: &config.FileConfig{
				Path: "/data/registry.json",
				URL:  "https://example.com/registry.json",
			},
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "path_and_data_mutually_exclusive_returns_error",
			cfg: &config.FileConfig{
				Path: "/data/registry.json",
				Data: validToolhiveJSON,
			},
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "url_and_data_mutually_exclusive_returns_error",
			cfg: &config.FileConfig{
				URL:  "https://example.com/registry.json",
				Data: validToolhiveJSON,
			},
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "all_three_mutually_exclusive_returns_error",
			cfg: &config.FileConfig{
				Path: "/data/registry.json",
				URL:  "https://example.com/registry.json",
				Data: validToolhiveJSON,
			},
			wantErr: true,
			errMsg:  "mutually exclusive",
		},
		{
			name: "valid_url_with_timeout",
			cfg: &config.FileConfig{
				URL:     "https://example.com/registry.json",
				Timeout: "30s",
			},
			wantErr: false,
		},
		{
			name: "valid_url_with_long_timeout",
			cfg: &config.FileConfig{
				URL:     "https://example.com/registry.json",
				Timeout: "5m",
			},
			wantErr: false,
		},
		{
			name: "timeout_only_valid_with_url_returns_error",
			cfg: &config.FileConfig{
				Path:    "/data/registry.json",
				Timeout: "30s",
			},
			wantErr: true,
			errMsg:  "file.timeout is only applicable when file.url is specified",
		},
		{
			name: "timeout_with_data_returns_error",
			cfg: &config.FileConfig{
				Data:    validToolhiveJSON,
				Timeout: "30s",
			},
			wantErr: true,
			errMsg:  "file.timeout is only applicable when file.url is specified",
		},
		{
			name: "invalid_timeout_returns_error",
			cfg: &config.FileConfig{
				URL:     "https://example.com/registry.json",
				Timeout: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid file.timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateFileConfig(tt.cfg)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateInlineDataBasic(t *testing.T) {
	t.Parallel()

	// Valid upstream format JSON that matches the schema
	validUpstreamJSON := `{
		"$schema": "https://raw.githubusercontent.com/stacklok/toolhive/main/pkg/registry/data/upstream-registry.schema.json",
		"version": "1.0.0",
		"meta": {
			"last_updated": "2025-01-15T10:30:00Z"
		},
		"data": {
			"servers": [{
				"$schema": "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
				"name": "io.github.test/test-server",
				"description": "A test server for validation",
				"title": "test-server",
				"version": "1.0.0",
				"packages": [{
					"registryType": "oci",
					"identifier": "test/image:latest",
					"transport": {
						"type": "stdio"
					}
				}],
				"_meta": {
					"io.modelcontextprotocol.registry/publisher-provided": {
						"io.github.test": {
							"test/image:latest": {
								"tier": "Community",
								"status": "Active",
								"tools": ["test_tool"]
							}
						}
					}
				}
			}]
		}
	}`

	// Valid toolhive format JSON that matches the schema
	validToolhiveJSON := `{
		"version": "1.0.0",
		"last_updated": "2025-01-15T10:30:00Z",
		"servers": {
			"test-server": {
				"name": "test-server",
				"description": "A test server for validation",
				"image": "test/image:latest",
				"tier": "Community",
				"status": "Active",
				"transport": "stdio",
				"tools": ["test_tool"]
			}
		}
	}`

	tests := []struct {
		name    string
		data    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty_data_returns_error",
			data:    "",
			wantErr: true,
			errMsg:  "data cannot be empty",
		},
		{
			name:    "whitespace_only_data_returns_error",
			data:    "   ",
			wantErr: true,
			// The validator will report parse error or schema validation error
		},
		{
			name:    "invalid_json_returns_error",
			data:    "not json at all",
			wantErr: true,
		},
		{
			name:    "incomplete_json_returns_error",
			data:    `{"version": "1.0"`,
			wantErr: true,
		},
		{
			name:    "valid_upstream_format_json",
			data:    validUpstreamJSON,
			wantErr: false,
		},
		{
			name:    "valid_toolhive_format_json",
			data:    validToolhiveJSON,
			wantErr: false,
		},
		{
			name:    "json_array_is_invalid_returns_error",
			data:    `[{"name": "test"}]`,
			wantErr: true,
		},
		{
			name:    "json_primitive_string_is_invalid_returns_error",
			data:    `"just a string"`,
			wantErr: true,
		},
		{
			name:    "json_primitive_number_is_invalid_returns_error",
			data:    `42`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateInlineDataBasic(tt.data)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateInlineDataWithFormat(t *testing.T) {
	t.Parallel()

	// Valid upstream format JSON that matches the schema
	validUpstreamJSON := `{
		"$schema": "https://raw.githubusercontent.com/stacklok/toolhive/main/pkg/registry/data/upstream-registry.schema.json",
		"version": "1.0.0",
		"meta": {
			"last_updated": "2025-01-15T10:30:00Z"
		},
		"data": {
			"servers": [{
				"$schema": "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
				"name": "io.github.test/test-server",
				"description": "A test server for validation",
				"title": "test-server",
				"version": "1.0.0",
				"packages": [{
					"registryType": "oci",
					"identifier": "test/image:latest",
					"transport": {
						"type": "stdio"
					}
				}],
				"_meta": {
					"io.modelcontextprotocol.registry/publisher-provided": {
						"io.github.test": {
							"test/image:latest": {
								"tier": "Community",
								"status": "Active",
								"tools": ["test_tool"]
							}
						}
					}
				}
			}]
		}
	}`

	// Valid toolhive format JSON that matches the schema
	validToolhiveJSON := `{
		"version": "1.0.0",
		"last_updated": "2025-01-15T10:30:00Z",
		"servers": {
			"test-server": {
				"name": "test-server",
				"description": "A test server for validation",
				"image": "test/image:latest",
				"tier": "Community",
				"status": "Active",
				"transport": "stdio",
				"tools": ["test_tool"]
			}
		}
	}`

	tests := []struct {
		name    string
		data    string
		format  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty_data_returns_error",
			data:    "",
			format:  "upstream",
			wantErr: true,
			errMsg:  "data cannot be empty",
		},
		{
			name:    "empty_data_with_empty_format_returns_error",
			data:    "",
			format:  "",
			wantErr: true,
			errMsg:  "data cannot be empty",
		},
		{
			name:    "valid_upstream_format",
			data:    validUpstreamJSON,
			format:  "upstream",
			wantErr: false,
		},
		{
			name:    "valid_toolhive_format",
			data:    validToolhiveJSON,
			format:  "toolhive",
			wantErr: false,
		},
		{
			name:    "upstream_data_with_empty_format_auto_detects",
			data:    validUpstreamJSON,
			format:  "",
			wantErr: false,
		},
		{
			name:    "toolhive_data_with_empty_format_auto_detects",
			data:    validToolhiveJSON,
			format:  "",
			wantErr: false,
		},
		{
			name:    "toolhive_data_with_upstream_format_returns_error",
			data:    validToolhiveJSON,
			format:  "upstream",
			wantErr: true,
		},
		{
			name:    "upstream_data_with_toolhive_format_returns_error",
			data:    validUpstreamJSON,
			format:  "toolhive",
			wantErr: true,
		},
		{
			name:    "unsupported_format_returns_error",
			data:    validUpstreamJSON,
			format:  "unsupported",
			wantErr: true,
			errMsg:  "unsupported format",
		},
		{
			name:    "invalid_json_with_upstream_format_returns_error",
			data:    "not json",
			format:  "upstream",
			wantErr: true,
		},
		{
			name:    "invalid_json_with_toolhive_format_returns_error",
			data:    "not json",
			format:  "toolhive",
			wantErr: true,
		},
		{
			name: "upstream_format_missing_servers_returns_error",
			data: `{
				"version": "1.0.0",
				"meta": {"last_updated": "2025-01-01T00:00:00Z"},
				"data": {
					"servers": []
				}
			}`,
			format:  "upstream",
			wantErr: true,
			errMsg:  "at least one server",
		},
		{
			name: "upstream_format_server_missing_name_returns_error",
			data: `{
				"version": "1.0.0",
				"meta": {"last_updated": "2025-01-01T00:00:00Z"},
				"data": {
					"servers": [{
						"description": "A test server without name",
						"version": "1.0.0"
					}]
				}
			}`,
			format:  "upstream",
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "upstream_format_server_missing_description_returns_error",
			data: `{
				"version": "1.0.0",
				"meta": {"last_updated": "2025-01-01T00:00:00Z"},
				"data": {
					"servers": [{
						"name": "io.github.test/test-server",
						"version": "1.0.0"
					}]
				}
			}`,
			format:  "upstream",
			wantErr: true,
			errMsg:  "description is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateInlineDataWithFormat(tt.data, tt.format)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSourceSpecific(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     *RegistryCreateRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "git_source_validation",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
			},
			wantErr: false,
		},
		{
			name: "git_source_missing_repository_returns_error",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{},
			},
			wantErr: true,
			errMsg:  "git.repository is required",
		},
		{
			name: "api_source_validation",
			req: &RegistryCreateRequest{
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "api_source_missing_endpoint_returns_error",
			req: &RegistryCreateRequest{
				API: &config.APIConfig{},
			},
			wantErr: true,
			errMsg:  "api.endpoint is required",
		},
		{
			name: "file_source_validation",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
			},
			wantErr: false,
		},
		{
			name: "file_source_empty_returns_error",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{},
			},
			wantErr: true,
			errMsg:  "file.path, file.url, or file.data is required",
		},
		{
			name: "managed_source_no_validation_required",
			req: &RegistryCreateRequest{
				Managed: &config.ManagedConfig{},
			},
			wantErr: false,
		},
		{
			name: "kubernetes_source_no_validation_required",
			req: &RegistryCreateRequest{
				Kubernetes: &config.KubernetesConfig{},
			},
			wantErr: false,
		},
		{
			name: "unknown_source_type_returns_error",
			req:  &RegistryCreateRequest{
				// All source types are nil
			},
			wantErr: true,
			errMsg:  "unknown source type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateSourceSpecific(tt.req)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRegistryCreateRequest_GetSourceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      *RegistryCreateRequest
		expected config.SourceType
	}{
		{
			name: "git_source_type",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
			},
			expected: config.SourceTypeGit,
		},
		{
			name: "api_source_type",
			req: &RegistryCreateRequest{
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			expected: config.SourceTypeAPI,
		},
		{
			name: "file_source_type",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
			},
			expected: config.SourceTypeFile,
		},
		{
			name: "managed_source_type",
			req: &RegistryCreateRequest{
				Managed: &config.ManagedConfig{},
			},
			expected: config.SourceTypeManaged,
		},
		{
			name: "kubernetes_source_type",
			req: &RegistryCreateRequest{
				Kubernetes: &config.KubernetesConfig{},
			},
			expected: config.SourceTypeKubernetes,
		},
		{
			name:     "no_source_type_returns_empty",
			req:      &RegistryCreateRequest{},
			expected: config.SourceType(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.req.GetSourceType()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRegistryCreateRequest_CountSourceTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      *RegistryCreateRequest
		expected int
	}{
		{
			name:     "no_source_types",
			req:      &RegistryCreateRequest{},
			expected: 0,
		},
		{
			name: "one_source_type_git",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
			},
			expected: 1,
		},
		{
			name: "one_source_type_api",
			req: &RegistryCreateRequest{
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			expected: 1,
		},
		{
			name: "one_source_type_file",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
			},
			expected: 1,
		},
		{
			name: "one_source_type_managed",
			req: &RegistryCreateRequest{
				Managed: &config.ManagedConfig{},
			},
			expected: 1,
		},
		{
			name: "one_source_type_kubernetes",
			req: &RegistryCreateRequest{
				Kubernetes: &config.KubernetesConfig{},
			},
			expected: 1,
		},
		{
			name: "two_source_types",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			expected: 2,
		},
		{
			name: "three_source_types",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
			},
			expected: 3,
		},
		{
			name: "all_five_source_types",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
				Managed:    &config.ManagedConfig{},
				Kubernetes: &config.KubernetesConfig{},
			},
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.req.CountSourceTypes()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRegistryCreateRequest_IsNonSyncedType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      *RegistryCreateRequest
		expected bool
	}{
		{
			name: "git_is_synced",
			req: &RegistryCreateRequest{
				Git: &config.GitConfig{
					Repository: "https://github.com/example/repo.git",
				},
			},
			expected: false,
		},
		{
			name: "api_is_synced",
			req: &RegistryCreateRequest{
				API: &config.APIConfig{
					Endpoint: "https://api.example.com",
				},
			},
			expected: false,
		},
		{
			name: "file_with_path_is_synced",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
			},
			expected: false,
		},
		{
			name: "file_with_url_is_synced",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					URL: "https://example.com/registry.json",
				},
			},
			expected: false,
		},
		{
			name: "file_with_inline_data_is_non_synced",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Data: `{"version":"1.0","meta":{},"data":{"servers":[]}}`,
				},
			},
			expected: true,
		},
		{
			name: "managed_is_non_synced",
			req: &RegistryCreateRequest{
				Managed: &config.ManagedConfig{},
			},
			expected: true,
		},
		{
			name: "kubernetes_is_non_synced",
			req: &RegistryCreateRequest{
				Kubernetes: &config.KubernetesConfig{},
			},
			expected: true,
		},
		{
			name:     "no_source_type_is_synced",
			req:      &RegistryCreateRequest{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.req.IsNonSyncedType()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRegistryCreateRequest_IsInlineData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		req      *RegistryCreateRequest
		expected bool
	}{
		{
			name:     "no_file_config_returns_false",
			req:      &RegistryCreateRequest{},
			expected: false,
		},
		{
			name: "file_config_with_path_returns_false",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Path: "/data/registry.json",
				},
			},
			expected: false,
		},
		{
			name: "file_config_with_url_returns_false",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					URL: "https://example.com/registry.json",
				},
			},
			expected: false,
		},
		{
			name: "file_config_with_data_returns_true",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Data: `{"version":"1.0"}`,
				},
			},
			expected: true,
		},
		{
			name: "file_config_with_empty_data_returns_false",
			req: &RegistryCreateRequest{
				File: &config.FileConfig{
					Data: "",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.req.IsInlineData()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRegistryCreateRequest_GetSourceConfig(t *testing.T) {
	t.Parallel()

	gitConfig := &config.GitConfig{Repository: "https://github.com/example/repo.git"}
	apiConfig := &config.APIConfig{Endpoint: "https://api.example.com"}
	fileConfig := &config.FileConfig{Path: "/data/registry.json"}
	managedConfig := &config.ManagedConfig{}
	kubernetesConfig := &config.KubernetesConfig{}

	tests := []struct {
		name     string
		req      *RegistryCreateRequest
		expected interface{}
	}{
		{
			name: "returns_git_config",
			req: &RegistryCreateRequest{
				Git: gitConfig,
			},
			expected: gitConfig,
		},
		{
			name: "returns_api_config",
			req: &RegistryCreateRequest{
				API: apiConfig,
			},
			expected: apiConfig,
		},
		{
			name: "returns_file_config",
			req: &RegistryCreateRequest{
				File: fileConfig,
			},
			expected: fileConfig,
		},
		{
			name: "returns_managed_config",
			req: &RegistryCreateRequest{
				Managed: managedConfig,
			},
			expected: managedConfig,
		},
		{
			name: "returns_kubernetes_config",
			req: &RegistryCreateRequest{
				Kubernetes: kubernetesConfig,
			},
			expected: kubernetesConfig,
		},
		{
			name:     "returns_nil_when_no_source",
			req:      &RegistryCreateRequest{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.req.GetSourceConfig()
			assert.Equal(t, tt.expected, result)
		})
	}
}
