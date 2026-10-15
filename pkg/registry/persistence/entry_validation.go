package persistence

import (
	"fmt"
	"time"

	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

// Validate checks a publication input, including its discriminated payload and
// matching name/version. Storage-assigned IDs, latest flags and timestamps must
// be absent. Backends still validate before writing; callers cannot bypass it.
//
//nolint:gocyclo // Three payload unions have independent identity and storage-metadata checks.
func (e Entry) Validate() error {
	if err := e.Kind.Validate(); err != nil {
		return err
	}
	if e.ID != "" || e.SourceID != "" || e.IsLatest || e.Name == "" || e.Version == "" {
		return ErrInvalid
	}
	var snapshot model.Snapshot
	switch e.Kind {
	case ServerKind:
		if e.Server == nil || e.Skill != nil || e.Plugin != nil || e.Server.Name != e.Name || e.Server.Version != e.Version {
			return ErrInvalid
		}
		snapshot.Data.Servers = []model.Server{*e.Server}
	case SkillKind:
		if e.Skill == nil || e.Server != nil || e.Plugin != nil || e.Skill.Name != e.Name || e.Skill.Version != e.Version ||
			e.Skill.ID != "" || e.Skill.IsLatest || !e.Skill.CreatedAt.IsZero() || !e.Skill.UpdatedAt.IsZero() {
			return ErrInvalid
		}
		snapshot.Data.Skills = []thvregistry.Skill{formats.SkillPayload(e.Skill)}
	case PluginKind:
		if e.Plugin == nil || e.Server != nil || e.Skill != nil || e.Plugin.Name != e.Name || e.Plugin.Version != e.Version ||
			e.Plugin.ID != "" || e.Plugin.IsLatest || !e.Plugin.CreatedAt.IsZero() || !e.Plugin.UpdatedAt.IsZero() {
			return ErrInvalid
		}
		snapshot.Data.Plugins = []thvregistry.Plugin{formats.PluginPayload(e.Plugin)}
	}
	return ValidateSnapshot(&snapshot)
}

// ValidateSnapshot validates every version before any write. An empty snapshot
// is a valid deletion of a source's catalog.
//
//nolint:gocyclo // Server/skill/plugin and their related metadata share one validation boundary.
func ValidateSnapshot(s *model.Snapshot) error {
	if s == nil {
		return fmt.Errorf("%w: nil snapshot", ErrInvalid)
	}
	seen := make(map[string]bool)
	for _, v := range s.Data.Servers {
		if name, err := formats.ValidateServerName(v.Name); err != nil || name != v.Name || v.Version == "" || v.Description == "" {
			return fmt.Errorf("%w: invalid server identity or description", ErrInvalid)
		}
		if err := uniqueVersion(seen, ServerKind, v.Name, v.Version); err != nil {
			return err
		}
		packages := map[[3]string]bool{}
		for _, p := range v.Packages {
			key := [3]string{p.RegistryType, p.Identifier, p.Transport.Type}
			if packages[key] {
				return fmt.Errorf("%w: duplicate server package", ErrInvalid)
			}
			packages[key] = true
		}
		remotes := map[[2]string]bool{}
		for _, r := range v.Remotes {
			key := [2]string{r.Type, r.URL}
			if remotes[key] {
				return fmt.Errorf("%w: duplicate server remote", ErrInvalid)
			}
			remotes[key] = true
		}
		icons := map[[3]string]bool{}
		for _, i := range v.Icons {
			theme, mime := "light", ""
			if i.Theme != nil {
				theme = *i.Theme
			}
			if i.MimeType != nil {
				mime = *i.MimeType
			}
			key := [3]string{i.Src, mime, theme}
			if icons[key] {
				return fmt.Errorf("%w: duplicate server icon", ErrInvalid)
			}
			icons[key] = true
		}
	}
	for _, v := range s.Data.Skills {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("%w: invalid skill payload", ErrInvalid)
		}
		if err := uniqueVersion(seen, SkillKind, v.Name, v.Version); err != nil {
			return err
		}
	}
	for _, v := range s.Data.Plugins {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("%w: invalid plugin payload", ErrInvalid)
		}
		if err := uniqueVersion(seen, PluginKind, v.Name, v.Version); err != nil {
			return err
		}
	}
	// Validate the same wire schema as imported registries, without requiring an
	// envelope for a caller-constructed (possibly empty) catalog snapshot.
	check := *s
	if check.Schema == "" {
		check.Schema = "https://example.org/registry.schema.json"
	}
	if check.Version == "" {
		check.Version = "1.0.0"
	}
	if check.Meta.LastUpdated == "" {
		check.Meta.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	}
	if check.Data.Servers == nil {
		check.Data.Servers = []model.Server{}
	}
	if err := check.Validate(); err != nil {
		return fmt.Errorf("%w: invalid snapshot format", ErrInvalid)
	}
	return nil
}

func uniqueVersion(seen map[string]bool, kind EntryKind, name, version string) error {
	key := string(kind) + "\x00" + name + "\x00" + version
	if seen[key] {
		return fmt.Errorf("%w: duplicate entry version", ErrInvalid)
	}
	seen[key] = true
	return nil
}
