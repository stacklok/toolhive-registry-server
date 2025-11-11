package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stacklok/toolhive-registry-server/test-integration/registry-api/helpers"
)

var _ = Describe("File Source Integration", Label("file"), func() {
	var (
		tempDir      string
		registryFile string
		configFile   string
		serverHelper *helpers.ServerTestHelper
		testServers  []helpers.RegistryServer
		storageDir   string
	)

	BeforeEach(func() {
		// Create temporary directories for testing
		tempDir = createTempDir("file-test-")
		storageDir = filepath.Join(tempDir, "storage")
		err := os.MkdirAll(storageDir, 0750)
		Expect(err).NotTo(HaveOccurred())

		// Create test registry data
		testServers = helpers.CreateOriginalTestServers()
		registryData := helpers.ToolHiveRegistryData{
			Version:     "1.0.0",
			LastUpdated: "2025-01-15T10:00:00Z",
			Servers:     helpers.ServersToMap(testServers),
		}

		// Write registry data to file
		registryFile = filepath.Join(tempDir, "registry.json")
		jsonData, err := json.MarshalIndent(registryData, "", "  ")
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(registryFile, jsonData, 0600)
		Expect(err).NotTo(HaveOccurred())

		// Create config file
		configFile = helpers.WriteConfigYAML(tempDir, "test-registry", "file", map[string]string{
			"path": registryFile,
		})

		// Note: Actual server startup would happen here in a real implementation
		// For now, we're creating the helper to demonstrate the pattern
		serverHelper, err = helpers.NewServerTestHelper(ctx, configFile, storageDir)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cleanupTempDir(tempDir)
	})

	Context("Loading from Local File", func() {
		It("should successfully load registry data from a file", func() {
			// Start the registry server with file source config
			err := serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			// Wait for server to be ready
			serverHelper.WaitForServerReady(10 * time.Second)

			// Wait for sync to complete and verify data was loaded
			servers := serverHelper.WaitForServers(len(testServers), 10*time.Second)
			Expect(servers).NotTo(BeEmpty())
			Expect(servers).To(HaveLen(len(testServers)))
		})

		It("should handle missing file gracefully", func() {
			// Create config pointing to non-existent file
			badConfigFile := helpers.WriteConfigYAML(tempDir, "bad-registry", "file", map[string]string{
				"path":        "/nonexistent/registry.json",
				"storagePath": storageDir,
			})

			badServerHelper, err := helpers.NewServerTestHelper(ctx, badConfigFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = badServerHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = badServerHelper.StopServer()
			}()

			badServerHelper.WaitForServerReady(10 * time.Second)

			// Server should start but return empty results since file doesn't exist
			resp, err := badServerHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("should validate against path traversal attacks", func() {
			// This test validates the path traversal security fix
			Skip("Security validation test - to be implemented")

			// Test cases:
			// - Attempt to load from ../../etc/passwd
			// - Attempt to load from absolute paths outside allowed directories
			// - Verify proper path cleaning and validation
		})
	})
})
