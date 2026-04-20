package service

// UpdateEntryClaimsOptions is the options for the UpdateEntryClaims operation.
type UpdateEntryClaimsOptions struct {
	EntryType string // EntryTypeServer or EntryTypeSkill
	Name      string
	Claims    map[string]any
	JWTClaims map[string]any
}

func (o *UpdateEntryClaimsOptions) setEntryType(entryType string) error {
	if err := ValidateEntryType(entryType); err != nil {
		return err
	}
	o.EntryType = entryType
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
