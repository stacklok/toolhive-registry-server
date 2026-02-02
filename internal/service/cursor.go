// Package service provides shared service utilities for the registry server.
package service

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// CursorSeparator is the delimiter used to separate fields in the cursor.
// We use comma because it does not appear in:
//   - server names: [a-zA-Z][a-zA-Z0-9_-]*
//   - ISO timestamps: contains colons and hyphens but not commas
//   - semver versions: digits, dots, hyphens, plus signs
const CursorSeparator = ","

// DecodeCursor decodes a base64-encoded cursor string into name and version components.
// The cursor format is: base64(name,version)
// Returns empty strings if the cursor is empty.
func DecodeCursor(cursor string) (name, version string, err error) {
	if cursor == "" {
		return "", "", nil
	}

	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode cursor: %w", err)
	}

	parts := strings.Split(string(decoded), CursorSeparator)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid cursor format: expected 2 fields separated by comma")
	}

	return parts[0], parts[1], nil
}

// EncodeCursor encodes a name and version into a base64 cursor string.
// The cursor format is: base64(name,version)
func EncodeCursor(name, version string) string {
	cursorValue := name + CursorSeparator + version
	return base64.StdEncoding.EncodeToString([]byte(cursorValue))
}
