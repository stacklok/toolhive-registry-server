// Package integration provides integration tests for the ToolHive Registry API Server.
// These tests validate the complete server lifecycle including all data source types
// (File, Git, API) and synchronization mechanisms.
//
// The test suite uses Ginkgo v2 for BDD-style testing and includes:
//   - File source tests: Local file loading and validation
//   - Git source tests: Repository cloning, branch/tag checkout, and sync
//   - API source tests: HTTP API fetching, periodic re-sync, and retry logic
//   - Filtering tests: Server search and filtering capabilities
//
// Test helpers provide utilities for:
//   - Dynamic port allocation to avoid conflicts
//   - Mock API server creation
//   - Git repository management
//   - Server lifecycle management with 1-second cache for test responsiveness
package integration
