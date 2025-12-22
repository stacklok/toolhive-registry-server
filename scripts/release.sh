#!/bin/bash
#
# Release PR Creator
#
# This script creates a release PR by:
# 1. Prompting for version bump type (major/minor/patch)
# 2. Creating a release branch
# 3. Updating VERSION file (and Helm chart if present)
# 4. Creating a PR via gh CLI
#
# Usage: ./scripts/release.sh
#
# Environment variables (optional):
#   VERSION_FILE  - Path to VERSION file (default: VERSION)
#   CHART_PATH    - Path to Helm chart (default: deploy/charts/toolhive-registry-server)

set -e

# Configuration (can be overridden by environment variables)
VERSION_FILE="${VERSION_FILE:-VERSION}"
CHART_PATH="${CHART_PATH:-deploy/charts/toolhive-registry-server}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Release PR Creator ===${NC}"
echo ""

# Check for required tools
if ! command -v gh &> /dev/null; then
  echo -e "${RED}Error: gh CLI is not installed${NC}"
  echo "Install it from: https://cli.github.com/"
  exit 1
fi

if ! command -v yq &> /dev/null; then
  echo -e "${RED}Error: yq is not installed${NC}"
  echo "Install it with: brew install yq"
  echo "Or see: https://github.com/mikefarah/yq#install"
  exit 1
fi

# Check gh auth status
if ! gh auth status &> /dev/null; then
  echo -e "${RED}Error: Not authenticated with gh CLI${NC}"
  echo "Run: gh auth login"
  exit 1
fi

# Check for clean working directory
if [ -n "$(git status --porcelain)" ]; then
  echo -e "${RED}Error: Working directory is not clean${NC}"
  echo "Please commit or stash your changes first."
  exit 1
fi

# Ensure we're on main branch
CURRENT_BRANCH=$(git branch --show-current)
if [ "$CURRENT_BRANCH" != "main" ]; then
  echo -e "${YELLOW}Warning: Not on main branch (currently on: $CURRENT_BRANCH)${NC}"
  read -p "Continue anyway? [y/N]: " CONTINUE
  if [[ ! "$CONTINUE" =~ ^[Yy]$ ]]; then
    echo -e "${YELLOW}Cancelled${NC}"
    exit 0
  fi
fi

# Pull latest changes
echo "Pulling latest changes..."
git pull --quiet

# Read current version
if [ ! -f "$VERSION_FILE" ]; then
  echo -e "${RED}Error: $VERSION_FILE file not found${NC}"
  exit 1
fi

CURRENT_VERSION=$(cat "$VERSION_FILE" | tr -d '[:space:]')
echo -e "Current version: ${GREEN}${CURRENT_VERSION}${NC}"
echo ""

# Parse current version using cut (more portable than BASH_REMATCH)
if ! echo "$CURRENT_VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo -e "${RED}Error: Invalid version format in $VERSION_FILE: $CURRENT_VERSION${NC}"
  echo "Expected format: MAJOR.MINOR.PATCH (e.g., 1.2.3)"
  exit 1
fi

MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
MINOR=$(echo "$CURRENT_VERSION" | cut -d. -f2)
PATCH=$(echo "$CURRENT_VERSION" | cut -d. -f3)

# Ask user for bump type
echo "What type of release is this?"
echo ""
echo -e "  ${YELLOW}1)${NC} major  - Breaking changes (${CURRENT_VERSION} → $((MAJOR + 1)).0.0)"
echo -e "  ${YELLOW}2)${NC} minor  - New features, backward compatible (${CURRENT_VERSION} → ${MAJOR}.$((MINOR + 1)).0)"
echo -e "  ${YELLOW}3)${NC} patch  - Bug fixes, backward compatible (${CURRENT_VERSION} → ${MAJOR}.${MINOR}.$((PATCH + 1)))"
echo ""
read -p "Enter choice [1/2/3]: " CHOICE

case $CHOICE in
  1|major)
    NEW_VERSION="$((MAJOR + 1)).0.0"
    BUMP_TYPE="major"
    ;;
  2|minor)
    NEW_VERSION="${MAJOR}.$((MINOR + 1)).0"
    BUMP_TYPE="minor"
    ;;
  3|patch)
    NEW_VERSION="${MAJOR}.${MINOR}.$((PATCH + 1))"
    BUMP_TYPE="patch"
    ;;
  *)
    echo -e "${RED}Invalid choice: $CHOICE${NC}"
    exit 1
    ;;
esac

echo ""
echo -e "Bump type: ${YELLOW}${BUMP_TYPE}${NC}"
echo -e "New version: ${GREEN}${NEW_VERSION}${NC}"

