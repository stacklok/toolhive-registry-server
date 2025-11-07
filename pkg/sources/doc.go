// Package sources provides interfaces and implementations for retrieving
// MCP registry data from various external sources.
//
// The package defines the SourceHandler interface which abstracts the
// process of validating source configurations and fetching registry data
// from external sources such as ConfigMaps, HTTP endpoints, Git repositories,
// or external registries.
//
// Architecture:
//   - SourceHandler: Interface for fetching and validating registry data
//   - StorageManager: Interface for persisting registry data to local storage
//   - SourceDataValidator: Validates and parses registry data in different formats
//   - FetchResult: Strongly-typed result containing Registry instances with metadata
//
// Current implementations:
//   - ConfigMapSourceHandler: Retrieves registry data from Kubernetes ConfigMaps
//     Supports both ToolHive and Upstream registry formats with format validation
//   - GitSourceHandler: Retrieves registry data from Git repositories
//     Supports public repos via HTTPS with branch/tag/commit checkout
//   - APISourceHandler: Retrieves registry data from HTTP/HTTPS endpoints
//     Delegates to format-specific handlers (ToolHiveAPIHandler, UpstreamAPIHandler)
//   - FileStorageManager: Persists Registry data to local file storage for serving
//
// The package provides a factory pattern for creating appropriate
// source handlers based on the source type configuration, and uses
// strongly-typed Registry instances throughout for type safety.
package sources
