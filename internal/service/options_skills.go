package service

// PublishSkillOptions is the options for the PublishSkill operation
type PublishSkillOptions struct {
	Claims    map[string]any
	JWTClaims map[string]any
}

//nolint:unparam
func (o *PublishSkillOptions) setClaims(claims map[string]any) error {
	o.Claims = claims
	return nil
}

//nolint:unparam
func (o *PublishSkillOptions) setJWTClaims(claims map[string]any) error {
	o.JWTClaims = claims
	return nil
}

// DeleteSkillVersionOptions is the options for the DeleteSkillVersion operation
type DeleteSkillVersionOptions struct {
	Namespace string
	Name      string
	Version   string
	JWTClaims map[string]any
}

//nolint:unparam
func (o *DeleteSkillVersionOptions) setName(name string) error {
	o.Name = name
	return nil
}

//nolint:unparam
func (o *DeleteSkillVersionOptions) setVersion(version string) error {
	o.Version = version
	return nil
}

//nolint:unparam
func (o *DeleteSkillVersionOptions) setJWTClaims(claims map[string]any) error {
	o.JWTClaims = claims
	return nil
}
