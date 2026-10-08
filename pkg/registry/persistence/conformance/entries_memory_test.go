package conformance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"
	"testing"

	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

var _ persistence.Entries = (*memory)(nil)

func (m *memory) ReplaceSnapshot(ctx context.Context, source persistence.SourceDefinition, snapshot model.Snapshot) error {
	if err := persistence.ValidateSnapshot(&snapshot); err != nil {
		return err
	}
	kind, err := source.Kind()
	if err != nil {
		return err
	}
	if kind == "managed" {
		return persistence.ErrConflict
	}
	return m.transact(ctx, func(n *memory) error {
		s, ok := n.sources[source.Name]
		if !ok {
			return persistence.ErrNotFound
		}
		if s.ID != source.ID {
			return persistence.ErrConflict
		}
		k, _ := s.Kind()
		if k != kind {
			return persistence.ErrConflict
		}
		oldIDs := make(map[string]string)
		for key, v := range n.records {
			if v.SourceID == s.ID {
				oldIDs[key] = v.ID
				delete(n.records, key)
			}
		}
		for _, v := range snapshot.Data.Servers {
			n.add(persistence.Entry{SourceID: s.ID, Kind: persistence.ServerKind, Name: v.Name, Version: v.Version, Server: clone(&v)})
		}
		for _, v := range snapshot.Data.Skills {
			p := model.Skill{Namespace: v.Namespace, Name: v.Name, Version: v.Version, Description: v.Description, Status: v.Status, Title: v.Title, License: v.License,
				Compatibility: v.Compatibility, AllowedTools: v.AllowedTools, Metadata: v.Metadata, Meta: v.Meta, Provenance: v.Provenance}
			if v.Repository != nil {
				p.Repository = &model.SkillRepository{URL: v.Repository.URL, Type: v.Repository.Type}
			}
			for _, i := range v.Icons {
				p.Icons = append(p.Icons, model.SkillIcon{Src: i.Src, Size: i.Size, Type: i.Type, Label: i.Label})
			}
			for _, pkg := range v.Packages {
				p.Packages = append(p.Packages, model.SkillPackage{RegistryType: pkg.RegistryType, Identifier: pkg.Identifier, Digest: pkg.Digest,
					MediaType: pkg.MediaType, URL: pkg.URL, Ref: pkg.Ref, Commit: pkg.Commit, Subfolder: pkg.Subfolder})
			}
			if v.Status == "" {
				p.Status = "active"
			}
			n.add(persistence.Entry{SourceID: s.ID, Kind: persistence.SkillKind, Name: v.Name, Version: v.Version, Skill: clone(&p)})
		}
		for _, v := range snapshot.Data.Plugins {
			p := model.Plugin{Namespace: v.Namespace, Name: v.Name, Version: v.Version, Description: v.Description, Status: v.Status, Title: v.Title, License: v.License,
				Metadata: v.Metadata, Meta: v.Meta}
			if v.Repository != nil {
				p.Repository = &model.PluginRepository{URL: v.Repository.URL, Type: v.Repository.Type}
			}
			for _, i := range v.Icons {
				p.Icons = append(p.Icons, model.PluginIcon{Src: i.Src, Size: i.Size, Type: i.Type, Label: i.Label})
			}
			for _, pkg := range v.Packages {
				p.Packages = append(p.Packages, model.PluginPackage{RegistryType: pkg.RegistryType, Identifier: pkg.Identifier, Digest: pkg.Digest,
					MediaType: pkg.MediaType, URL: pkg.URL, Ref: pkg.Ref, Commit: pkg.Commit, Subfolder: pkg.Subfolder})
			}
			if v.Status == "" {
				p.Status = "active"
			}
			n.add(persistence.Entry{SourceID: s.ID, Kind: persistence.PluginKind, Name: v.Name, Version: v.Version, Plugin: clone(&p)})
		}
		for key, v := range n.records {
			if old, ok := oldIDs[key]; ok {
				v.ID = old
				if v.Skill != nil {
					v.Skill.ID = old
				}
				if v.Plugin != nil {
					v.Plugin.ID = old
				}
				n.records[key] = v
			}
		}
		return nil
	})
}

