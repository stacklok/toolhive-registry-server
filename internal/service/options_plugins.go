package service

// PublishPluginOptions is the options for the PublishPlugin operation
type PublishPluginOptions struct {
	Claims    map[string]any
	JWTClaims map[string]any
}

//nolint:unparam
func (o *PublishPluginOptions) setClaims(claims map[string]any) error {
	o.Claims = claims
	return nil
}

//nolint:unparam
func (o *PublishPluginOptions) setJWTClaims(claims map[string]any) error {
	o.JWTClaims = claims
	return nil
}

// DeletePluginVersionOptions is the options for the DeletePluginVersion operation
type DeletePluginVersionOptions struct {
	Namespace string
	Name      string
	Version   string
	JWTClaims map[string]any
}

//nolint:unparam
func (o *DeletePluginVersionOptions) setName(name string) error {
	o.Name = name
	return nil
}

//nolint:unparam
func (o *DeletePluginVersionOptions) setVersion(version string) error {
	o.Version = version
	return nil
}

//nolint:unparam
func (o *DeletePluginVersionOptions) setJWTClaims(claims map[string]any) error {
	o.JWTClaims = claims
	return nil
}
