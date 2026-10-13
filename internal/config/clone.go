package config

import (
	"maps"
	"slices"
)

// Clone owns the entire decoded configuration, including loader evidence and
// credentials excluded from JSON. It never rereads files or the environment.
//
//nolint:gocyclo // Keep all owned fields visible in one explicit copy, without reflection or serialization.
func (c *Config) Clone() *Config {
	out := copyPointer(c)
	if out == nil {
		return nil
	}
	out.exposureRetired = copyConfigMap(c.exposureRetired)
	out.exposureEnv = maps.Clone(c.exposureEnv)
	out.Sources = slices.Clone(c.Sources)
	for i := range out.Sources {
		s := &out.Sources[i]
		s.Git = copyPointer(s.Git)
		if s.Git != nil {
			s.Git.Auth = copyPointer(s.Git.Auth)
		}
		s.API = copyPointer(s.API)
		s.File = copyPointer(s.File)
		s.Managed = copyPointer(s.Managed)
		s.Kubernetes = copyPointer(s.Kubernetes)
		if s.Kubernetes != nil {
			s.Kubernetes.Namespaces = slices.Clone(s.Kubernetes.Namespaces)
		}
		s.SyncPolicy = copyPointer(s.SyncPolicy)
		s.Filter = copyPointer(s.Filter)
		if s.Filter != nil {
			s.Filter.Names = copyPointer(s.Filter.Names)
			s.Filter.Tags = copyPointer(s.Filter.Tags)
			for _, f := range []*NameFilterConfig{s.Filter.Names, s.Filter.Tags} {
				if f != nil {
					f.Include = slices.Clone(f.Include)
					f.Exclude = slices.Clone(f.Exclude)
				}
			}
		}
		s.Claims = copyConfigMap(s.Claims)
	}
	out.Registries = slices.Clone(c.Registries)
	for i := range out.Registries {
		out.Registries[i].Sources = slices.Clone(c.Registries[i].Sources)
		out.Registries[i].Claims = copyConfigMap(c.Registries[i].Claims)
	}
	out.Database = copyPointer(c.Database)
	if out.Database != nil {
		out.Database.MaxMetaSize = copyPointer(c.Database.MaxMetaSize)
		out.Database.DynamicAuth = copyPointer(c.Database.DynamicAuth)
		if out.Database.DynamicAuth != nil {
			out.Database.DynamicAuth.AWSRDSIAM = copyPointer(c.Database.DynamicAuth.AWSRDSIAM)
		}
	}
	out.Auth = copyPointer(c.Auth)
	if out.Auth != nil {
		out.Auth.PublicPaths = slices.Clone(c.Auth.PublicPaths)
		out.Auth.OAuth = copyPointer(c.Auth.OAuth)
		if out.Auth.OAuth != nil {
			out.Auth.OAuth.Providers = slices.Clone(c.Auth.OAuth.Providers)
		}
		out.Auth.Authz = copyPointer(c.Auth.Authz)
		if out.Auth.Authz != nil {
			r := &out.Auth.Authz.Roles
			r.SuperAdmin = copyClaimMaps(r.SuperAdmin)
			r.ManageSources = copyClaimMaps(r.ManageSources)
			r.ManageRegistries = copyClaimMaps(r.ManageRegistries)
			r.ManageEntries = copyClaimMaps(r.ManageEntries)
		}
	}
	out.Authorization = copyPointer(c.Authorization)
	if out.Authorization != nil {
		out.Authorization.IdentityMappings = slices.Clone(c.Authorization.IdentityMappings)
		for i := range out.Authorization.IdentityMappings {
			m := &out.Authorization.IdentityMappings[i]
			m.Groups = copyPointer(m.Groups)
			m.Scopes = copyPointer(m.Scopes)
		}
	}
	out.Audit = copyPointer(c.Audit)
	if out.Audit != nil {
		out.Audit.EventTypes = slices.Clone(c.Audit.EventTypes)
		out.Audit.ExcludeEventTypes = slices.Clone(c.Audit.ExcludeEventTypes)
	}
	out.Telemetry = copyPointer(c.Telemetry)
	if out.Telemetry != nil {
		out.Telemetry.Metrics = copyPointer(c.Telemetry.Metrics)
		out.Telemetry.Tracing = copyPointer(c.Telemetry.Tracing)
		if out.Telemetry.Tracing != nil {
			out.Telemetry.Tracing.Sampling = copyPointer(c.Telemetry.Tracing.Sampling)
		}
	}
	return out
}

func copyPointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	out := *p
	return &out
}

func copyClaimMaps(in []map[string]any) []map[string]any {
	out := slices.Clone(in)
	for i := range out {
		out[i] = copyConfigMap(out[i])
	}
	return out
}

func copyConfigMap(in map[string]any) map[string]any {
	out := maps.Clone(in)
	for k, v := range out {
		out[k] = copyConfigValue(v)
	}
	return out
}

// Claims and raw YAML evidence contain scalars, maps and sequences only.
func copyConfigValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		return copyConfigMap(v)
	case []string:
		return slices.Clone(v)
	case []any:
		out := slices.Clone(v)
		for i := range out {
			out[i] = copyConfigValue(out[i])
		}
		return out
	case map[any]any:
		out := maps.Clone(v)
		for k, item := range out {
			out[k] = copyConfigValue(item)
		}
		return out
	default:
		return v
	}
}
