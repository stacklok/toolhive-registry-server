package service

import "fmt"

// Supported entry types for registry operations.
const (
	EntryTypeServer = "server"
	EntryTypeSkill  = "skill"
)

// ValidateEntryType returns nil if entryType is a supported entry type,
// or an error otherwise. It is the single source of truth for the set of
// valid entry type strings used across the API, options, and service layers.
func ValidateEntryType(entryType string) error {
	switch entryType {
	case EntryTypeServer, EntryTypeSkill:
		return nil
	default:
		return fmt.Errorf("unsupported entry type: must be %q or %q", EntryTypeServer, EntryTypeSkill)
	}
}
