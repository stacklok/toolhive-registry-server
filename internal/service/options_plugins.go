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

// ListPluginsOptions is the options for the ListPlugins and ListPluginVersions
// operations.
type ListPluginsOptions struct {
	RegistryName string
	Namespace    string
	Name         *string
	Version      *string
	Search       *string
	Limit        int
	Cursor       *string
	Claims       map[string]any
}

//nolint:unparam
func (o *ListPluginsOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
}

//nolint:unparam
func (o *ListPluginsOptions) setNamespace(namespace string) error {
	o.Namespace = namespace
	return nil
}

//nolint:unparam
func (o *ListPluginsOptions) setName(name string) error {
	o.Name = &name
	return nil
}

//nolint:unparam
func (o *ListPluginsOptions) setVersion(version string) error {
	o.Version = &version
	return nil
}

//nolint:unparam
func (o *ListPluginsOptions) setSearch(search string) error {
	o.Search = &search
	return nil
}

//nolint:unparam
func (o *ListPluginsOptions) setLimit(limit int) error {
	o.Limit = limit
	return nil
}

//nolint:unparam
func (o *ListPluginsOptions) setCursor(cursor string) error {
	o.Cursor = &cursor
	return nil
}

//nolint:unparam
func (o *ListPluginsOptions) setClaims(claims map[string]any) error {
	o.Claims = claims
	return nil
}

// GetPluginVersionOptions is the options for the GetPluginVersion operation.
type GetPluginVersionOptions struct {
	RegistryName string
	SourceName   string
	Namespace    string
	Name         string
	Version      string
	Claims       map[string]any
}

//nolint:unparam
func (o *GetPluginVersionOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
}

//nolint:unparam
func (o *GetPluginVersionOptions) setNamespace(namespace string) error {
	o.Namespace = namespace
	return nil
}

//nolint:unparam
func (o *GetPluginVersionOptions) setName(name string) error {
	o.Name = name
	return nil
}

//nolint:unparam
func (o *GetPluginVersionOptions) setVersion(version string) error {
	o.Version = version
	return nil
}

//nolint:unparam
func (o *GetPluginVersionOptions) setClaims(claims map[string]any) error {
	o.Claims = claims
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
func (o *DeletePluginVersionOptions) setNamespace(namespace string) error {
	o.Namespace = namespace
	return nil
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
