# Contributing to ToolHive Registry Server <!-- omit from toc -->

First off, thank you for taking the time to contribute to the ToolHive Registry Server! :+1: :tada:

The ToolHive Registry Server is a community-driven project with maintainers from multiple organizations.
It is released under the Apache 2.0 license. If you would like to
contribute something or want to hack on the code, this document should help you
get started. You can find some hints for starting development in the ToolHive Registry Server's
[README](https://github.com/stacklok/toolhive-registry-server/blob/main/README.md).

## Table of contents <!-- omit from toc -->

- [Code of conduct](#code-of-conduct)
- [Reporting security vulnerabilities](#reporting-security-vulnerabilities)
- [How to contribute](#how-to-contribute)
  - [Using GitHub Issues](#using-github-issues)
  - [Not sure how to start contributing?](#not-sure-how-to-start-contributing)
  - [Pull request process](#pull-request-process)
  - [Contributing to docs](#contributing-to-docs)
  - [Commit message guidelines](#commit-message-guidelines)

## Code of conduct

This project adheres to the
[Contributor Covenant](https://github.com/stacklok/toolhive-registry-server/blob/main/CODE_OF_CONDUCT.md)
code of conduct. By participating, you are expected to uphold this code. Please
report unacceptable behavior to
[code-of-conduct@stacklok.dev](mailto:code-of-conduct@stacklok.dev).

## Reporting security vulnerabilities

If you think you have found a security vulnerability in the ToolHive Registry Server please DO NOT
disclose it publicly until we've had a chance to fix it. Please don't report
security vulnerabilities using GitHub issues; instead, please follow this
[process](https://github.com/stacklok/toolhive-registry-server/blob/main/SECURITY.md)

## How to contribute

### Using GitHub Issues

We use GitHub issues to track bugs and enhancements. If you have a general usage
question, please ask in
[ToolHive's discussion forum](https://discord.gg/stacklok).

If you are reporting a bug, please help to speed up problem diagnosis by
providing as much information as possible. Ideally, that would include a small
sample project that reproduces the problem.

### Not sure how to start contributing?

PRs to resolve existing issues are greatly appreciated, and issues labeled as
["good first issue"](https://github.com/stacklok/toolhive-registry-server/issues?q=is%3Aopen+is%3Aissue+label%3A%22good+first+issue%22)
are a great place to start!

### Pull request process

- All commits must include a Signed-off-by trailer at the end of each commit
  message to indicate that the contributor agrees to the Developer Certificate
  of Origin.
- Create an issue outlining the fix or feature.
- Fork the ToolHive Registry Server repository to your own GitHub account and clone it locally.
- Hack on your changes.
- Correctly format your commit messages, see
  [Commit message guidelines](#commit-message-guidelines) below.
- Open a PR by ensuring the title and its description reflect the content of the
  PR.
- Ensure that CI passes, if it fails, fix the failures.
- Every pull request requires a review from the core ToolHive Registry Server team before
  merging.
- Once approved, all of your commits will be squashed into a single commit with
  your PR title.

### Contributing to docs

The ToolHive Registry Server documentation is maintained in the
[docs-website](https://github.com/stacklok/docs-website) repository. If you want
to contribute to the documentation, please open a PR in that repo.

Please review the README and
[STYLE-GUIDE](https://github.com/stacklok/docs-website/blob/main/STYLE-GUIDE.md)
in the docs-website repository for more information on how to contribute to the
documentation.

### Development setup

```bash
# Clone the repository
git clone https://github.com/stacklok/toolhive-registry-server.git
cd toolhive-registry-server

# Install dependencies
go mod download

# Run all checks (lint + test + build)
task all
```

#### Prerequisites

- Go 1.26 or later
- [Task](https://taskfile.dev) for build automation
- PostgreSQL 16+ (for database-backed tests)

#### Build commands

```bash
task build          # Build the binary
task lint-fix       # Run linting (auto-fix)
task test           # Run tests
task gen            # Generate mocks (run before testing)
task build-image    # Build container image
task docs           # Regenerate documentation
task all            # Lint + test + build
task migrate-up CONFIG=examples/config-database-dev.yaml  # Run database migrations
```

#### Testing

```bash
# Generate mocks before testing
task gen

# Run all tests
task test

# Run specific test
go test ./internal/service/... -v
```

The project uses table-driven tests with mocks generated via `go.uber.org/mock`.

#### Architecture

The server follows clean architecture with clear separation of concerns:

```text
┌─────────────────────────────────────────────┐
│  API Layer (Chi Router)                     │
│  ├─ Registry API v0.1                       │
│  ├─ Admin API v1                            │
│  └─ OAuth/OIDC Middleware                   │
└─────────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────────┐
│  Service Layer (RegistryService)            │
│  └─ PostgreSQL backend                      │
└─────────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────────┐
│  Data Source Layer                          │
│  ├─ Git Handler                             │
│  ├─ API Handler                             │
│  ├─ File Handler                            │
│  ├─ Managed Handler                         │
│  └─ Kubernetes Handler                      │
└─────────────────────────────────────────────┘
                    │
┌─────────────────────────────────────────────┐
│  Sync Layer (Background Coordinator)        │
└─────────────────────────────────────────────┘
```

#### Project structure

```text
cmd/thv-registry-api/    # Main application
├── app/                 # CLI commands (serve, migrate, prime-db, version)
└── main.go

internal/                # Internal packages
├── api/                 # HTTP API handlers
├── app/                 # Application builder and startup
├── audit/               # SIEM-compliant audit logging
├── auth/                # OAuth/OIDC authentication
├── authz/               # JWT claim-based authorization
├── config/              # Configuration loading
├── db/                  # Database access (sqlc generated)
├── filtering/           # Registry entry filtering
├── kubernetes/          # Kubernetes reconciler for K8s sources
├── registry/            # Registry data models
├── service/             # Business logic
├── sources/             # Data source handlers (Git, API, File, Managed)
├── sync/                # Background sync coordination
├── telemetry/           # OpenTelemetry integration
└── validators/          # Input validation

database/                # Database schema and queries
├── migrations/          # SQL migrations
└── queries/             # SQL queries for sqlc

examples/                # Example configurations
docs/                    # Documentation
```

#### Development guidelines

- Run `task lint-fix` before committing
- Ensure tests pass with `task test`
- Follow Go standard project layout
- Use mockgen for test mocks (never hand-written)
- Write table-driven tests

### Commit message guidelines

We follow the commit formatting recommendations found on
[Chris Beams' How to Write a Git Commit Message article](https://chris.beams.io/posts/git-commit/):

1. Separate subject from body with a blank line
1. Limit the subject line to 50 characters
1. Capitalize the subject line
1. Do not end the subject line with a period
1. Use the imperative mood in the subject line
1. Use the body to explain what and why vs. how
