package service

// PublishSkillOptions is the options for the PublishSkill operation
type PublishSkillOptions struct {
	RegistryName string
}

//nolint:unparam
func (o *PublishSkillOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
}

// ListSkillsOptions is the options for the ListSkills and ListSkillVersions
// operations.
type ListSkillsOptions struct {
	RegistryName string
	Namespace    string
	Name         *string
	Version      *string
	Search       *string
	Limit        int
	Cursor       *string
}

//nolint:unparam
func (o *ListSkillsOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
}

//nolint:unparam
func (o *ListSkillsOptions) setNamespace(namespace string) error {
	o.Namespace = namespace
	return nil
}

//nolint:unparam
func (o *ListSkillsOptions) setName(name string) error {
	o.Name = &name
	return nil
}

//nolint:unparam
func (o *ListSkillsOptions) setVersion(version string) error {
	o.Version = &version
	return nil
}

//nolint:unparam
func (o *ListSkillsOptions) setSearch(search string) error {
	o.Search = &search
	return nil
}

//nolint:unparam
func (o *ListSkillsOptions) setLimit(limit int) error {
	o.Limit = limit
	return nil
}

//nolint:unparam
func (o *ListSkillsOptions) setCursor(cursor string) error {
	o.Cursor = &cursor
	return nil
}

// GetSkillVersionOptions is the options for the GetSkillVersion operation.
type GetSkillVersionOptions struct {
	RegistryName string
	Namespace    string
	Name         string
	Version      string
}

//nolint:unparam
func (o *GetSkillVersionOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
}

//nolint:unparam
func (o *GetSkillVersionOptions) setNamespace(namespace string) error {
	o.Namespace = namespace
	return nil
}

//nolint:unparam
func (o *GetSkillVersionOptions) setName(name string) error {
	o.Name = name
	return nil
}

//nolint:unparam
func (o *GetSkillVersionOptions) setVersion(version string) error {
	o.Version = version
	return nil
}

// DeleteSkillVersionOptions is the options for the DeleteSkillVersion operation
type DeleteSkillVersionOptions struct {
	RegistryName string
	Namespace    string
	Name         string
	Version      string
}

//nolint:unparam
func (o *DeleteSkillVersionOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
}

//nolint:unparam
func (o *DeleteSkillVersionOptions) setNamespace(namespace string) error {
	o.Namespace = namespace
	return nil
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
