package helpers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/onsi/gomega"
)

// ServerTestHelper manages the registry API server lifecycle for testing
type ServerTestHelper struct {
	ctx        context.Context
	configPath string
	baseURL    string
	httpClient *http.Client
}

// NewServerTestHelper creates a new server test helper
func NewServerTestHelper(ctx context.Context, configPath string, port int) *ServerTestHelper {
	return &ServerTestHelper{
		ctx:        ctx,
		configPath: configPath,
		baseURL:    fmt.Sprintf("http://localhost:%d", port),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
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

// GetServers makes a GET request to /api/v0/servers
func (s *ServerTestHelper) GetServers() (*http.Response, error) {
	return s.httpClient.Get(s.baseURL + "/api/v0/servers")
}

// GetServer makes a GET request to /api/v0/servers/{name}
func (s *ServerTestHelper) GetServer(name string) (*http.Response, error) {
	return s.httpClient.Get(fmt.Sprintf("%s/api/v0/servers/%s", s.baseURL, name))
}

// GetDeployed makes a GET request to /api/v0/deployed
func (s *ServerTestHelper) GetDeployed() (*http.Response, error) {
	return s.httpClient.Get(s.baseURL + "/api/v0/deployed")
}

// GetDeployedServer makes a GET request to /api/v0/deployed/{name}
func (s *ServerTestHelper) GetDeployedServer(name string) (*http.Response, error) {
	return s.httpClient.Get(fmt.Sprintf("%s/api/v0/deployed/%s", s.baseURL, name))
}

// GetHealth makes a GET request to /health
func (s *ServerTestHelper) GetHealth() (*http.Response, error) {
	return s.httpClient.Get(s.baseURL + "/health")
}

// GetBaseURL returns the base URL of the server
func (s *ServerTestHelper) GetBaseURL() string {
	return s.baseURL
}

// WriteConfigYAML writes a YAML configuration file for testing
func WriteConfigYAML(dir, registryName, sourceType string, sourceConfig map[string]string) string {
	configContent := fmt.Sprintf(`registryName: %s

source:
  type: %s
`, registryName, sourceType)

	// Add source-specific configuration
	switch sourceType {
	case "configmap":
		configContent += fmt.Sprintf(`  configmap:
    name: %s
    namespace: %s
    key: registry.json
`, sourceConfig["name"], sourceConfig["namespace"])

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
	}

	// Add storage configuration
	if storagePath, ok := sourceConfig["storagePath"]; ok {
		configContent += fmt.Sprintf(`
storage:
  path: %s
`, storagePath)
	}

	configPath := fmt.Sprintf("%s/config.yaml", dir)
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return configPath
}
