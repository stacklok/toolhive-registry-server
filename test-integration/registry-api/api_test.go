package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
				"endpoint":    mockAPIServer.URL,
				"storagePath": storageDir,
			})

			serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8087)
		})

		It("should successfully sync from ToolHive API endpoint", func() {
			Skip("Server integration pending - demonstrates API sync")

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

			servers, ok := response["servers"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveKey("filesystem"))
		})

		It("should handle API endpoint failures gracefully", func() {
			Skip("Server integration pending - demonstrates error handling")

			// Create config with invalid endpoint
			badConfigFile := helpers.WriteConfigYAML(tempDir, "bad-api-registry", "api", map[string]string{
				"endpoint":    "http://invalid-endpoint-does-not-exist.local:9999",
				"storagePath": storageDir,
			})

			badServerHelper := helpers.NewServerTestHelper(ctx, badConfigFile, 8088)

			// Server should handle the error gracefully
			// Could verify via health endpoint showing degraded state
			// TODO use /sync andpoint instead
			_ = badServerHelper
		})
	})

	Context("Custom API Responses", func() {
		It("should handle API with multiple servers", func() {
			Skip("Server integration pending - demonstrates multiple servers")

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
				"endpoint":    customMock.URL,
				"storagePath": storageDir,
			})

			serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8089)
			serverHelper.WaitForServerReady(30)

			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			servers, ok := response["servers"].(map[string]interface{})
			Expect(ok).To(BeTrue())
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
						"total_servers": ` + string(rune(len(dynamicServers)+'0')) + `
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
					_, _ = w.Write([]byte(`{"servers": ` + serversJSON + `, "total": ` + string(rune(len(dynamicServers)+'0')) + `}`))

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
				"storagePath":  storageDir,
				"syncInterval": "5s",
			})

			serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8090)
		})

		AfterEach(func() {
			if dynamicAPIServer != nil {
				dynamicAPIServer.Close()
			}
		})

		It("should periodically re-sync from API endpoint", func() {
			Skip("Server integration pending - demonstrates periodic API polling")

			// Start server and wait for it to be ready
			serverHelper.WaitForServerReady(30)

			// Verify initial data is loaded (original test servers)
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var initialResponse map[string]interface{}
			err = json.Unmarshal(body, &initialResponse)
			Expect(err).NotTo(HaveOccurred())

			servers, ok := initialResponse["servers"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveKey("filesystem"))

			// Verify initial filesystem server data
			filesystemServer, ok := servers["filesystem"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(filesystemServer["description"]).To(Equal("File system operations for secure file access"))

			// Update the mock API to return different data
			updateServers(helpers.CreateUpdatedTestServers())

			// Wait for automatic re-sync (sync interval is 5s, wait 8s to be safe)
			// The sync coordinator should detect the changes and update the data
			Eventually(func() string {
				resp, err := serverHelper.GetServers()
				if err != nil {
					return ""
				}
				defer func() {
					_ = resp.Body.Close()
				}()

				body, err := io.ReadAll(resp.Body)
				if err != nil {
					return ""
				}

				var response map[string]interface{}
				if err := json.Unmarshal(body, &response); err != nil {
					return ""
				}

				servers, ok := response["servers"].(map[string]interface{})
				if !ok {
					return ""
				}

				// Check if filesystem server has been updated
				if fs, ok := servers["filesystem"].(map[string]interface{}); ok {
					if desc, ok := fs["description"].(string); ok {
						return desc
					}
				}
				return ""
			}, 12*time.Second, 1*time.Second).Should(ContainSubstring("UPDATED"))

			// Verify the synced data contains both servers
			resp, err = serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			body, err = io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var updatedResponse map[string]interface{}
			err = json.Unmarshal(body, &updatedResponse)
			Expect(err).NotTo(HaveOccurred())

			updatedServers, ok := updatedResponse["servers"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(updatedServers).To(HaveKey("filesystem"))
			Expect(updatedServers).To(HaveKey("github"))

			// Verify filesystem server is updated
			updatedFilesystem, ok := updatedServers["filesystem"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(updatedFilesystem["description"]).To(ContainSubstring("UPDATED"))
			Expect(updatedFilesystem["image"]).To(Equal("ghcr.io/modelcontextprotocol/server-filesystem:v2.0.0"))
		})

		It("should retry failed syncs at configured interval", func() {
			Skip("Retry logic testing - demonstrates periodic retry at sync interval")

			// Create a failing API server that returns 500 errors
			failureCount := 0
			maxFailures := 3
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
					servers := helpers.CreateOriginalTestServers()
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
				"storagePath":  storageDir,
				"syncInterval": "3s",
			})

			retryServerHelper := helpers.NewServerTestHelper(ctx, retryConfigFile, 8091)

			// Server should start even if initial sync fails
			retryServerHelper.WaitForServerReady(30)

			// Eventually, after retries, the sync should succeed
			Eventually(func() int {
				resp, err := retryServerHelper.GetServers()
				if err != nil {
					return 0
				}
				defer func() {
					_ = resp.Body.Close()
				}()

				if resp.StatusCode != http.StatusOK {
					return 0
				}

				body, err := io.ReadAll(resp.Body)
				if err != nil {
					return 0
				}

				var response map[string]interface{}
				if err := json.Unmarshal(body, &response); err != nil {
					return 0
				}

				servers, ok := response["servers"].(map[string]interface{})
				if !ok {
					return 0
				}

				return len(servers)
			}, 15*time.Second, 1*time.Second).Should(Equal(1))

			// Verify that we had the expected number of failures
			Expect(failureCount).To(BeNumerically(">=", maxFailures))

			// Verify the synced data is correct
			resp, err := retryServerHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = resp.Body.Close()
			}()

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			servers, ok := response["servers"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveKey("filesystem"))
		})
	})
})
