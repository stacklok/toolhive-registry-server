package helpers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/onsi/gomega"

	registryapp "github.com/stacklok/toolhive-registry-server/pkg/app"
	"github.com/stacklok/toolhive-registry-server/pkg/config"
)

// ServerTestHelper manages the registry API server lifecycle for testing
type ServerTestHelper struct {
	ctx        context.Context
	configPath string
	baseURL    string
	httpClient *http.Client
	app        *registryapp.RegistryApp
	dataDir    string
	port       int
}

// NewServerTestHelper creates a new server test helper
func NewServerTestHelper(ctx context.Context, configPath string, port int, dataDir string) *ServerTestHelper {
	return &ServerTestHelper{
		ctx:        ctx,
		configPath: configPath,
		baseURL:    fmt.Sprintf("http://localhost:%d", port),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		dataDir: dataDir,
		port:    port,
	}
}

// StartServer starts the registry API server programmatically
func (s *ServerTestHelper) StartServer() error {
	// Load configuration
	cfg, err := config.NewConfigLoader().LoadConfig(s.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Build the application
	app, err := registryapp.NewRegistryAppBuilder(cfg).
		WithAddress(fmt.Sprintf(":%d", s.port)).
		WithDataDirectory(s.dataDir).
		Build(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to build app: %w", err)
	}

	s.app = app

	// Start the server in a goroutine (non-blocking)
	go func() {
		if err := app.Start(); err != nil {
			// Log error but don't fail the test here
			// The test will fail when it tries to connect
			fmt.Fprintf(os.Stderr, "Server start failed: %v\n", err)
		}
	}()

	return nil
}

// StopServer gracefully stops the registry API server
func (s *ServerTestHelper) StopServer() error {
	if s.app != nil {
		return s.app.Stop(5 * time.Second)
	}
	return nil
}

// WaitForServerReady waits for the server to be ready to accept requests
func (s *ServerTestHelper) WaitForServerReady(timeout time.Duration) {
	gomega.Eventually(func() error {
		resp, err := s.httpClient.Get(s.baseURL + "/health")
		if err != nil {
			return err
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned status %d", resp.StatusCode)
		}
		return nil
	}, timeout, 500*time.Millisecond).Should(gomega.Succeed(), "Server should be ready")
}

// GetServers makes a GET request to /v0/servers
func (s *ServerTestHelper) GetServers() (*http.Response, error) {
	return s.httpClient.Get(s.baseURL + "/v0/servers")
}

// GetServer makes a GET request to /v0/servers/{name}
func (s *ServerTestHelper) GetServer(name string) (*http.Response, error) {
	return s.httpClient.Get(fmt.Sprintf("%s/v0/servers/%s", s.baseURL, name))
}

// GetDeployed makes a GET request to /v0/servers/deployed
func (s *ServerTestHelper) GetDeployed() (*http.Response, error) {
	return s.httpClient.Get(s.baseURL + "/v0/servers/deployed")
}

// GetDeployedServer makes a GET request to /v0/servers/deployed/{name}
func (s *ServerTestHelper) GetDeployedServer(name string) (*http.Response, error) {
	return s.httpClient.Get(fmt.Sprintf("%s/v0/servers/deployed/%s", s.baseURL, name))
}

// GetHealth makes a GET request to /health
func (s *ServerTestHelper) GetHealth() (*http.Response, error) {
	return s.httpClient.Get(s.baseURL + "/health")
}

// GetBaseURL returns the base URL of the server
func (s *ServerTestHelper) GetBaseURL() string {
	return s.baseURL
}

// FilterOptions holds optional configuration for WriteConfigYAMLWithOptions
type FilterOptions struct {
	NameInclude []string
	NameExclude []string
	TagInclude  []string
	TagExclude  []string
}

// WriteConfigYAML writes a YAML configuration file for testing
func WriteConfigYAML(dir, registryName, sourceType string, sourceConfig map[string]string) string {
	return WriteConfigYAMLWithOptions(dir, registryName, sourceType, sourceConfig, nil)
}

// WriteConfigYAMLWithOptions writes a YAML configuration file with optional filter configuration
func WriteConfigYAMLWithOptions(dir, registryName, sourceType string, sourceConfig map[string]string, filterOpts *FilterOptions) string {
	// Default to toolhive format if not specified
	format := sourceConfig["format"]
	if format == "" {
		format = "toolhive"
	}

	configContent := fmt.Sprintf(`registryName: %s

source:
  type: %s
  format: %s
`, registryName, sourceType, format)

	// Add source-specific configuration
	switch sourceType {
	case "git":
		configContent += fmt.Sprintf(`  git:
    url: %s
    path: %s
`, sourceConfig["url"], sourceConfig["path"])
		if branch, ok := sourceConfig["branch"]; ok {
			configContent += fmt.Sprintf(`    branch: %s
`, branch)
		}
		if tag, ok := sourceConfig["tag"]; ok {
			configContent += fmt.Sprintf(`    tag: %s
`, tag)
		}

	case "api":
		configContent += fmt.Sprintf(`  api:
    endpoint: %s
`, sourceConfig["endpoint"])

	case "file":
		configContent += fmt.Sprintf(`  file:
    path: %s
`, sourceConfig["path"])
	}

	// Add sync policy if configured
	if interval, ok := sourceConfig["syncInterval"]; ok {
		configContent += fmt.Sprintf(`
syncPolicy:
  interval: %s
`, interval)
	} else {
		// Default sync policy for tests
		configContent += `
syncPolicy:
  interval: 1h
`
	}

	// Note: storagePath is handled via WithDataDirectory() in the app builder,
	// not as a config file field

	// Add filter configuration if provided
	if filterOpts != nil && (len(filterOpts.NameInclude) > 0 || len(filterOpts.NameExclude) > 0 || len(filterOpts.TagInclude) > 0 || len(filterOpts.TagExclude) > 0) {
		configContent += `
filter:
`
		// Add name filters
		if len(filterOpts.NameInclude) > 0 || len(filterOpts.NameExclude) > 0 {
			configContent += `  names:
`
			if len(filterOpts.NameInclude) > 0 {
				configContent += `    include:
`
				for _, pattern := range filterOpts.NameInclude {
					configContent += fmt.Sprintf(`      - "%s"
`, pattern)
				}
			}
			if len(filterOpts.NameExclude) > 0 {
				configContent += `    exclude:
`
				for _, pattern := range filterOpts.NameExclude {
					configContent += fmt.Sprintf(`      - "%s"
`, pattern)
				}
			}
		}

		// Add tag filters
		if len(filterOpts.TagInclude) > 0 || len(filterOpts.TagExclude) > 0 {
			configContent += `  tags:
`
			if len(filterOpts.TagInclude) > 0 {
				configContent += `    include:
`
				for _, tag := range filterOpts.TagInclude {
					configContent += fmt.Sprintf(`      - %s
`, tag)
				}
			}
			if len(filterOpts.TagExclude) > 0 {
				configContent += `    exclude:
`
				for _, tag := range filterOpts.TagExclude {
					configContent += fmt.Sprintf(`      - %s
`, tag)
				}
			}
		}
	}

	configPath := fmt.Sprintf("%s/config.yaml", dir)
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return configPath
}
