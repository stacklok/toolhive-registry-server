#!/usr/bin/env bash
set -e

# Verify that generated CLI docs are up-to-date.
tmpdir=$(mktemp -d)
go run cmd/help/main.go --dir "$tmpdir"
diff -Naur -I "^  date:" "$tmpdir" docs/cli/

# Generate API docs in temp directory that mimics the final structure
api_tmpdir=$(mktemp -d)
mkdir -p "$api_tmpdir/docs/thv-registry-api"
task docs SWAG_OUTPUT_DIR="$api_tmpdir/docs/thv-registry-api"
# Exclude README.md from diff as it's manually maintained
diff -Naur --exclude="README.md" "$api_tmpdir/docs/thv-registry-api" docs/thv-registry-api/

echo "######################################################################################"
echo "If diffs are found, please run: \`task docs\` to regenerate the docs."
echo "######################################################################################"