func (m *memory) Publish(ctx context.Context, v persistence.Entry) (persistence.Entry, error) {
	if err := v.Kind.Validate(); err != nil {
		return persistence.Entry{}, err
	}
	if v.ID != "" || v.SourceID != "" || v.IsLatest || v.Name == "" || v.Version == "" {
		return persistence.Entry{}, persistence.ErrInvalid
	}
	snap := model.Snapshot{}
	switch v.Kind {
	case persistence.ServerKind:
		if v.Server == nil || v.Skill != nil || v.Plugin != nil || v.Server.Name != v.Name || v.Server.Version != v.Version {
			return persistence.Entry{}, persistence.ErrInvalid
		}
		snap.Data.Servers = []model.Server{*v.Server}
	case persistence.SkillKind:
		if v.Skill == nil || v.Server != nil || v.Plugin != nil || v.Skill.Name != v.Name ||
			v.Skill.Version != v.Version || v.Skill.ID != "" || v.Skill.IsLatest ||
			!v.Skill.CreatedAt.IsZero() || !v.Skill.UpdatedAt.IsZero() {
			return persistence.Entry{}, persistence.ErrInvalid
		}
		v.Skill = clone(v.Skill)
		if v.Skill.Status == "" {
			v.Skill.Status = "active"
		}
		snap.Data.Skills = []thvregistry.Skill{formats.SkillPayload(v.Skill)}
	case persistence.PluginKind:
		if v.Plugin == nil || v.Server != nil || v.Skill != nil || v.Plugin.Name != v.Name ||
			v.Plugin.Version != v.Version || v.Plugin.ID != "" || v.Plugin.IsLatest ||
			!v.Plugin.CreatedAt.IsZero() || !v.Plugin.UpdatedAt.IsZero() {
			return persistence.Entry{}, persistence.ErrInvalid
		}
		v.Plugin = clone(v.Plugin)
		if v.Plugin.Status == "" {
			v.Plugin.Status = "active"
		}
		snap.Data.Plugins = []thvregistry.Plugin{formats.PluginPayload(v.Plugin)}
	}
	if err := persistence.ValidateSnapshot(&snap); err != nil {
		return persistence.Entry{}, err
	}
	var out persistence.Entry
	err := m.transact(ctx, func(n *memory) error {
		var id string
		for _, s := range n.sources {
			if s.Managed != nil {
				id = s.ID
				break
			}
		}
		if id == "" {
			return persistence.ErrNotFound
		}
		v.SourceID = id
		key := memKey(v)
		if _, ok := n.records[key]; ok {
			return persistence.ErrConflict
		}
		n.add(v)
		out = n.latest(n.records[key])
		return nil
	})
	return clone(out), err
}

func (m *memory) DeleteManaged(ctx context.Context, kind persistence.EntryKind, name, version string) error {
	if err := kind.Validate(); err != nil {
		return err
	}
	if name == "" || version == "" {
		return persistence.ErrInvalid
	}
	return m.transact(ctx, func(n *memory) error {
		for _, s := range n.sources {
			if s.Managed != nil {
				key := memKey(persistence.Entry{SourceID: s.ID, Kind: kind, Name: name, Version: version})
				if _, ok := n.records[key]; !ok {
					return persistence.ErrNotFound
				}
				delete(n.records, key)
				return nil
			}
		}
		return persistence.ErrNotFound
	})
}

func (m *memory) GetEntry(ctx context.Context, id string, kind persistence.EntryKind, name string, selector persistence.VersionSelector) (persistence.Entry, error) {
	if err := kind.Validate(); err != nil {
		return persistence.Entry{}, err
	}
	if name == "" || selector.Latest == (selector.Exact != "") {
		return persistence.Entry{}, persistence.ErrInvalid
	}
	m.Lock()
	defer m.Unlock()
	if err := ctx.Err(); err != nil {
		return persistence.Entry{}, err
	}
	if !m.hasSource(id) {
		return persistence.Entry{}, persistence.ErrNotFound
	}
	if !selector.Latest {
		v, ok := m.records[memKey(persistence.Entry{SourceID: id, Kind: kind, Name: name, Version: selector.Exact})]
		if !ok {
			return persistence.Entry{}, persistence.ErrNotFound
		}
		return clone(m.latest(v)), nil
	}
	var best persistence.Entry
	for _, v := range m.records {
		if v.SourceID == id && v.Kind == kind && v.Name == name && (best.ID == "" || model.CompareVersions(v.Version, best.Version) > 0) {
			best = v
		}
	}
	if best.ID == "" {
		return best, persistence.ErrNotFound
	}
	return clone(m.latest(best)), nil
}

