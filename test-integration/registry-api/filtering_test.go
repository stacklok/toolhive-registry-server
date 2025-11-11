package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

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

		// Create separate directories for source data and synced/filtered data
		sourceDir := filepath.Join(tempDir, "source")
		storageDir = filepath.Join(tempDir, "storage")

		err := os.MkdirAll(sourceDir, 0750)
		Expect(err).NotTo(HaveOccurred())
		err = os.MkdirAll(storageDir, 0750)
		Expect(err).NotTo(HaveOccurred())

		// Source file with unfiltered data
		registryFile = filepath.Join(sourceDir, "registry.json")

		// Create test servers with various tags and names for filtering
		// This mirrors the operator test's server setup
		testServers := []helpers.RegistryServer{
			{
				Name:        "production-server",
				Description: "Production server",
				Tier:        "Official",
				Status:      "Active",
				Transport:   "stdio",
				Tools:       []string{"prod_tool"},
				Image:       "test/prod:1.0.0",
				Tags:        []string{"production", "stable"},
			},
			{
				Name:        "test-server-alpha",
				Description: "Test server alpha",
				Tier:        "Community",
				Status:      "Active",
				Transport:   "streamable-http",
				Tools:       []string{"test_tool_alpha"},
				Image:       "test/alpha:1.0.0",
				Tags:        []string{"testing", "experimental"},
			},
			{
				Name:        "test-server-beta",
				Description: "Test server beta",
				Tier:        "Community",
				Status:      "Active",
				Transport:   "stdio",
				Tools:       []string{"test_tool_beta"},
				Image:       "test/beta:1.0.0",
				Tags:        []string{"testing", "beta"},
			},
			{
				Name:        "dev-server",
				Description: "Development server",
				Tier:        "Community",
				Status:      "Active",
				Transport:   "sse",
				Tools:       []string{"dev_tool"},
				Image:       "test/dev:1.0.0",
				Tags:        []string{"development", "unstable"},
			},
			{
				Name:        "stable-server",
				Description: "Stable production server",
				Tier:        "Official",
				Status:      "Active",
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
			// Create config with name include filter (similar to operator's WithNameIncludeFilter)
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "name-include-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					NameInclude: []string{"production-*", "dev-*"},
				})

			serverHelper, err := helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			// Verify filtering applied - should include only production-server and dev-server
			servers := serverHelper.WaitForServers(2, 10*time.Second)

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("dev-server"))
			Expect(serverNames).NotTo(ContainElement("test-server-alpha"))
			Expect(serverNames).NotTo(ContainElement("test-server-beta"))
			Expect(serverNames).NotTo(ContainElement("stable-server"))
		})

		It("should apply name exclude filters correctly", func() {
			// Create config with name exclude filter
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "name-exclude-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					NameExclude: []string{"test-*"},
				})

			serverHelper, err := helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(3, 10*time.Second)

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("dev-server"))
			Expect(serverNames).To(ContainElement("stable-server"))
			Expect(serverNames).NotTo(ContainElement("test-server-alpha"))
			Expect(serverNames).NotTo(ContainElement("test-server-beta"))
		})

		It("should apply both name include and exclude filters correctly", func() {

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

			serverHelper, err := helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(2, 10*time.Second)

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

			// Create config with tag include filter
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "tag-include-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					TagInclude: []string{"production", "testing"},
				})

			serverHelper, err := helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(4, 10*time.Second)

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("stable-server"))
			Expect(serverNames).To(ContainElement("test-server-alpha"))
			Expect(serverNames).To(ContainElement("test-server-beta"))
			Expect(serverNames).NotTo(ContainElement("dev-server")) // dev-server has development tag
		})

		It("should apply tag exclude filters correctly", func() {

			// Create config with tag exclude filter
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "tag-exclude-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					TagExclude: []string{"experimental", "unstable"},
				})

			serverHelper, err := helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(3, 10*time.Second)

			serverNames := extractServerNames(servers)
			Expect(serverNames).To(ContainElement("production-server"))
			Expect(serverNames).To(ContainElement("test-server-beta"))
			Expect(serverNames).To(ContainElement("stable-server"))
			Expect(serverNames).NotTo(ContainElement("test-server-alpha"))
			Expect(serverNames).NotTo(ContainElement("dev-server"))
		})

		It("should apply both tag include and exclude filters correctly", func() {

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

			serverHelper, err := helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(3, 10*time.Second)

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

			serverHelper, err := helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(2, 10*time.Second)

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

			// Create config with filter that matches nothing
			configFile := helpers.WriteConfigYAMLWithOptions(tempDir, "empty-filter-test", "file",
				map[string]string{
					"path":        registryFile,
					"storagePath": storageDir,
				},
				&helpers.FilterOptions{
					TagInclude: []string{"nonexistent-tag"},
				})

			serverHelper, err := helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(0, 12*time.Second)
			Expect(servers).To(BeEmpty())
		})

		It("should handle conflicting filters gracefully", func() {

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

			serverHelper, err := helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(0, 12*time.Second)
			Expect(servers).To(BeEmpty())
		})
	})
})
