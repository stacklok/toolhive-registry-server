package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// Independent, test-only copy-on-write implementation of the public domain contract.
type memory struct{ *memoryState }

type memoryState struct {
	sync.Mutex
	sources map[string]persistence.SourceDefinition
	views   map[string]persistence.ViewDefinition
	seq     int
}

var _ persistence.Definitions = (*memory)(nil)

func fresh() *memory {
	return &memory{memoryState: &memoryState{sources: map[string]persistence.SourceDefinition{}, views: map[string]persistence.ViewDefinition{}}}
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func (m *memory) transact(ctx context.Context, f func(*memory) error) error {
	m.Lock()
	defer m.Unlock()
	if e := ctx.Err(); e != nil {
		return e
	}
	next := &memory{memoryState: &memoryState{sources: clone(m.sources), views: clone(m.views), seq: m.seq}}
	if e := f(next); e != nil {
		return e
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	m.sources, m.views, m.seq = next.sources, next.views, next.seq
	return nil
}
func (m *memory) CreateSource(ctx context.Context, s persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	if s.ID != "" || s.Origin != "" {
		return s, persistence.ErrInvalid
	}
	kind, e := s.Kind()
	if e != nil {
		return s, e
	}
	var out persistence.SourceDefinition
	e = m.transact(ctx, func(n *memory) error {
		if _, ok := n.sources[s.Name]; ok {
			return persistence.ErrConflict
		}
		if kind == "managed" {
			for _, v := range n.sources {
				if v.Managed != nil {
					return persistence.ErrConflict
				}
			}
		}
		n.seq++
		s.ID = fmt.Sprintf("%d", n.seq)
		s.Origin = persistence.OriginAPI
		n.sources[s.Name] = clone(s)
		out = clone(s)
		return nil
	})
	return out, e
}
func (m *memory) GetSource(ctx context.Context, name string) (persistence.SourceDefinition, error) {
	m.Lock()
	defer m.Unlock()
	if e := ctx.Err(); e != nil {
		return persistence.SourceDefinition{}, e
	}
	v, ok := m.sources[name]
	if !ok {
		return v, persistence.ErrNotFound
	}
	return clone(v), nil
}
func (m *memory) ListSources(ctx context.Context) ([]persistence.SourceDefinition, error) {
	m.Lock()
	defer m.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	keys := []string{}
	for k := range m.sources {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []persistence.SourceDefinition{}
	for _, k := range keys {
		out = append(out, clone(m.sources[k]))
	}
	return out, nil
}
func (m *memory) UpdateSource(ctx context.Context, name string, s persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	if s.Name != name || (s.Origin != "" && s.Origin != persistence.OriginAPI) {
		return s, persistence.ErrInvalid
	}
	kind, e := s.Kind()
	if e != nil {
		return s, e
	}
	var out persistence.SourceDefinition
	e = m.transact(ctx, func(n *memory) error {
		old, ok := n.sources[name]
		if !ok {
			return persistence.ErrNotFound
		}
		if old.Origin != persistence.OriginAPI {
			return persistence.ErrConflict
		}
		if s.ID != "" && s.ID != old.ID {
			return persistence.ErrConflict
		}
		k, _ := old.Kind()
		if k != kind || old.File != nil && fileLocation(old.File) != fileLocation(s.File) {
			return persistence.ErrConflict
		}
		s.ID = old.ID
		s.Origin = old.Origin
		n.sources[name] = clone(s)
		out = clone(s)
		return nil
	})
	return out, e
}
func (m *memory) DeleteSource(ctx context.Context, name string) error {
	return m.transact(ctx, func(n *memory) error {
		old, ok := n.sources[name]
		if !ok {
			return persistence.ErrNotFound
		}
		if old.Origin != persistence.OriginAPI {
			return persistence.ErrConflict
		}
		for _, v := range n.views {
			for _, ref := range v.Sources {
				if ref == name {
					return persistence.ErrInUse
				}
			}
		}
		delete(n.sources, name)
		return nil
	})
}
func (m *memory) CreateView(ctx context.Context, v persistence.ViewDefinition) (persistence.ViewDefinition, error) {
	if v.Origin != "" {
		return v, persistence.ErrInvalid
	}
	if e := persistence.ValidateView(v); e != nil {
		return v, e
	}
	var out persistence.ViewDefinition
	e := m.transact(ctx, func(n *memory) error {
		if _, ok := n.views[v.Name]; ok {
			return persistence.ErrConflict
		}
		for _, ref := range v.Sources {
			if _, ok := n.sources[ref]; !ok {
				return fmt.Errorf("%w: referenced source %q", persistence.ErrNotFound, ref)
			}
		}
		v.Origin = persistence.OriginAPI
		n.views[v.Name] = clone(v)
		out = clone(v)
		return nil
	})
	return out, e
}
func (m *memory) GetView(ctx context.Context, name string) (persistence.ViewDefinition, error) {
	m.Lock()
	defer m.Unlock()
	if e := ctx.Err(); e != nil {
		return persistence.ViewDefinition{}, e
	}
	v, ok := m.views[name]
	if !ok {
		return v, persistence.ErrNotFound
	}
	return clone(v), nil
}
func (m *memory) ListViews(ctx context.Context) ([]persistence.ViewDefinition, error) {
	m.Lock()
	defer m.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	keys := []string{}
	for k := range m.views {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []persistence.ViewDefinition{}
	for _, k := range keys {
		out = append(out, clone(m.views[k]))
	}
	return out, nil
}
func (m *memory) UpdateView(ctx context.Context, name string, v persistence.ViewDefinition) (persistence.ViewDefinition, error) {
	if v.Name != name || (v.Origin != "" && v.Origin != persistence.OriginAPI) {
		return v, persistence.ErrInvalid
	}
	if e := persistence.ValidateView(v); e != nil {
		return v, e
	}
	var out persistence.ViewDefinition
	e := m.transact(ctx, func(n *memory) error {
		old, ok := n.views[name]
		if !ok {
			return persistence.ErrNotFound
		}
		if old.Origin != persistence.OriginAPI {
			return persistence.ErrConflict
		}
		for _, ref := range v.Sources {
			if _, ok := n.sources[ref]; !ok {
				return fmt.Errorf("%w: referenced source %q", persistence.ErrNotFound, ref)
			}
		}
		v.Origin = persistence.OriginAPI
		n.views[name] = clone(v)
		out = clone(v)
		return nil
	})
	return out, e
}
func (m *memory) DeleteView(ctx context.Context, name string) error {
	return m.transact(ctx, func(n *memory) error {
		old, ok := n.views[name]
		if !ok {
			return persistence.ErrNotFound
		}
		if old.Origin != persistence.OriginAPI {
			return persistence.ErrConflict
		}
		delete(n.views, name)
		return nil
	})
}
func (m *memory) Reconcile(ctx context.Context, sources []persistence.SourceDefinition, views []persistence.ViewDefinition) error {
	if e := persistence.ValidateReconcile(sources, views); e != nil {
		return e
	}
	return m.transact(ctx, func(n *memory) error {
		for _, s := range sources {
			if old, ok := n.sources[s.Name]; ok {
				if old.Origin != persistence.OriginConfig {
					return persistence.ErrConflict
				}
				a, _ := old.Kind()
				b, _ := s.Kind()
				if a != b || old.File != nil && fileLocation(old.File) != fileLocation(s.File) {
					return persistence.ErrConflict
				}
			}
		}
		for _, v := range views {
			if old, ok := n.views[v.Name]; ok && old.Origin != persistence.OriginConfig {
				return persistence.ErrConflict
			}
		}
		for _, existing := range n.sources {
			if existing.Origin == persistence.OriginAPI && existing.Managed != nil {
				for _, desired := range sources {
					if desired.Managed != nil {
						return persistence.ErrConflict
					}
				}
			}
		}
		keepS := map[string]bool{}
		for _, s := range sources {
			keepS[s.Name] = true
			old, ok := n.sources[s.Name]
			if ok {
				s.ID = old.ID
			} else {
				n.seq++
				s.ID = fmt.Sprintf("%d", n.seq)
			}
			s.Origin = persistence.OriginConfig
			n.sources[s.Name] = clone(s)
		}
		keepV := map[string]bool{}
		for _, v := range views {
			keepV[v.Name] = true
			v.Origin = persistence.OriginConfig
			n.views[v.Name] = clone(v)
		}
		for name, v := range n.views {
			if v.Origin == persistence.OriginConfig && !keepV[name] {
				delete(n.views, name)
			}
		}
		for name, s := range n.sources {
			if s.Origin == persistence.OriginConfig && !keepS[name] {
				for _, v := range n.views {
					for _, ref := range v.Sources {
						if ref == name {
							return persistence.ErrInUse
						}
					}
				}
				delete(n.sources, name)
			}
		}
		return nil
	})
}
func fileLocation(f *persistence.FileSpec) string {
	if f.Path != "" {
		return "path"
	}
	if f.URL != "" {
		return "url"
	}
	return "data"
}

func TestMemoryConformance(t *testing.T) {
	t.Parallel()
	Run(t, func(*testing.T) persistence.Definitions { return fresh() })
	RunMulti(t, func(*testing.T) (persistence.Definitions, persistence.Definitions) {
		first := fresh()
		return first, &memory{memoryState: first.memoryState}
	})
}
