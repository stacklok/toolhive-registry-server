# Release Process

This document describes the release process for the ToolHive Registry Server.

## Overview

The release process is semi-automated:

1. A maintainer runs `task release` locally to create a release PR
2. The PR is reviewed and merged
3. GitHub Actions automatically creates the tag and GitHub Release
4. GitHub Actions builds and publishes binaries, container images, and Helm charts

## Prerequisites

### Required Tools

- [Task](https://taskfile.dev/) - Task runner
- [yq](https://github.com/mikefarah/yq) - YAML processor
- [gh](https://cli.github.com/) - GitHub CLI (authenticated)

### Required Secrets

The following repository secret must be configured:

| Secret | Description |
|--------|-------------|
| `RELEASE_TOKEN` | Personal Access Token with `contents: write` scope. Required because `GITHUB_TOKEN` cannot trigger other workflows. |

> **Warning: Token Expiry**
>
> The `RELEASE_TOKEN` PAT has a **90-day expiry**. Set a calendar reminder to rotate it before expiration. When the token expires, releases will fail at the tag creation step.
>
> To rotate:
> 1. Create a new PAT at https://github.com/settings/tokens with `contents: write` scope
> 2. Update the `RELEASE_TOKEN` repository secret
> 3. Delete the old PAT

## Creating a Release

### Step 1: Run the Release Task

From the repository root on the `main` branch:

```bash
task release
```

This interactive script will:

1. Display the current version from the `VERSION` file
2. Prompt you to select a version bump type:
   - **major** - Breaking changes (1.0.0 → 2.0.0)
   - **minor** - New features, backward compatible (1.0.0 → 1.1.0)
   - **patch** - Bug fixes, backward compatible (1.0.0 → 1.0.1)
3. Create a release branch (`release/v{version}`)
4. Update version files:
   - `VERSION`
   - `deploy/charts/toolhive-registry-server/Chart.yaml` (if exists)
   - `deploy/charts/toolhive-registry-server/values.yaml` (if exists)
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
│                        LOCAL                                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   task release                                                   │
│        │                                                         │
│        ├─ Prompts for version bump type                         │
│        ├─ Creates branch: release/v{version}                    │
│        ├─ Updates VERSION, Chart.yaml, values.yaml              │
│        ├─ Commits: "Release v{version}"                         │
│        ├─ Pushes branch                                         │
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

## Version Files

The release process updates these files:

| File | Field(s) Updated |
|------|------------------|
| `VERSION` | Contains the version number (e.g., `1.0.0`) |
| `deploy/charts/*/Chart.yaml` | `version` and `appVersion` fields |
| `deploy/charts/*/values.yaml` | `image.tag` field |

## Security Verifications

The release process includes multiple security checks:

### In `create-release-tag.yml`:

1. **Commit message verification** - Must match pattern `Release v{semver}` or be a merge from `release/v{semver}`
2. **Version match verification** - VERSION file must match version in commit message
3. **File change verification** - Only VERSION and Helm chart files can be modified

### In `releaser.yml`:

1. **Tag/VERSION verification** - Release tag must match VERSION file exactly
2. **Binary version verification** - Built binary must report correct version

## Troubleshooting

### Release PR Creation Fails

**"Error: yq is not installed"**
```bash
# macOS
brew install yq

# Other platforms
# See: https://github.com/mikefarah/yq#install
```

**"Error: gh CLI is not installed"**
```bash
# macOS
brew install gh

# Then authenticate
gh auth login
```

**"Error: Branch release/v{version} already exists"**
```bash
# Delete the existing branch
git branch -D release/v{version}
git push origin --delete release/v{version}
```

### Tag Creation Workflow Fails

**"Commit message does not match release pattern"**

The PR was likely squash-merged with a non-standard commit message. The commit message must:
- Start with `Release v{semver}` (squash merge), OR
- Contain `release/v{semver}` (merge commit)

**"Unexpected file changes detected"**

The release PR contained changes to files other than:
- `VERSION`
- `deploy/charts/*/Chart.yaml`
- `deploy/charts/*/values.yaml`

Create a new release PR with only version-related changes.

### Releaser Workflow Fails

**"VERSION MISMATCH"**

The tag version doesn't match the VERSION file. This can happen if:
- Someone manually created a tag with the wrong version
- The VERSION file was modified after the tag was created

Solution: Delete the tag and release, fix the VERSION file, and create a new release.

### Release Not Triggered After Merge

The `create-release-tag.yml` workflow uses a Personal Access Token (`RELEASE_TOKEN`) to create the tag and release. If releases aren't triggering:

1. Verify `RELEASE_TOKEN` secret is configured
2. Verify the PAT has `contents: write` scope
3. Verify the PAT hasn't expired

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
