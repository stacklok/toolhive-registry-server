package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stacklok/toolhive-registry-server/test-integration/registry-api/helpers"
)

var _ = Describe("API Source Integration", Label("api"), func() {
	var (
		tempDir       string
		mockAPIServer *httptest.Server
		configFile    string
		serverHelper  *helpers.ServerTestHelper
		storageDir    string
	)

	BeforeEach(func() {
		tempDir = createTempDir("api-test-")
		storageDir = filepath.Join(tempDir, "storage")
		err := os.MkdirAll(storageDir, 0750)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if mockAPIServer != nil {
			mockAPIServer.Close()
		}
		cleanupTempDir(tempDir)
	})

	Context("ToolHive API Format", func() {
		BeforeEach(func() {
			// Create mock ToolHive API server
			mockAPIServer = helpers.NewToolHiveMockServer()

			configFile = helpers.WriteConfigYAML(tempDir, "api-registry", "api", map[string]string{
				"endpoint": mockAPIServer.URL,
			})

			var err error
			serverHelper, err = helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should successfully sync from ToolHive API endpoint", func() {
			err := serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(len(helpers.CreateOriginalTestServers()), 10*time.Second)
			Expect(servers).NotTo(BeEmpty())
		})

		It("should handle API endpoint failures gracefully", func() {
			// Create config with invalid endpoint
			badConfigFile := helpers.WriteConfigYAML(tempDir, "bad-api-registry", "api", map[string]string{
				"endpoint": "http://invalid-endpoint-does-not-exist.local:9999",
			})

			badServerHelper, err := helpers.NewServerTestHelper(ctx, badConfigFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = badServerHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = badServerHelper.StopServer()
			}()

			badServerHelper.WaitForServerReady(10 * time.Second)

			// Server should start even if initial sync fails
			// Should return empty results or handle gracefully
			resp, err := badServerHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Context("Custom API Responses", func() {
		It("should handle API with multiple servers", func() {
			// Create custom mock with multiple servers
			complexServers := helpers.CreateComplexTestServers()

			customMock := helpers.NewMockAPIServerBuilder().
				WithToolHiveInfo("1.0.0", "2025-01-15T12:00:00Z", "test", len(complexServers)).
				WithToolHiveServers(complexServers).
				Build()
			defer customMock.Close()

			for _, server := range complexServers {
				helpers.NewMockAPIServerBuilder().
					WithServerDetail(
						server.Name,
						server.Description,
						server.Tier,
						server.Status,
						server.Transport,
						server.Image,
					)
			}

			configFile = helpers.WriteConfigYAML(tempDir, "multi-server-api", "api", map[string]string{
				"endpoint": customMock.URL,
			})

			var err error
			serverHelper, err = helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(len(complexServers), 10*time.Second)
			Expect(servers).NotTo(BeEmpty())
			Expect(servers).To(HaveLen(len(complexServers)))
		})
	})

	Context("Automatic Sync from API", func() {
		var (
			dynamicServers   []helpers.RegistryServer
			updateServers    func([]helpers.RegistryServer)
			dynamicAPIServer *httptest.Server
		)

		BeforeEach(func() {
			// Create dynamic mock API that can be updated during tests
			dynamicServers = helpers.CreateOriginalTestServers()

			dynamicAPIServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v0/info":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{
						"version": "1.0.0",
						"last_updated": "2025-01-15T12:00:00Z",
						"source": "dynamic-api",
						"total_servers": ` + fmt.Sprintf("%d", len(dynamicServers)) + `
					}`))

				case "/v0/servers":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					serversJSON := "["
					for i, server := range dynamicServers {
						if i > 0 {
							serversJSON += ","
						}
						serversJSON += `{
							"name": "` + server.Name + `",
							"description": "` + server.Description + `",
							"tier": "` + server.Tier + `",
							"status": "` + server.Status + `",
							"transport": "` + server.Transport + `"
						}`
					}
					serversJSON += "]"
					_, _ = w.Write([]byte(`{"servers": ` + serversJSON + `, "total": ` + strconv.Itoa(len(dynamicServers)) + `}`))

				default:
					// Check for server detail requests
					for _, server := range dynamicServers {
						if r.URL.Path == "/v0/servers/"+server.Name {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusOK)
							_, _ = w.Write([]byte(`{
								"name": "` + server.Name + `",
								"description": "` + server.Description + `",
								"tier": "` + server.Tier + `",
								"status": "` + server.Status + `",
								"transport": "` + server.Transport + `",
								"image": "` + server.Image + `"
							}`))
							return
						}
					}
					w.WriteHeader(http.StatusNotFound)
				}
			}))

			// Helper function to update servers
			updateServers = func(newServers []helpers.RegistryServer) {
				dynamicServers = newServers
			}

			// Create config with short sync interval (5 seconds)
			configFile = helpers.WriteConfigYAML(tempDir, "auto-sync-registry", "api", map[string]string{
				"endpoint":     dynamicAPIServer.URL,
				"syncInterval": "5s",
			})

			var err error
			serverHelper, err = helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if dynamicAPIServer != nil {
				dynamicAPIServer.Close()
			}
		})

		It("should periodically re-sync from API endpoint", func() {
			// Start server and wait for it to be ready
			err := serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)

			// Verify initial data is loaded (original test servers)
			servers := serverHelper.WaitForServers(len(dynamicServers), 10*time.Second)
			Expect(servers).NotTo(BeEmpty())
			Expect(servers[0].Name).To(Equal("filesystem"))
			Expect(servers[0].Description).To(Equal("File system operations for secure file access"))
			Expect(servers).To(HaveLen(1))

			// Update the mock API to return different data
			updatedServers := helpers.CreateUpdatedTestServers()
			updateServers(updatedServers)

			// Wait for automatic re-sync (sync interval is 5s + cache 1s + buffer)
			syncedServers := serverHelper.WaitForServers(len(updatedServers), 8*time.Second)
			// Check if any server has been updated (look for "UPDATED" in description)
			Expect(syncedServers[0].Description).To(ContainSubstring("UPDATED"))

			// Verify the synced data contains both servers
			Expect(syncedServers).To(HaveLen(2)) // Should have filesystem and github

			// Verify filesystem server is updated
			serverNames := extractServerNames(syncedServers)
			Expect(serverNames).To(ContainElement("filesystem"))
			Expect(serverNames).To(ContainElement("github"))

			// Find and verify filesystem server
			Expect(updatedServers[0].Description).To(ContainSubstring("UPDATED"))
			Expect(updatedServers[0].Image).To(Equal("ghcr.io/modelcontextprotocol/server-filesystem:v2.0.0"))
		})

		It("should retry failed syncs at configured interval", func() {
			// Create a failing API server that returns 500 errors
			failureCount := 0
			maxFailures := 3
			servers := helpers.CreateOriginalTestServers()
			failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failureCount < maxFailures {
					failureCount++
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error": "internal server error"}`))
					return
				}

				// After maxFailures, start returning success
				switch r.URL.Path {
				case "/v0/info":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{
						"version": "1.0.0",
						"last_updated": "2025-01-15T12:00:00Z",
						"source": "recovered-api",
						"total_servers": 1
					}`))

				case "/v0/servers":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					serversJSON := "["
					for i, server := range servers {
						if i > 0 {
							serversJSON += ","
						}
						serversJSON += `{
							"name": "` + server.Name + `",
							"description": "` + server.Description + `",
							"tier": "` + server.Tier + `",
							"status": "` + server.Status + `",
							"transport": "` + server.Transport + `"
						}`
					}
					serversJSON += "]"
					_, _ = w.Write([]byte(`{"servers": ` + serversJSON + `, "total": 1}`))

				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer failingServer.Close()

			// Create config with short retry interval
			retryConfigFile := helpers.WriteConfigYAML(tempDir, "retry-registry", "api", map[string]string{
				"endpoint":     failingServer.URL,
				"syncInterval": "3s",
			})

			retryServerHelper, err := helpers.NewServerTestHelper(ctx, retryConfigFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
			err = retryServerHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = retryServerHelper.StopServer()
			}()

			// Server should start even if initial sync fails
			retryServerHelper.WaitForServerReady(10 * time.Second)
			// Eventually, after retries (3s interval), the sync should succeed
			retryServerHelper.WaitForServers(len(servers), 12*time.Second)

			// Verify that we had the expected number of failures
			Expect(failureCount).To(BeNumerically(">=", maxFailures))
		})
	})
})
