// Package sources provides interfaces and implementations for retrieving
// MCP registry data from various external sources.
//
// The package defines the RegistryHandler interface which abstracts the
// process of validating registry configurations and fetching registry data
// from external sources such as HTTP endpoints, Git repositories,
// local files, or external registries.
//
// Architecture:
//   - RegistryHandler: Interface for fetching and validating registry data
//   - RegistryDataValidator: Validates and parses registry data in different formats
//   - FetchResult: Strongly-typed result containing Registry instances with metadata
//
// Current implementations:
//   - GitRegistryHandler: Retrieves registry data from Git repositories
//     Supports public repos via HTTPS with branch/tag/commit checkout
//   - APIRegistryHandler: Retrieves registry data from HTTP/HTTPS endpoints
//     Delegates to format-specific handlers (ToolHiveAPIHandler, UpstreamAPIHandler)
//   - FileRegistryHandler: Retrieves registry data from local filesystem
//     Supports both absolute and relative file paths for development and production
//
// The package provides a factory pattern for creating appropriate
// registry handlers based on the registry type configuration, and uses
// strongly-typed Registry instances throughout for type safety.
package sources
