# CLAUDE.md

This file provides AI assistant guidance for working with the ToolHive Registry API Server codebase.

## Project Overview

See [README.md](README.md) for complete project documentation including features, API endpoints, configuration, and deployment.

**Quick Summary**: This is a standards-compliant MCP Registry API server that provides REST endpoints for discovering MCP servers. It supports Git, API, and file-based data sources.

## Essential Build Commands

See [README.md](README.md#development) for complete build and development documentation.

**Most commonly used:**
- `task build` - Build the binary
- `task lint-fix` - **Preferred** linting (auto-fixes issues)
- `task test` - Run tests
- `task gen` - Generate mocks (run before testing)
- `task all` - Lint, test, and build

## Code Architecture

See [README.md](README.md#architecture) for project structure diagram.

### Layered Architecture

The codebase follows clean architecture with three layers:

1. **API Layer** ([api/v1/](cmd/thv-registry-api/api/v1/))
   - Chi router with HTTP handlers
   - Implements MCP Registry API specification
   - Handles request/response serialization

2. **Service Layer** ([internal/service/](cmd/thv-registry-api/internal/service/))
   - Business logic coordinating data providers
   - Provider abstraction with factory pattern

3. **Provider Layer** (backends for registry data)
   - `FileRegistryDataProvider` - Local file backend (reads synced data)
   - `K8sDeploymentProvider` - Queries deployed MCP server instances

### Key Patterns for AI Development

- **Provider Pattern**: When adding new data sources, implement the `RegistryDataProvider` interface
- **Factory Pattern**: Register new providers in `provider_factory.go`
- **Table-Driven Tests**: All tests follow this pattern with comprehensive test cases
- **Mock Generation**: Use `go.uber.org/mock` - never write mocks by hand

## Testing Guidelines

- **Always run `task gen` before testing** to generate mocks
- Tests are located alongside source files (`*_test.go`)
- Use table-driven test pattern for comprehensive coverage
- Mock external dependencies (HTTP clients, file system, Git operations)
- Mocks location: [internal/service/mocks/](cmd/thv-registry-api/internal/service/mocks/)

## AI Assistant Best Practices

### Code Style
- **Public methods first, private methods last** in Go files
- Use interfaces for testability
- Keep layers separated (API → Service → Provider)

### Workflow
1. **Linting**: Always use `task lint-fix` (not `task lint`)
2. **Testing**: Run `task gen` before testing to regenerate mocks
3. **Commits**: See [CONTRIBUTING.md](CONTRIBUTING.md) - **NO conventional commits** (no `feat:`, `fix:`, etc.)

### Common Tasks

**Adding a new API endpoint:**
1. Add handler in [api/v1/routes.go](cmd/thv-registry-api/api/v1/routes.go)
2. Add Swagger annotations
3. Run `task docs`
4. Add tests

**Adding a new data provider:**
1. Implement `RegistryDataProvider` interface
2. Add factory support in [provider_factory.go](cmd/thv-registry-api/internal/service/provider_factory.go)
3. Run `task gen` to create mocks
4. Write table-driven tests

### Key Dependencies
- **Web**: Chi router
- **CLI**: Cobra + Viper
- **Git**: go-git (for Git repository sources)
- **HTTP**: net/http (for API sources)
- **K8s**: client-go (for K8sDeploymentProvider only)
- **Testing**: `go.uber.org/mock`
- **Docs**: Swag/Swagger
