// Package registry provides utilities for working with MCP registry data structures
// and conversions between different registry formats.
//
// This package serves as a central location for registry-related utilities, including
// metadata extraction, format conversions, and test utilities for building registry
// instances in a consistent, maintainable way.
//
// # Core Components
//
//   - Tag Extraction: Utilities for extracting tags from upstream server metadata
//   - Test Utilities: Builder pattern functions for creating test registry data
//   - Format Conversions: Helpers for converting between registry formats
//
// # Tag Extraction
//
// The ExtractTags function extracts tags from the nested metadata structure used
// by the upstream MCP registry format. Tags are stored in the PublisherProvided
// metadata following ToolHive conventions:
//
//	tags := ExtractTags(server)
//	// Returns []string of tags from server.Meta.PublisherProvided["provider"]["metadata"]["tags"]
//
// # Test Utilities
//
// The package provides a comprehensive set of test builder functions following the
// options pattern, enabling creation of realistic test data without manual struct
// construction or JSON unmarshalling:
//
//	// Create a test registry with servers
//	testRegistry := registry.NewTestUpstreamRegistry(
//	    registry.WithVersion("1.0.0"),
//	    registry.WithServers(
//	        registry.NewTestServer("postgres",
//	            registry.WithDescription("PostgreSQL MCP server"),
//	            registry.WithOCIPackage("postgres:latest"),
//	            registry.WithTags("database", "sql"),
//	            registry.WithToolHiveMetadata("tier", "Official"),
//	        ),
//	    ),
//	)
//
// # Registry Builder Options
//
// Registry-level options for customizing the UpstreamRegistry:
//
//   - WithVersion: Set the registry version
//   - WithLastUpdated: Set the last updated timestamp
//   - WithServers: Add one or more servers to the registry
//
// # Server Builder Options
//
// Server-level options for customizing ServerJSON instances:
//
//   - WithDescription: Set server description
//   - WithServerVersion: Set server version
//   - WithOCIPackage: Add an OCI (container) package
//   - WithHTTPPackage: Add an HTTP (remote) package
//   - WithTags: Add tags (stored in provider.metadata.tags)
//   - WithNamespace: Prepend namespace to server name (e.g., "io.test/")
//   - WithMetadata: Add arbitrary top-level metadata
//   - WithToolHiveMetadata: Add ToolHive-specific metadata (stored in provider.toolhive)
//
// # Metadata Structure
//
// The package follows specific conventions for storing metadata in the upstream format:
//
//   - Tags: Meta.PublisherProvided["provider"]["metadata"]["tags"]
//   - ToolHive fields: Meta.PublisherProvided["provider"]["toolhive"]["field_name"]
//   - Custom metadata: Meta.PublisherProvided["custom_key"]
//
// ToolHive-specific fields stored in the toolhive namespace include:
//
//   - tier: Server tier (Official, Community, Enterprise)
//   - status: Server status (Active, Deprecated, Experimental)
//   - transport: Transport protocol (stdio, http, sse, streamable-http)
//   - tools: Array of tool names provided by the server
//   - repository_url: Source repository URL
//   - metadata: Additional metadata (stars, pulls, last_updated)
//   - permissions: Permission requirements
//   - env_vars: Environment variable definitions
//   - args: Command-line arguments
//   - headers: HTTP headers (for remote servers)
//   - oauth_config: OAuth configuration (for remote servers)
//   - provenance: Provenance information
//   - target_port: Target port for HTTP servers
//
// # Usage in Tests
//
// The test utilities are designed to be used across all test files in the project,
// providing consistency and reducing code duplication:
//
//	// In any test file
//	import "github.com/stacklok/toolhive-registry-server/internal/registry"
//
//	func TestMyFunction(t *testing.T) {
//	    reg := registry.NewTestUpstreamRegistry(
//	        registry.WithServers(
//	            registry.NewTestServer("my-server",
//	                registry.WithOCIPackage("my-image:latest"),
//	            ),
//	        ),
//	    )
//	    // Use reg in your test...
//	}
//
// # Design Principles
//
//   - Options Pattern: Provides flexibility and readability in test data construction
//   - Type Safety: Works directly with strongly-typed structs, no JSON marshalling
//   - Discoverability: Builder functions are self-documenting through option names
//   - Consistency: Ensures all tests use the same metadata structure conventions
//   - Maintainability: Changes to data structure only require updates in one place
//
// # Format Compatibility
//
// The utilities support creating test data that is compatible with:
//
//   - Upstream MCP registry format (primary)
//   - ToolHive registry format (via conversion functions)
//   - Filtering operations (name patterns, tags)
//   - API serialization (JSON output)
package registry
