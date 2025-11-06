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

var _ = Describe("Git Source Integration", Label("git"), func() {
	var (
		tempDir      string
		gitHelper    *helpers.GitTestHelper
		testRepo     *helpers.GitTestRepository
		configFile   string
		serverHelper *helpers.ServerTestHelper
		storageDir   string
	)

	BeforeEach(func() {
		// Create temporary directories
		tempDir = createTempDir("git-test-")
		storageDir = filepath.Join(tempDir, "storage")
		err := os.MkdirAll(storageDir, 0750)
		Expect(err).NotTo(HaveOccurred())

		// Setup Git test helper
		gitHelper = helpers.NewGitTestHelper(ctx)
	})

	AfterEach(func() {
		if gitHelper != nil {
			_ = gitHelper.CleanupRepositories()
		}
		cleanupTempDir(tempDir)
	})

	Context("Basic Git Repository Sync", func() {
		BeforeEach(func() {
			// Create test Git repository
			testRepo = gitHelper.CreateRepository("test-registry-repo")

			// Add registry data to the repository
			testServers := helpers.CreateOriginalTestServers()
			gitHelper.CommitRegistryData(testRepo, "registry.json", testServers, "Add initial registry")

			// Create config file
			configFile = helpers.WriteConfigYAML(tempDir, "git-registry", "git", map[string]string{
				"url":         testRepo.CloneURL,
				"path":        "registry.json",
				"branch":      "main",
				"storagePath": storageDir,
			})

			serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8082)
		})

		It("should successfully clone and load registry from Git repository", func() {
			Skip("Server integration pending - demonstrates test structure")

			// Start server and wait for it to be ready
			serverHelper.WaitForServerReady(30)

			// Verify data was loaded from Git
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
	})

	Context("Git Branches and Tags", func() {
		BeforeEach(func() {
			testRepo = gitHelper.CreateRepository("multi-branch-repo")

			// Commit to main branch
			mainServers := helpers.CreateOriginalTestServers()
			gitHelper.CommitRegistryData(testRepo, "registry.json", mainServers, "Main branch registry")

			// Create development branch with different data
			gitHelper.CreateBranch(testRepo, "development")
			devServers := helpers.CreateUpdatedTestServers()
			gitHelper.CommitRegistryData(testRepo, "registry.json", devServers, "Development registry")

			// Switch back to main and create a tag
			gitHelper.SwitchBranch(testRepo, "main")
			gitHelper.CreateTag(testRepo, "v1.0.0", "Release v1.0.0")
		})

		It("should sync from a specific branch", func() {
			Skip("Server integration pending - demonstrates branch testing")

			// Create config pointing to development branch
			configFile = helpers.WriteConfigYAML(tempDir, "branch-registry", "git", map[string]string{
				"url":         testRepo.CloneURL,
				"path":        "registry.json",
				"branch":      "development",
				"storagePath": storageDir,
			})

			serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8083)
			serverHelper.WaitForServerReady(30)

			// Verify we got the development branch data (2 servers)
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			servers, ok := response["servers"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveLen(2)) // Development has 2 servers
		})

		It("should sync from a specific tag", func() {
			Skip("Server integration pending - demonstrates tag testing")

			configFile = helpers.WriteConfigYAML(tempDir, "tag-registry", "git", map[string]string{
				"url":         testRepo.CloneURL,
				"path":        "registry.json",
				"tag":         "v1.0.0",
				"storagePath": storageDir,
			})

			serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8084)
			serverHelper.WaitForServerReady(30)

			// Verify we got the tagged version (1 server)
			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())

			var response map[string]interface{}
			err = json.Unmarshal(body, &response)
			Expect(err).NotTo(HaveOccurred())

			servers, ok := response["servers"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(servers).To(HaveLen(1)) // Tagged version has 1 server
		})
	})

	Context("Nested File Paths", func() {
		It("should load registry from nested directory in repository", func() {
			Skip("Server integration pending - demonstrates nested path testing")

			testRepo = gitHelper.CreateRepository("nested-path-repo")

			// Commit registry to nested path
			testServers := helpers.CreateOriginalTestServers()
			gitHelper.CommitRegistryData(testRepo, "configs/prod/registry.json", testServers, "Nested registry")

			configFile = helpers.WriteConfigYAML(tempDir, "nested-registry", "git", map[string]string{
				"url":         testRepo.CloneURL,
				"path":        "configs/prod/registry.json",
				"branch":      "main",
				"storagePath": storageDir,
			})

			serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8085)
			serverHelper.WaitForServerReady(30)

			resp, err := serverHelper.GetServers()
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Context("Automatic Sync", func() {
		It("should automatically re-sync when Git repository is updated", func() {
			Skip("Automatic sync testing - demonstrates periodic sync validation")

			testRepo = gitHelper.CreateRepository("auto-sync-repo")
			initialServers := helpers.CreateOriginalTestServers()
			gitHelper.CommitRegistryData(testRepo, "registry.json", initialServers, "Initial commit")

			// Start server with short sync interval
			configFile = helpers.WriteConfigYAML(tempDir, "auto-sync-registry", "git", map[string]string{
				"url":          testRepo.CloneURL,
				"path":         "registry.json",
				"branch":       "main",
				"storagePath":  storageDir,
				"syncInterval": "5s",
			})

			serverHelper = helpers.NewServerTestHelper(ctx, configFile, 8086)
			serverHelper.WaitForServerReady(30)

			// Update the repository
			updatedServers := helpers.CreateUpdatedTestServers()
			gitHelper.UpdateRegistryData(testRepo, "registry.json", updatedServers, "Update registry")

			// Wait for automatic re-sync (should happen within sync interval)
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
			}, 15).Should(Equal(2), "Should detect repository update and re-sync")
		})
	})
})