# Check if release branch already exists
BRANCH_NAME="release/v${NEW_VERSION}"
if git rev-parse --verify "$BRANCH_NAME" &> /dev/null; then
  echo -e "${RED}Error: Branch $BRANCH_NAME already exists${NC}"
  echo "Delete it first with: git branch -D $BRANCH_NAME"
  exit 1
fi

# Check if tag already exists
TAG="v${NEW_VERSION}"
if git rev-parse "$TAG" &> /dev/null; then
  echo -e "${RED}Error: Tag $TAG already exists${NC}"
  echo "This version has already been released."
  exit 1
fi

# Confirm with user
echo ""
echo -e "Ready to create release PR for: ${GREEN}v${NEW_VERSION}${NC}"
echo ""
echo "This will:"
echo "  1. Create branch: $BRANCH_NAME"
echo "  2. Update VERSION to: $NEW_VERSION"
if [ -f "$CHART_PATH/Chart.yaml" ]; then
  echo "  3. Update Chart.yaml version and appVersion to: $NEW_VERSION"
  echo "  4. Update values.yaml image.tag to: $NEW_VERSION"
  echo "  5. Commit and push the branch"
  echo "  6. Create a pull request"
else
  echo "  3. Commit and push the branch"
  echo "  4. Create a pull request"
fi
echo ""
echo -n "Proceed? [y/N]: "
read CONFIRM
if [[ ! "$CONFIRM" =~ ^[Yy]$ ]]; then
  echo -e "${YELLOW}Cancelled${NC}"
  exit 0
fi

# Create and checkout release branch
echo ""
echo "Creating branch $BRANCH_NAME..."
git checkout -b "$BRANCH_NAME"

# Update VERSION file
echo "Updating VERSION file..."
echo "$NEW_VERSION" > "$VERSION_FILE"

# Update Chart.yaml (if exists)
CHART_FILE="${CHART_PATH}/Chart.yaml"
if [ -f "$CHART_FILE" ]; then
  echo "Updating Chart.yaml..."
  yq -i ".version = \"$NEW_VERSION\" | .appVersion = \"$NEW_VERSION\"" "$CHART_FILE"
fi

# Update values.yaml (if exists)
VALUES_FILE="${CHART_PATH}/values.yaml"
if [ -f "$VALUES_FILE" ]; then
  echo "Updating values.yaml..."
  # Use sed to update only the image.tag line, preserving file formatting
  sed -i '' "s/^\\(  tag: \\)\".*\"/\\1\"$NEW_VERSION\"/" "$VALUES_FILE"
fi

# Commit changes
echo "Committing changes..."
git add "$VERSION_FILE"
[ -f "$CHART_FILE" ] && git add "$CHART_FILE"
[ -f "$VALUES_FILE" ] && git add "$VALUES_FILE"
git commit -m "Release v${NEW_VERSION}"

# Push branch
echo "Pushing branch to origin..."
git push -u origin "$BRANCH_NAME"

# Create PR body
PR_BODY="## Release v${NEW_VERSION}

### Changes
- Updated \`VERSION\` file to \`${NEW_VERSION}\`"

if [ -f "$CHART_FILE" ]; then
  PR_BODY="${PR_BODY}
- Updated \`Chart.yaml\` version to \`${NEW_VERSION}\`
- Updated \`Chart.yaml\` appVersion to \`${NEW_VERSION}\`
- Updated \`values.yaml\` image.tag to \`${NEW_VERSION}\`"
fi

PR_BODY="${PR_BODY}

### Release Type
**${BUMP_TYPE}** release

### Next Steps
1. Review this PR
2. Merge to main
3. The \`create-release-tag\` workflow will automatically:
   - Verify the release
   - Create tag \`v${NEW_VERSION}\`
4. The \`releaser\` workflow will then:
   - Build and publish binaries
   - Build and publish container images"

if [ -f "$CHART_FILE" ]; then
  PR_BODY="${PR_BODY}
   - Package and publish Helm chart to GHCR"
fi

PR_BODY="${PR_BODY}

### Checklist
- [ ] All CI checks pass
- [ ] Version bump type is correct (${BUMP_TYPE})"

# Create PR
echo "Creating pull request..."
PR_URL=$(gh pr create \
  --title "Release v${NEW_VERSION}" \
  --body "$PR_BODY" \
  --label "release" \
  --head "$BRANCH_NAME" \
  --base "main")

# Switch back to main
echo "Switching back to main branch..."
git checkout main

echo ""
echo -e "${GREEN}=== Success ===${NC}"
echo -e "Release PR created: ${BLUE}${PR_URL}${NC}"
echo ""
echo "Next steps:"
echo "  1. Review the PR"
echo "  2. Merge when ready"
echo "  3. The release will be created automatically"
