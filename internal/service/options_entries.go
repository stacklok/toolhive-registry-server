package service

import "fmt"

// UpdateEntryClaimsOptions is the options for the UpdateEntryClaims operation.
type UpdateEntryClaimsOptions struct {
	EntryType string // "server" or "skill"
	Name      string
	Claims    map[string]any
	JWTClaims map[string]any
}

//nolint:unparam
func (o *UpdateEntryClaimsOptions) setEntryType(entryType string) error {
	switch entryType {
	case "server", "skill":
		o.EntryType = entryType
	default:
		return fmt.Errorf("unsupported entry type: %s", entryType)
	}
	return nil
}

//nolint:unparam
func (o *UpdateEntryClaimsOptions) setName(name string) error {
	o.Name = name
	return nil
}

//nolint:unparam
func (o *UpdateEntryClaimsOptions) setClaims(claims map[string]any) error {
	o.Claims = claims
	return nil
}

//nolint:unparam
func (o *UpdateEntryClaimsOptions) setJWTClaims(claims map[string]any) error {
	o.JWTClaims = claims
	return nil
}
