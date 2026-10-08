package persistence

import (
	"fmt"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

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
