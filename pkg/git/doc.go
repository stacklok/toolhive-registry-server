// Package git provides Git repository operations for registry sources.
//
// This package implements a thin wrapper around the go-git library to enable
// registry resources to fetch registry data directly from Git repositories.
// It supports cloning repositories, checking out specific branches/tags/commits,
// and retrieving file contents from the repository.
//
// Key Components:
//
// # Client Interface
//
// The Client interface defines the core Git operations:
//   - Clone: Clone public repositories to in-memory filesystem
//   - GetFileContent: Retrieve specific files from repositories
//   - Cleanup: Clean up in-memory repository resources
//
// Repository information including commit hashes is tracked via the
// RepositoryInfo struct which is populated during clone operations.
//
// # Example Usage
//
//	client := git.NewDefaultGitClient()
//	config := &git.CloneConfig{
//	    URL:       "https://github.com/example/registry.git",
//	    Branch:    "main",
//	    Directory: "/tmp/repo",
//	}
//
//	repoInfo, err := client.Clone(ctx, config)
//	if err != nil {
//	    return err
//	}
//	defer client.Cleanup(repoInfo)
//
//	content, err := client.GetFileContent(repoInfo, "registry.json")
//	if err != nil {
//	    return err
//	}
//
// # Security Considerations
//
// This package is designed to be used within a Kubernetes operator environment
// where Git repositories contain MCP server registry data. Security features include:
//   - In-memory filesystem operations (no disk access)
//   - Size limits on cloned repositories (max files and total size)
//   - Shallow clones by default for efficiency
//
// Future security hardening may include:
//   - Repository URL validation to prevent SSRF attacks
//   - Additional resource limits and timeouts
//   - Secure credential management via Kubernetes secrets for private repos
//
// # Implementation Details
//
// Current implementation uses:
//   - In-memory filesystems (go-billy memfs) for all Git operations
//   - LimitedFs wrapper to enforce size constraints (10k files, 100MB total)
//   - Shallow clones (depth=1) for branch/tag checkouts
//   - Full clones only when specific commits are requested
//   - Explicit memory cleanup via Cleanup() method with GC hints
//
// Supported features:
//   - Public repository access via HTTPS
//   - Branch, tag, and commit checkout
//   - File content retrieval from any path in the repository
//
// Planned features:
//   - Authentication for private repositories
//   - Webhook support for immediate sync triggers
//   - Git LFS support for large files
package git
