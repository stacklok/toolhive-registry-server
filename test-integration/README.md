# Integration Tests for ToolHive Registry API Server

This directory contains integration tests for the ToolHive Registry API Server. These tests validate the complete server lifecycle including all data source types and synchronization mechanisms.

## Overview

Integration tests verify end-to-end functionality of the registry server by:
- Starting actual server instances
- Using real dependencies (Git operations, HTTP servers)
- Testing complete request/response cycles
- Validating automatic synchronization behavior
- Testing error handling and recovery scenarios

## Test Framework

These tests use:
- **[Ginkgo v2](https://onsi.github.io/ginkgo/)**: BDD-style test framework
- **[Gomega](https://onsi.github.io/gomega/)**: Matcher/assertion library
- **[httptest](https://pkg.go.dev/net/http/httptest)**: Mock HTTP servers for API source tests

## Directory Structure

```
test-integration/
├── README.md                    # This file
└── registry-api/                # Registry API integration tests
    ├── doc.go                   # Package documentation
    ├── suite_test.go            # Ginkgo suite setup
    ├── file_test.go             # File source tests
    ├── git_test.go              # Git source tests
    ├── api_test.go              # API source tests
    ├── filtering_test.go        # Filtering and search tests
    └── helpers/                 # Test utilities
        ├── factories.go         # Test data factories
        ├── git_helpers.go       # Git repository helpers
        ├── api_helpers.go       # Mock API server builders
        └── server_helpers.go    # Server lifecycle helpers
```

## Running Tests

### Prerequisites

1. **Go 1.23+** installed
2. **Task** build tool: `brew install go-task/tap/go-task`
3. **Ginkgo CLI** (optional, for better output): `go install github.com/onsi/ginkgo/v2/ginkgo@latest`
4. **Git** configured with user name and email

### Install Dependencies

```bash
# Install Ginkgo and other test dependencies
go get github.com/onsi/ginkgo/v2
go get github.com/onsi/gomega
```

### Run All Integration Tests

```bash
task test-integration
```

### Run Specific Test Files

```bash
# File source tests only
go test -v ./test-integration/registry-api -run TestFileSource

# Git source tests only
go test -v ./test-integration/registry-api -run TestGitSource

# Using Ginkgo CLI with labels
ginkgo -v --label-filter=git ./test-integration/registry-api

# Run filtering tests
go test -v ./test-integration/registry-api -run TestFiltering
```

## Test Coverage

### File Source Tests (`file_test.go`)

- ✅ Load registry from local file
- ✅ Handle missing files gracefully
- ✅ Path traversal security validation
- 🔄 File watching and automatic reload (future)

### Git Source Tests (`git_test.go`)

- ✅ Clone and sync from Git repository
- ✅ Sync from specific branch
- ✅ Sync from specific tag
- ✅ Load from nested directory paths
- ✅ Automatic re-sync on repository updates

### API Source Tests (`api_test.go`)

- ✅ Sync from ToolHive API format
- ✅ Handle API endpoint failures
- ✅ Support multiple servers
- ✅ Periodic re-sync from API
- ✅ Retry logic with backoff

### Filtering Tests (`filtering_test.go`)

- ✅ Filter servers by capability
- ✅ Search servers by name
- ✅ Combine multiple filters
- ✅ Handle empty results

## Test Helpers

### Factory Functions (`helpers/factories.go`)

```go
// Create test data
servers := helpers.CreateOriginalTestServers()
complexServers := helpers.CreateComplexTestServers()
names := helpers.NewUniqueNames("test-prefix")
```

### Git Test Helper (`helpers/git_helpers.go`)

```go
gitHelper := helpers.NewGitTestHelper(ctx)
repo := gitHelper.CreateRepository("test-repo")
gitHelper.CommitRegistryData(repo, "registry.json", servers, "Initial commit")
gitHelper.CreateBranch(repo, "development")
gitHelper.CreateTag(repo, "v1.0.0", "Release")
```

### API Mock Server (`helpers/api_helpers.go`)

```go
mockServer := helpers.NewMockAPIServerBuilder().
    WithToolHiveInfo("1.0.0", "2025-01-15", "test", 2).
    WithToolHiveServers(servers).
    Build()
defer mockServer.Close()
```

### Server Helper (`helpers/server_helpers.go`)

```go
// Create server helper with auto-allocated port
serverHelper, err := helpers.NewServerTestHelper(ctx, configPath, storageDir)
Expect(err).NotTo(HaveOccurred())

// Start server
err = serverHelper.StartServer()
Expect(err).NotTo(HaveOccurred())
defer serverHelper.StopServer()

// Wait for server to be ready
serverHelper.WaitForServerReady(30 * time.Second)

// Make API requests
resp, err := serverHelper.GetServers()
```

**Note**: The server uses a **1-second cache duration** in tests (vs 30 seconds in production) to ensure tests can quickly observe data changes after sync operations.

## Writing New Tests

### Example Test Structure

```go
var _ = Describe("My New Feature", Label("feature"), func() {
    var (
        tempDir string
        // ... other variables
    )

    BeforeEach(func() {
        tempDir = createTempDir("my-test-")
        // Setup test environment
    })

    AfterEach(func() {
        cleanupTempDir(tempDir)
        // Cleanup resources
    })

    Context("Specific Scenario", func() {
        It("should behave as expected", func() {
            // Test implementation
            Expect(result).To(Equal(expected))
        })
    })
})
```

### Best Practices

1. **Use descriptive labels**: `Label("git", "sync", "security")`
2. **Clean up resources**: Always use `AfterEach` for cleanup
3. **Use Eventually for async**: `Eventually(func() {...}, timeout, interval).Should(...)`
4. **Skip unimplemented tests**: `Skip("Future enhancement")` with description
5. **Isolate tests**: Each test should be independent
6. **Use helpers**: Leverage existing test helpers for common operations

## Current Status

### ✅ Completed
- Test infrastructure and Ginkgo suite setup
- Test helper utilities for all source types (File, Git, API)
- Server lifecycle integration (starting/stopping with port allocation)
- Complete end-to-end test implementations (22 passing tests)
- Sync coordinator integration tests (periodic sync, retry logic)
- Cache configuration for responsive test behavior
- Taskfile integration for running tests

### 📋 TODO
- CI/CD integration (GitHub Actions)
- Test data validation helpers
- Performance/load testing framework
- Documentation for custom test scenarios
- WebSocket/streaming endpoint tests (when implemented)

## Troubleshooting

### Port Conflicts

Tests use **dynamic port allocation** in the range 8000-9000. Ports are automatically allocated to avoid conflicts. If you still encounter issues:
```bash
# Check for processes using the port range
lsof -i :8000-9000

# Kill conflicting processes if needed
# The test framework will automatically find available ports
```

**Note**: Port allocation is managed by `PortAllocator` in `helpers/server_helpers.go` which tracks allocated ports and finds available ones automatically.

### Git Test Failures

Ensure Git is installed and configured:
```bash
git --version
git config --global user.name "Test User"
git config --global user.email "test@example.com"
```

## References

- [Ginkgo Documentation](https://onsi.github.io/ginkgo/)
- [Gomega Matchers](https://onsi.github.io/gomega/)
- [Go httptest Package](https://pkg.go.dev/net/http/httptest)

## Contributing

When adding new integration tests:

1. Follow the existing test structure and patterns
2. Add appropriate labels for test categorization
3. Update this README with new test coverage
4. Ensure tests can run in CI/CD environments
5. Add helper functions for reusable test logic
6. Document any special setup requirements