func (m *memory) ListEntries(ctx context.Context, opts persistence.ListOptions) (persistence.EntryPage, error) {
	var page persistence.EntryPage
	if err := opts.Kind.Validate(); err != nil {
		return page, err
	}
	if opts.Limit < 1 || opts.Limit > 100 {
		return page, persistence.ErrInvalid
	}
	var cur memCursor
	if opts.Cursor != "" {
		if len(opts.Cursor) > persistence.MaxCursorBytes {
			return page, persistence.ErrInvalid
		}
		b, e := base64.RawURLEncoding.DecodeString(opts.Cursor)
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if e != nil || dec.Decode(&cur) != nil || dec.Decode(new(any)) != io.EOF || cur.Binding != memoryBinding(opts) || cur.After == "" {
			return page, persistence.ErrInvalid
		}
	}
	m.Lock()
	defer m.Unlock()
	if err := ctx.Err(); err != nil {
		return page, err
	}
	if !m.hasSource(opts.SourceID) {
		return page, persistence.ErrNotFound
	}
	all := make([]persistence.Entry, 0)
	for _, v := range m.records {
		if v.SourceID != opts.SourceID || v.Kind != opts.Kind || opts.Name != "" && opts.Name != v.Name {
			continue
		}
		v = m.latest(v)
		if opts.LatestOnly && !v.IsLatest {
			continue
		}
		title, description := "", ""
		if v.Server != nil {
			title = v.Server.Title
			description = v.Server.Description
		}
		if v.Skill != nil {
			title = v.Skill.Title
			description = v.Skill.Description
		}
		if v.Plugin != nil {
			title = v.Plugin.Title
			description = v.Plugin.Description
		}
		needle := strings.ToLower(opts.Search)
		if needle != "" && !strings.Contains(strings.ToLower(v.Name), needle) && !strings.Contains(strings.ToLower(title), needle) && !strings.Contains(strings.ToLower(description), needle) {
			continue
		}
		if cur.After != "" && v.Name+"\x00"+v.Version <= cur.After {
			continue
		}
		all = append(all, clone(v))
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Name != all[j].Name {
			return all[i].Name < all[j].Name
		}
		return all[i].Version < all[j].Version
	})
	if len(all) > opts.Limit {
		last := all[opts.Limit-1]
		b, _ := json.Marshal(memCursor{Binding: memoryBinding(opts), After: last.Name + "\x00" + last.Version})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
		if len(page.NextCursor) > persistence.MaxCursorBytes {
			return persistence.EntryPage{}, persistence.ErrInvalid
		}
		all = all[:opts.Limit]
	}
	page.Entries = all
	return page, nil
}

type memCursor struct {
	Binding string
	After   string
}

func memoryBinding(opts persistence.ListOptions) string {
	query, _ := json.Marshal([]any{opts.SourceID, opts.Kind, opts.Name, opts.Search, opts.LatestOnly, opts.Limit, "name-version-C"})
	sum := sha256.Sum256(query)
	return hex.EncodeToString(sum[:])
}

func (m *memory) hasSource(id string) bool {
	for _, s := range m.sources {
		if s.ID == id {
			return true
		}
	}
	return false
}
func (m *memory) add(v persistence.Entry) {
	m.seq++
	v = clone(v)
	v.ID = "memory-" + strconv.Itoa(m.seq)
	if v.Skill != nil {
		v.Skill.ID = v.ID
	}
	if v.Plugin != nil {
		v.Plugin.ID = v.ID
	}
	m.records[memKey(v)] = v
}
func memKey(v persistence.Entry) string {
	return v.SourceID + "\x00" + string(v.Kind) + "\x00" + v.Name + "\x00" + v.Version
}
func (m *memory) latest(v persistence.Entry) persistence.Entry {
	v = clone(v)
	v.IsLatest = true
	for _, other := range m.records {
		if other.SourceID == v.SourceID && other.Kind == v.Kind && other.Name == v.Name && model.CompareVersions(other.Version, v.Version) > 0 {
			v.IsLatest = false
			break
		}
	}
	if v.Skill != nil {
		v.Skill.IsLatest = v.IsLatest
	}
	if v.Plugin != nil {
		v.Plugin.IsLatest = v.IsLatest
	}
	return v
}

func TestMemoryEntries(t *testing.T) {
	t.Parallel()
	RunEntries(t, func(*testing.T) (persistence.Sources, persistence.Entries) { m := fresh(); return m, m })
	RunEntriesMulti(t, func(*testing.T) (persistence.Sources, persistence.Entries, persistence.Entries) {
		m := fresh()
		other := &memory{memoryState: m.memoryState}
		return m, m, other
	})
}
