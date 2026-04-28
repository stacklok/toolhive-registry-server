# Release Process

This document describes the release process for the ToolHive Registry Server.

## Overview

The release process is fully automated using the [releaseo](https://github.com/stacklok/releaseo) GitHub Action:

1. A maintainer triggers the `Create Release PR` workflow from GitHub Actions
2. The workflow creates a release PR with version bumps
3. The PR is reviewed and merged
4. GitHub Actions automatically creates the tag and GitHub Release
5. GitHub Actions builds and publishes binaries, container images, and Helm charts

## Prerequisites

### Required GitHub App

A GitHub App must be installed on this repository to mint short-lived
installation tokens for the release workflows. An installation token (rather
than `GITHUB_TOKEN`) is required because events authored by `GITHUB_TOKEN`
cannot trigger downstream workflows.

| Setting | Value |
|---------|-------|
| Repository **variable** `RELEASE_APP_CLIENT_ID` | The GitHub App's Client ID |
| Repository **secret** `RELEASE_APP_PRIVATE_KEY` | The GitHub App's private key (PEM) |
| App repository permissions | `contents: write`, `pull-requests: write` |
| App installation | Must be installed on this repository |

The release workflows use [`actions/create-github-app-token`](https://github.com/actions/create-github-app-token)
to mint a token at the start of each job. The token is automatically scoped
to this repository and expires after one hour, so there is nothing to
rotate manually.

## Creating a Release

### Step 1: Trigger the Release Workflow

You can trigger the release workflow in two ways:

**Option A: GitHub Actions UI**

1. Go to [Actions > Create Release PR](../../actions/workflows/create-release-pr.yml)
2. Click "Run workflow"
3. Select the version bump type (patch, minor, or major)
4. Click "Run workflow"

**Option B: GitHub CLI**

```bash
gh workflow run create-release-pr.yml -f bump_type=patch
```

Replace `patch` with `minor` or `major` as needed:
- **major** - Breaking changes (1.0.0 → 2.0.0)
- **minor** - New features, backward compatible (1.0.0 → 1.1.0)
- **patch** - Bug fixes, backward compatible (1.0.0 → 1.0.1)

The workflow will:

1. Calculate the new version based on the bump type
2. Create a release branch (`release/v{version}`)
3. Update all version files configured in the workflow
4. Regenerate Helm chart documentation via helm-docs
5. Commit and push the branch
6. Create a pull request

### Step 2: Review and Merge the PR

1. Review the PR to ensure:
   - Version bump type is correct
   - All CI checks pass
   - Only version-related files were modified
2. Merge the PR to `main`

### Step 3: Automatic Release (No Action Required)

After merging, the following happens automatically:

1. **`create-release-tag.yml`** workflow triggers (on VERSION file change):
   - Verifies the commit message matches the release pattern
   - Verifies only release files were changed
   - Creates git tag `v{version}`
   - Creates GitHub Release

2. **`releaser.yml`** workflow triggers (on release published):
   - Verifies tag matches VERSION file
   - Builds binaries for multiple platforms
   - Signs artifacts with Cosign
   - Publishes to GitHub Release

3. **`image-build-and-publish.yml`** workflow:
   - Builds container images
   - Pushes to GHCR
   - Signs images with Cosign

## Release Flow Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                     GITHUB ACTIONS                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   create-release-pr.yml (triggered manually via workflow_dispatch)│
│        │                                                         │
│        ├─ Uses stacklok/releaseo action                         │
│        ├─ Calculates new version from bump type                 │
│        ├─ Creates branch: release/v{version}                    │
│        ├─ Updates configured version files                      │
│        ├─ Runs helm-docs to update chart README                 │
│        ├─ Commits: "Release v{version}"                         │
│        └─ Creates PR                                            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                     PR Review & Merge
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     GITHUB ACTIONS                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   create-release-tag.yml (triggered by VERSION change)          │
│        │                                                         │
│        ├─ Verify commit message pattern                         │
│        ├─ Verify only release files changed                     │
│        ├─ Create tag v{version}                                 │
│        └─ Create GitHub Release                                 │
│                              │                                   │
│                              ▼                                   │
│   releaser.yml (triggered by release:published)                 │
│        │                                                         │
│        ├─ verify-release (VERSION must match tag)               │
│        ├─ compute-build-flags                                   │
│        ├─ release-binaries (GoReleaser + Cosign)                │
│        ├─ image-build-and-push                                  │
│        └─ update-docs-website                                   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Security Verifications

The release process includes multiple security checks:

### In `create-release-tag.yml`:

1. **Commit message verification** - Must match pattern `Release v{semver}` or be a merge from `release/v{semver}`
2. **Version match verification** - VERSION file must match version in commit message
3. **File change verification** - Only release-related files can be modified

### In `releaser.yml`:

1. **Tag/VERSION verification** - Release tag must match VERSION file exactly
2. **Binary version verification** - Built binary must report correct version

## Troubleshooting

### Release PR Creation Fails

**Workflow fails to create PR**

Check the workflow logs in GitHub Actions for specific error messages. Common issues:
- Release GitHub App is not installed on the repo, or `RELEASE_APP_CLIENT_ID` variable / `RELEASE_APP_PRIVATE_KEY` secret is missing
- App lacks `contents: write` and/or `pull-requests: write` permission
- Branch `release/v{version}` already exists

**"Branch release/v{version} already exists"**
```bash
# Delete the existing branch remotely
git push origin --delete release/v{version}
```

### Tag Creation Workflow Fails

**"Commit message does not match release pattern"**

The PR was likely squash-merged with a non-standard commit message. The commit message must:
- Start with `Release v{semver}` (squash merge), OR
- Contain `release/v{semver}` (merge commit)

**"Unexpected file changes detected"**

The release PR contained changes to files other than the expected release files. Create a new release PR with only version-related changes.

### Releaser Workflow Fails

**"VERSION MISMATCH"**

The tag version doesn't match the VERSION file. This can happen if:
- Someone manually created a tag with the wrong version
- The VERSION file was modified after the tag was created

Solution: Delete the tag and release, fix the VERSION file, and create a new release.

### Release Not Triggered After Merge

The `create-release-tag.yml` workflow uses a GitHub App installation token (minted via `actions/create-github-app-token`) to create the tag and release. Installation tokens are required because tags pushed under `GITHUB_TOKEN` do not trigger downstream workflows. If releases aren't triggering:

1. Verify the release GitHub App is installed on the repo
2. Verify `RELEASE_APP_CLIENT_ID` repository variable and `RELEASE_APP_PRIVATE_KEY` repository secret are configured
3. Verify the app has `contents: write` permission

## Manual Release (Emergency Only)

In rare cases where automation fails, you can manually create a release:

```bash
# Ensure VERSION file is correct
cat VERSION

# Create and push tag
VERSION=$(cat VERSION)
git tag -a "v${VERSION}" -m "Release v${VERSION}"
git push origin "v${VERSION}"

# Create GitHub release (triggers releaser.yml)
gh release create "v${VERSION}" --title "Release v${VERSION}" --generate-notes
```

**Warning**: Manual releases bypass security verifications. Only use in emergencies.
