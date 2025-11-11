package integration

import (
	"os"
	"path/filepath"
	"time"

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
				"repository": testRepo.CloneURL,
				"path":       "registry.json",
				"branch":     "main",
			})

			var err error
			serverHelper, err = helpers.NewServerTestHelper(ctx, configFile, storageDir)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should successfully clone and load registry from Git repository", func() {
			// Start server and wait for it to be ready
			err := serverHelper.StartServer()
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = serverHelper.StopServer()
			}()

			serverHelper.WaitForServerReady(10 * time.Second)
			servers := serverHelper.WaitForServers(len(helpers.CreateOriginalTestServers()), 10*time.Second)
			Expect(servers).NotTo(BeEmpty())
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
			// Create config pointing to development branch
			configFile = helpers.WriteConfigYAML(tempDir, "branch-registry", "git", map[string]string{
				"repository":  testRepo.CloneURL,
				"path":        "registry.json",
				"branch":      "development",
				"storagePath": storageDir,
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
			servers := serverHelper.WaitForServers(2, 10*time.Second)
			Expect(servers).NotTo(BeEmpty())
			Expect(servers).To(HaveLen(2)) // Development has 2 servers
		})

		It("should sync from a specific tag", func() {
			configFile = helpers.WriteConfigYAML(tempDir, "tag-registry", "git", map[string]string{
				"repository":  testRepo.CloneURL,
				"path":        "registry.json",
				"tag":         "v1.0.0",
				"storagePath": storageDir,
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
			servers := serverHelper.WaitForServers(1, 10*time.Second)
			Expect(servers).To(HaveLen(1)) // Tagged version has 1 server
		})
	})

	Context("Nested File Paths", func() {
		It("should load registry from nested directory in repository", func() {
			testRepo = gitHelper.CreateRepository("nested-path-repo")

			// Commit registry to nested path
			testServers := helpers.CreateOriginalTestServers()
			gitHelper.CommitRegistryData(testRepo, "configs/prod/registry.json", testServers, "Nested registry")

			configFile = helpers.WriteConfigYAML(tempDir, "nested-registry", "git", map[string]string{
				"repository":  testRepo.CloneURL,
				"path":        "configs/prod/registry.json",
				"branch":      "main",
				"storagePath": storageDir,
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
			servers := serverHelper.WaitForServers(len(helpers.CreateOriginalTestServers()), 10*time.Second)
			Expect(servers).NotTo(BeEmpty())
		})
	})

	Context("Automatic Sync", func() {
		It("should automatically re-sync when Git repository is updated", func() {
			testRepo = gitHelper.CreateRepository("auto-sync-repo")
			initialServers := helpers.CreateOriginalTestServers()
			gitHelper.CommitRegistryData(testRepo, "registry.json", initialServers, "Initial commit")

			// Start server with short sync interval
			configFile = helpers.WriteConfigYAML(tempDir, "auto-sync-registry", "git", map[string]string{
				"repository":   testRepo.CloneURL,
				"path":         "registry.json",
				"branch":       "main",
				"syncInterval": "5s",
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

			// Update the repository
			updatedServers := helpers.CreateUpdatedTestServers()
			gitHelper.UpdateRegistryData(testRepo, "registry.json", updatedServers, "Update registry")

			// Wait for automatic re-sync (sync interval 5s + cache 1s + buffer)
			servers := serverHelper.WaitForServers(2, 8*time.Second)
			Expect(servers).NotTo(BeEmpty())
			Expect(servers).To(HaveLen(2))
		})
	})
})
