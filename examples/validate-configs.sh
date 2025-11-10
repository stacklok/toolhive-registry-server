#!/bin/bash
# Validate example configuration files

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Validating example configurations..."
echo

for config in "$SCRIPT_DIR"/config-*.yaml; do
    echo "Checking $config..."
    
    # Basic YAML syntax check using yq (if available) or python
    if command -v yq &> /dev/null; then
        if yq eval '.' "$config" > /dev/null 2>&1; then
            echo "  ✓ Valid YAML syntax"
        else
            echo "  ✗ Invalid YAML syntax"
            exit 1
        fi
    elif command -v python3 &> /dev/null; then
        if python3 -c "import yaml; yaml.safe_load(open('$config'))" 2>/dev/null; then
            echo "  ✓ Valid YAML syntax"
        else
            echo "  ✗ Invalid YAML syntax"
            exit 1
        fi
    else
        echo "  ⚠ No YAML validator found (install yq or python3+pyyaml)"
    fi
    
    # Check required fields
    if grep -q "source:" "$config" && \
       grep -q "type:" "$config" && \
       grep -q "format:" "$config" && \
       grep -q "syncPolicy:" "$config" && \
       grep -q "interval:" "$config"; then
        echo "  ✓ Required fields present"
    else
        echo "  ✗ Missing required fields"
        exit 1
    fi
    
    echo
done

echo "✓ All configurations validated successfully!"
