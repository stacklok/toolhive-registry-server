#!/bin/bash
# Validate example configuration files using the actual Go config loader
# This ensures examples are validated with the same strict rules as production code

set -e  # Exit on first error

echo "Validating example configurations..."
echo

# Find the script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Check if Go is available
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed or not in PATH"
    echo "Please install Go to validate configuration files"
    exit 1
fi

# Validate each config file using the Go validator
failed=0
total=0

for config in "$SCRIPT_DIR"/config-*.yaml; do
    if [ ! -f "$config" ]; then
        continue
    fi

    total=$((total + 1))
    filename=$(basename "$config")
    echo "Validating $filename..."

    # Run the Go validator
    # Use 'go run' to avoid needing to build the binary first
    if output=$(cd "$PROJECT_ROOT" && go run examples/validate-config.go "$config" 2>&1); then
        echo "$output" | sed 's/^/  /'
        echo
    else
        echo "  ✗ Validation failed:"
        echo "$output" | sed 's/^/    /'
        echo
        failed=$((failed + 1))
    fi
done

# Summary
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
if [ $failed -eq 0 ]; then
    echo "✓ All $total configuration(s) validated successfully!"
    exit 0
else
    echo "✗ $failed of $total configuration(s) failed validation"
    exit 1
fi
