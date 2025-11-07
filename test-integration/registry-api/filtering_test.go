package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stacklok/toolhive-registry-server/test-integration/registry-api/helpers"
)

var _ = Describe("Registry Filtering", Label("filtering"), func() {
	var (
		tempDir       string
		storageDir    string
		registryFile  string
		configMapData *helpers.ToolHiveRegistryData
	)

	BeforeEach(func() {
		tempDir = createTempDir("filtering-test-")
		storageDir = filepath.Join(tempDir, "storage")
		registryFile = filepath.Join(storageDir, "registry.json")
		err := os.MkdirAll(storageDir, 0750)
		Expect(err).NotTo(HaveOccurred())

		// Create test servers with various tags and names for filtering
		// This mirrors the operator test's server setup
		testServers := []helpers.RegistryServer{
			{
				Name:        "production-server",
				Description: "Production server",
				Tier:        "official",
				Status:      "active",
				Transport:   "stdio",
				Tools:       []string{"prod_tool"},
				Image:       "test/prod:1.0.0",
				Tags:        []string{"production", "stable"},
			},
			{
				Name:        "test-server-alpha",
				Description: "Test server alpha",
				Tier:        "community",
				Status:      "active",
				Transport:   "streamable-http",
				Tools:       []string{"test_tool_alpha"},
				Image:       "test/alpha:1.0.0",
				Tags:        []string{"testing", "experimental"},
			},
			{
				Name:        "test-server-beta",
				Description: "Test server beta",
				Tier:        "community",
				Status:      "active",
				Transport:   "stdio",
				Tools:       []string{"test_tool_beta"},
				Image:       "test/beta:1.0.0",
				Tags:        []string{"testing", "beta"},
			},
			{
				Name:        "dev-server",
				Description: "Development server",
				Tier:        "community",
				Status:      "active",
				Transport:   "sse",
				Tools:       []string{"dev_tool"},
				Image:       "test/dev:1.0.0",
				Tags:        []string{"development", "unstable"},
			},
			{
				Name:        "stable-server",
				Description: "Stable production server",
				Tier:        "official",
				Status:      "active",
				Transport:   "stdio",
				Tools:       []string{"stable_tool"},
				Image:       "test/stable:1.0.0",
				Tags:        []string{"production", "stable", "verified"},
			},
		}

		// Create registry data in ToolHive format
		configMapData = &helpers.ToolHiveRegistryData{
			Version:       "1.0.0",
			LastUpdated:   "2025-01-15T10:30:00Z",
			Servers:       helpers.ServersToMap(testServers),
			RemoteServers: make(map[string]helpers.RegistryServer),
		}

		// Write registry data to file
		jsonData, err := json.MarshalIndent(configMapData, "", "  ")
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(registryFile, jsonData, 0600)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cleanupTempDir(tempDir)
	})

	Context("Name-based filtering", func() {
		It("should apply name include filters correctly", func() {
			Skip("Server integration pending - demonstrates name-based include filtering")

			// Create config with name include filter (similar to operator's WithNameIncludeFilter)
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "name-include-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					NameInclude: []string{"production-*", "dev-*"},
				})

			serverHelper := helpers.NewServerTestHelper(ctx, configFile, 8095)
			serverHelper.WaitForServerReady(30)

			// Verify filtering applied - should include only production-server and dev-server
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			// Verify the API returns filtered servers
			servers, ok := response["servers"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveLen(2)) // Only production-server and dev-server

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("dev-server"))
			Expect(serverNames).NotTo(ContainElement("test-server-alpha"))
			Expect(serverNames).NotTo(ContainElement("test-server-beta"))
			Expect(serverNames).NotTo(ContainElement("stable-server"))
		})

		It("should apply name exclude filters correctly", func() {
			Skip("Server integration pending - demonstrates name-based exclude filtering")

			// Create config with name exclude filter
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "name-exclude-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					NameExclude: []string{"test-*"},
				})

			serverHelper := helpers.NewServerTestHelper(ctx, configFile, 8096)
			serverHelper.WaitForServerReady(30)

			// Verify filtering applied - should exclude test-server-alpha and test-server-beta
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			servers, ok := response["servers"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveLen(3)) // production-server, dev-server, stable-server

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("dev-server"))
			Expect(serverNames).To(ContainElement("stable-server"))
			Expect(serverNames).NotTo(ContainElement("test-server-alpha"))
			Expect(serverNames).NotTo(ContainElement("test-server-beta"))
		})

		It("should apply both name include and exclude filters correctly", func() {
			Skip("Server integration pending - demonstrates combined name filtering")

			// Create config with both include and exclude filters
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "name-include-exclude-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					NameInclude: []string{"*-server*"},       // Include all servers
					NameExclude: []string{"test-*", "dev-*"}, // Exclude test and dev servers
				})

			serverHelper := helpers.NewServerTestHelper(ctx, configFile, 8097)
			serverHelper.WaitForServerReady(30)

			// Verify filtering applied - should only include production-server and stable-server
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			servers, ok := response["servers"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveLen(2)) // Only production-server and stable-server

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("stable-server"))
			Expect(serverNames).NotTo(ContainElement("test-server-alpha"))
			Expect(serverNames).NotTo(ContainElement("test-server-beta"))
			Expect(serverNames).NotTo(ContainElement("dev-server"))
		})
	})

	Context("Tag-based filtering", func() {
		It("should apply tag include filters correctly", func() {
			Skip("Server integration pending - demonstrates tag-based include filtering")

			// Create config with tag include filter
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "tag-include-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					TagInclude: []string{"production", "testing"},
				})

			serverHelper := helpers.NewServerTestHelper(ctx, configFile, 8098)
			serverHelper.WaitForServerReady(30)

			// Verify filtering applied - should include servers with production or testing tags
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			// Should include production-server, stable-server, test-server-alpha, test-server-beta
			servers, ok := response["servers"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveLen(4))

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("stable-server"))
			Expect(serverNames).To(ContainElement("test-server-alpha"))
			Expect(serverNames).To(ContainElement("test-server-beta"))
			Expect(serverNames).NotTo(ContainElement("dev-server")) // dev-server has development tag
		})

		It("should apply tag exclude filters correctly", func() {
			Skip("Server integration pending - demonstrates tag-based exclude filtering")

			// Create config with tag exclude filter
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "tag-exclude-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					TagExclude: []string{"experimental", "unstable"},
				})

			serverHelper := helpers.NewServerTestHelper(ctx, configFile, 8099)
			serverHelper.WaitForServerReady(30)

			// Verify filtering applied - should exclude test-server-alpha and dev-server
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			servers, ok := response["servers"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveLen(3)) // production-server, test-server-beta, stable-server

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("test-server-beta"))
			Expect(serverNames).To(ContainElement("stable-server"))
			Expect(serverNames).NotTo(ContainElement("test-server-alpha"))
			Expect(serverNames).NotTo(ContainElement("dev-server"))
		})

		It("should apply both tag include and exclude filters correctly", func() {
			Skip("Server integration pending - demonstrates combined tag filtering")

			// Create config with both tag include and exclude filters
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "tag-include-exclude-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					TagInclude: []string{"production", "testing"},
					TagExclude: []string{"experimental"},
				})

			serverHelper := helpers.NewServerTestHelper(ctx, configFile, 8100)
			serverHelper.WaitForServerReady(30)

			// Verify filtering applied
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			// Should include production-server, stable-server, test-server-beta
			// Should exclude test-server-alpha (experimental) and dev-server (no production/testing tag)
			servers, ok := response["servers"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveLen(3))

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("stable-server"))
			Expect(serverNames).To(ContainElement("test-server-beta"))
			Expect(serverNames).NotTo(ContainElement("test-server-alpha"))
			Expect(serverNames).NotTo(ContainElement("dev-server"))
		})
	})

	Context("Combined filtering (name and tag)", func() {
		It("should apply both name and tag filters together", func() {
			Skip("Server integration pending - demonstrates combined name and tag filtering")

			// Create config with both name and tag filters
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "combined-filter-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					NameInclude: []string{"*-server*"},
					TagInclude:  []string{"production"},
					TagExclude:  []string{"experimental"},
				})

			serverHelper := helpers.NewServerTestHelper(ctx, configFile, 8101)
			serverHelper.WaitForServerReady(30)

			// Verify filtering applied
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			// Should only include production-server and stable-server
			servers, ok := response["servers"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveLen(2))

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("stable-server"))
			Expect(serverNames).NotTo(ContainElement("test-server-alpha"))
			Expect(serverNames).NotTo(ContainElement("test-server-beta"))
			Expect(serverNames).NotTo(ContainElement("dev-server"))
		})
	})

	Context("Filter updates", func() {
		It("should update served content when filters are changed", func() {
			Skip("Server integration pending - demonstrates filter update handling")

			// This test would demonstrate updating the config file with new filters
			// and verifying the server re-syncs and serves the newly filtered data
			// Similar to the operator test that updates the MCPRegistry CR

			// 1. Start with initial filter configuration
			// 2. Verify initial filtered results
			// 3. Update the config file with new filters
			// 4. Trigger a config reload (or wait for auto-sync)
			// 5. Verify the new filtered results
		})
	})

	Context("Empty and edge case filters", func() {
		It("should return empty results for non-matching filters", func() {
			Skip("Server integration pending - demonstrates empty filter results")

			// Create config with filter that matches nothing
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "empty-filter-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					TagInclude: []string{"nonexistent-tag"},
				})

			serverHelper := helpers.NewServerTestHelper(ctx, configFile, 8102)
			serverHelper.WaitForServerReady(30)

			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			// Should return empty list
			servers, ok := response["servers"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(BeEmpty())
		})

		It("should handle conflicting filters gracefully", func() {
			Skip("Server integration pending - demonstrates conflicting filter handling")

			// Include and exclude the same name pattern (conflicting)
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "conflict-filter-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					NameInclude: []string{"production-*"},
					NameExclude: []string{"production-*"},
				})

			serverHelper := helpers.NewServerTestHelper(ctx, configFile, 8103)
			serverHelper.WaitForServerReady(30)

			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			// Exclude takes precedence - should return empty list
			servers, ok := response["servers"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(BeEmpty())
		})
	})
})

// extractServerNames extracts server names from the API response servers list
func extractServerNames(servers []interface{}) []string {
	names := make([]string, 0, len(servers))
	for _, s := range servers {
		if serverMap, ok := s.(map[string]interface{}); ok {
			if name, ok := serverMap["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}
