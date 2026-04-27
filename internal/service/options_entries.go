package service

import "fmt"

// UpdateEntryClaimsOptions is the options for the UpdateEntryClaims operation.
type UpdateEntryClaimsOptions struct {
	EntryType string // EntryTypeServer or EntryTypeSkill
	Name      string
	Claims    map[string]any
	JWTClaims map[string]any
}

func (o *UpdateEntryClaimsOptions) setEntryType(entryType string) error {
	switch entryType {
	case EntryTypeServer, EntryTypeSkill:
		o.EntryType = entryType
		return nil
	default:
		return fmt.Errorf("%w: must be %q or %q", ErrInvalidEntryType, EntryTypeServer, EntryTypeSkill)
	}
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

// GetEntryClaimsOptions is the options for the GetEntryClaims operation.
type GetEntryClaimsOptions struct {
	EntryType string // EntryTypeServer or EntryTypeSkill
	Name      string
}

func (o *GetEntryClaimsOptions) setEntryType(entryType string) error {
	switch entryType {
	case EntryTypeServer, EntryTypeSkill:
		o.EntryType = entryType
		return nil
	default:
		return fmt.Errorf("%w: must be %q or %q", ErrInvalidEntryType, EntryTypeServer, EntryTypeSkill)
	}
}

//nolint:unparam
func (o *GetEntryClaimsOptions) setName(name string) error {
	o.Name = name
	return nil
}
