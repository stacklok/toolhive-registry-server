// Package service provides shared service utilities for the registry server.
package service

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// CursorSeparator is the delimiter used to separate name and version in the cursor
const CursorSeparator = ":"

// DecodeCursor decodes a base64-encoded cursor string into name and version components.
// The cursor format is: base64(name:version)
// Returns empty strings if the cursor is empty.
func DecodeCursor(cursor string) (name, version string, err error) {
	if cursor == "" {
		return "", "", nil
	}

	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode cursor: %w", err)
	}

	parts := strings.SplitN(string(decoded), CursorSeparator, 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid cursor format: expected name:version")
	}

	return parts[0], parts[1], nil
}

// EncodeCursor encodes a name and version into a base64 cursor string.
// The cursor format is: base64(name:version)
func EncodeCursor(name, version string) string {
	cursorValue := name + CursorSeparator + version
	return base64.StdEncoding.EncodeToString([]byte(cursorValue))
}
