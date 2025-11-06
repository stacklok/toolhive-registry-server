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

var _ = Describe("File Source Integration", Label("file"), func() {
	var (
		tempDir        string
		registryFile   string
		configFile     string
		serverHelper   *helpers.ServerTestHelper
		testServers    []helpers.RegistryServer
		storageDir     string
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
			"path":        registryFile,
			"storagePath": storageDir,
		})

		// Note: Actual server startup would happen here in a real implementation
		// For now, we're creating the helper to demonstrate the pattern
		serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8080)
	})

	AfterEach(func() {
		cleanupTempDir(tempDir)
	})

	Context("Loading from Local File", func() {
		It("should successfully load registry data from a file", func() {
			Skip("Server integration pending - demonstrates test structure")

			// This demonstrates what the test would do:
			// 1. Start the registry server with file source config
			// 2. Wait for server to be ready
			serverHelper.WaitForServerReady(30)

			// 3. Query the API to verify data was loaded
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// 4. Verify the response contains expected servers
			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			servers, ok := response["servers"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveLen(len(testServers)))
		})

		It("should handle missing file gracefully", func() {
			Skip("Server integration pending - demonstrates test structure")

			// Create config pointing to non-existent file
			badConfigFile := helpers.WriteConfigYAML(tempDir, "bad-registry", "file", map[string]string{
				"path":        "/nonexistent/registry.json",
				"storagePath": storageDir,
			})

			badServerHelper := helpers.NewServerTestHelper(ctx, badConfigFile, 8081)

			// Server should fail to start or return error status
			// This would be verified by checking health endpoint or startup errors
			_ = badServerHelper // Use variable to avoid compiler error
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

	Context("File Updates", func() {
		It("should detect file changes when file watching is enabled", func() {
			Skip("File watching not yet implemented - future enhancement")

			// This would test:
			// 1. Start server with file source
			// 2. Verify initial data loaded
			// 3. Modify the registry file
			// 4. Wait for automatic reload
			// 5. Verify API returns updated data
		})
	})
})
