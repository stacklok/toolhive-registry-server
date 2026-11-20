package conformance

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

var _ persistence.ViewEntryReader = (*memory)(nil)

func (m *memory) GetViewEntry(ctx context.Context, view string, name persistence.EntryName, selector persistence.VersionSelector) (persistence.Entry, error) {
	if err := name.Validate(view, selector); err != nil {
		return persistence.Entry{}, err
	}
	page, err := m.ListViewEntries(ctx, view, persistence.ViewQuery{Kind: name.Kind, Name: name.Name, Namespace: name.Namespace, VersionSelector: selector, Limit: 1})
	if err != nil {
		return persistence.Entry{}, err
	}
	if len(page.Entries) == 0 {
		return persistence.Entry{}, persistence.ErrNotFound
	}
	return page.Entries[0], nil
}

func (m *memory) ListViewEntries(ctx context.Context, view string, query persistence.ViewQuery) (persistence.EntryPage, error) {
	if err := query.Validate(view); err != nil {
		return persistence.EntryPage{}, err
	}
	m.Lock()
	defer m.Unlock()
	if err := ctx.Err(); err != nil {
		return persistence.EntryPage{}, err
	}
	definition, ok := m.views[view]
	if !ok {
		return persistence.EntryPage{}, persistence.ErrViewNotFound
	}
	winners := map[string]string{}
	for _, source := range definition.Sources {
		id := m.sources[source].ID
		for _, v := range m.records {
			if v.SourceID != id {
				continue
			}
			key := string(v.Kind) + "\x00" + v.Name
			if _, exists := winners[key]; !exists {
				winners[key] = id
			}
		}
	}
	// Cursors are positional: only the kind and the last (name, version).
	var cur memoryViewCursor
	if query.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(query.Cursor)
		if err != nil {
			return persistence.EntryPage{}, persistence.ErrInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&cur) != nil || decoder.Decode(new(any)) != io.EOF || cur.Format != 2 || cur.Kind != query.Kind || cur.Name == "" || cur.Version == "" {
			return persistence.EntryPage{}, persistence.ErrInvalid
		}
		if err := (persistence.EntryName{Kind: query.Kind, Name: cur.Name}).Validate(view, persistence.VersionSelector{Exact: cur.Version}); err != nil {
			return persistence.EntryPage{}, err
		}
	}
	entries := make([]persistence.Entry, 0)
	for _, v := range m.records {
		if v.Kind != query.Kind || winners[string(v.Kind)+"\x00"+v.Name] != v.SourceID {
			continue
		}
		v = m.latest(v)
		namespace, title, description := "", "", ""
		if v.Server != nil {
			title, description = v.Server.Title, v.Server.Description
		}
		if v.Skill != nil {
			namespace, title, description = v.Skill.Namespace, v.Skill.Title, v.Skill.Description
		}
		if v.Plugin != nil {
			namespace, title, description = v.Plugin.Namespace, v.Plugin.Title, v.Plugin.Description
		}
		if query.Name != "" && query.Name != v.Name || query.Namespace != "" && query.Namespace != namespace ||
			query.VersionSelector.Latest && !v.IsLatest || query.VersionSelector.Exact != "" && query.VersionSelector.Exact != v.Version ||
			query.UpdatedSince != nil && !m.updated[memKey(v)].After(*query.UpdatedSince) {
			continue
		}
		needle := "%" + strings.ToLower(query.Search) + "%"
		if query.Search != "" && !sqlLike(strings.ToLower(v.Name), needle) && !sqlLike(strings.ToLower(title), needle) && !sqlLike(strings.ToLower(description), needle) {
			continue
		}
		if cur.Name != "" && (v.Name < cur.Name || v.Name == cur.Name && v.Version <= cur.Version) {
			continue
		}
		entries = append(entries, clone(v))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Version < entries[j].Version
	})
	page := persistence.EntryPage{}
	if len(entries) > query.Limit {
		last := entries[query.Limit-1]
		data, _ := json.Marshal(memoryViewCursor{Format: 2, Kind: query.Kind, Name: last.Name, Version: last.Version})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
		if len(page.NextCursor) > persistence.MaxCursorBytes {
			return persistence.EntryPage{}, persistence.ErrInvalid
		}
		entries = entries[:query.Limit]
	}
	if err := ctx.Err(); err != nil {
		return persistence.EntryPage{}, err
	}
	page.Entries = entries
	return page, nil
}

type memoryViewCursor struct {
	Format  int
	Kind    persistence.EntryKind
	Name    string
	Version string
}

// sqlLike matches value against a SQL LIKE pattern with the default escape:
// % matches any run of characters, _ any one character, and \ makes the next
// character literal. It mirrors PostgreSQL for the patterns search builds.
func sqlLike(value, pattern string) bool {
	v, p := []rune(value), []rune(pattern)
	var match func(i, j int) bool
	match = func(i, j int) bool {
		for j < len(p) {
			switch c := p[j]; {
			case c == '%':
				for j < len(p) && p[j] == '%' {
					j++
				}
				for k := i; k <= len(v); k++ {
					if match(k, j) {
						return true
					}
				}
				return false
			case c == '_':
				if i == len(v) {
					return false
				}
			case c == '\\' && j+1 < len(p):
				j++
				if i == len(v) || v[i] != p[j] {
					return false
				}
			default:
				if i == len(v) || v[i] != c {
					return false
				}
			}
			i++
			j++
		}
		return i == len(v)
	}
	return match(0, 0)
}

func TestMemoryViewEntries(t *testing.T) {
	t.Parallel()
	RunViewEntries(t, func(*testing.T) (persistence.Definitions, persistence.Entries, persistence.ViewEntryReader) {
		m := fresh()
		return m, m, m
	})
}
