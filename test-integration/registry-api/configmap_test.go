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

var _ = Describe("ConfigMap Source Integration", Label("k8s", "configmap"), func() {
	var (
		tempDir         string
		testNamespace   string
		configMapHelper *helpers.ConfigMapTestHelper
		configFile      string
		serverHelper    *helpers.ServerTestHelper
		storageDir      string
	)

	BeforeEach(func() {
		tempDir = createTempDir("configmap-test-")
		storageDir = filepath.Join(tempDir, "storage")
		err := os.MkdirAll(storageDir, 0750)
		Expect(err).NotTo(HaveOccurred())

		// Create test namespace
		testNamespace = createTestNamespace(ctx)

		// Initialize ConfigMap helper
		configMapHelper = helpers.NewConfigMapTestHelper(ctx, k8sClient, testNamespace)
	})

	AfterEach(func() {
		// Cleanup ConfigMaps
		if configMapHelper != nil {
			_ = configMapHelper.CleanupConfigMaps()
		}
		deleteTestNamespace(ctx, testNamespace)
		cleanupTempDir(tempDir)
	})

	Context("Basic ConfigMap Sync", func() {
		var (
			configMapName string
			testServers   []helpers.RegistryServer
		)

		BeforeEach(func() {
			names := helpers.NewUniqueNames("cm-basic")
			configMapName = names.ConfigMapName

			// Create test servers
			testServers = helpers.CreateOriginalTestServers()

			// Create ConfigMap with registry data
			_ = configMapHelper.NewConfigMapBuilder(configMapName).
				WithToolHiveRegistry("registry.json", testServers).
				Create(configMapHelper)

			// Create config file
			configFile = helpers.WriteConfigYAML(tempDir, "cm-registry", "configmap", map[string]string{
				"name":        configMapName,
				"namespace":   testNamespace,
				"storagePath": storageDir,
			})

			serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8090)
		})

		AfterEach(func() {
			// Cleanup ConfigMap created in this context
			if configMapName != "" {
				_ = configMapHelper.DeleteConfigMap(configMapName)
			}
		})

		It("should successfully load registry from Kubernetes ConfigMap", func() {
			Skip("Server integration pending - demonstrates ConfigMap sync")

			serverHelper.WaitForServerReady(30)

			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			servers, ok := response["servers"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveKey("filesystem"))
		})

		It("should handle ConfigMap not found error", func() {
			Skip("Server integration pending - demonstrates error handling")

			// Create config pointing to non-existent ConfigMap
			badConfigFile := helpers.WriteConfigYAML(tempDir, "bad-cm-registry", "configmap", map[string]string{
				"name":        "nonexistent-configmap",
				"namespace":   testNamespace,
				"storagePath": storageDir,
			})

			badServerHelper := helpers.NewServerTestHelper(ctx, badConfigFile, 8091)

			// Server should handle missing ConfigMap gracefully
			_ = badServerHelper
		})
	})

	Context("ConfigMap Updates", func() {
		var (
			configMapName string
		)

		BeforeEach(func() {
			names := helpers.NewUniqueNames("cm-update")
			configMapName = names.ConfigMapName

			// Create initial ConfigMap
			initialServers := helpers.CreateOriginalTestServers()
			_ = configMapHelper.NewConfigMapBuilder(configMapName).
				WithToolHiveRegistry("registry.json", initialServers).
				Create(configMapHelper)

			configFile = helpers.WriteConfigYAML(tempDir, "cm-update-registry", "configmap", map[string]string{
				"name":         configMapName,
				"namespace":    testNamespace,
				"storagePath":  storageDir,
				"syncInterval": "5s",
			})

			serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8092)
		})

		AfterEach(func() {
			// Cleanup ConfigMap created in this context
			if configMapName != "" {
				_ = configMapHelper.DeleteConfigMap(configMapName)
			}
		})

		It("should periodically re-sync and detect ConfigMap updates", func() {
			Skip("Periodic sync testing - demonstrates automatic re-sync from ConfigMap")

			serverHelper.WaitForServerReady(30)

			// Verify initial data loaded
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var initialResponse map[string]interface{}
			err = json.Unmarshal(body, &initialResponse)
			Expect(err).NotTo(HaveOccurred())

			initialServers, ok := initialResponse["servers"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(initialServers).To(HaveLen(1), "Initial registry should have 1 server")

			// Update the ConfigMap with new data
			configMap, err := configMapHelper.GetConfigMap(configMapName)
			Expect(err).NotTo(HaveOccurred())

			updatedServers := helpers.CreateUpdatedTestServers()
			err = configMapHelper.UpdateConfigMapWithServers(configMap, updatedServers)
			Expect(err).NotTo(HaveOccurred())

			// Wait for periodic sync to detect the update
			// The sync interval is set to 5s in BeforeEach, so we wait up to 15s
			Eventually(func() int {
				resp, err := serverHelper.GetServers()
				if err != nil {
					return 0
				}
				defer resp.Body.Close()

				body, _ := io.ReadAll(resp.Body)
				var response map[string]interface{}
				_ = json.Unmarshal(body, &response)

				if servers, ok := response["servers"].(map[string]interface{}); ok {
					return len(servers)
				}
				return 0
			}, 15, 1).Should(Equal(2), "Periodic sync should detect ConfigMap update within sync interval")
		})

		It("should handle ConfigMap deletion and mark sync as failed", func() {
			Skip("ConfigMap deletion testing - demonstrates sync failure handling")

			serverHelper.WaitForServerReady(30)

			// Verify initial data loaded successfully
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			// Delete the ConfigMap source
			err = configMapHelper.DeleteConfigMap(configMapName)
			Expect(err).NotTo(HaveOccurred())

			// Wait for next periodic sync attempt (should fail)
			// The sync should fail but server should keep serving cached data
			// This would be verified by checking:
			// 1. /api/v0/servers still returns data (from cache)
			// 2. Sync status shows failure
			// 3. Last successful sync data is preserved

			// Server should still serve previously cached data
			Eventually(func() int {
				resp, err := serverHelper.GetServers()
				if err != nil {
					return 0
				}
				defer resp.Body.Close()
				return resp.StatusCode
			}, 10, 1).Should(Equal(http.StatusOK), "Server should continue serving cached data after source deletion")

			// Health endpoint should indicate degraded state
			resp, err = serverHelper.GetHealth()
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			// Note: Health check behavior depends on implementation
			// May return 200 with degraded status in body, or 503
			// Current implementation should be checked to verify expected behavior
		})
	})

	Context("Multiple ConfigMaps", func() {
		It("should support switching between different ConfigMaps", func() {
			Skip("ConfigMap switching - demonstrates configuration updates")

			// This would test:
			// 1. Start with ConfigMap A
			// 2. Verify data from A is served
			// 3. Update config to point to ConfigMap B
			// 4. Reload/restart server
			// 5. Verify data from B is served
		})
	})
})
