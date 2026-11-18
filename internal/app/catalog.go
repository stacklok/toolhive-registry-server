package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/gobwas/glob"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/sync/standalone"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// catalogDefinitions maps the YAML sources and registries to library
// definitions. It keeps v1.5.4's leniency: a syncPolicy or filter on a source
// that is never polled (managed, kubernetes, inline data) is ignored with a
// warning instead of rejected.
func catalogDefinitions(cfg *config.Config) ([]persistence.SourceDefinition, []persistence.ViewDefinition, error) {
	sources := make([]persistence.SourceDefinition, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		d, err := sourceDefinition(cfg, s)
		if err != nil {
			return nil, nil, err
		}
		sources = append(sources, d)
	}
	views := make([]persistence.ViewDefinition, 0, len(cfg.Registries))
	for _, v := range cfg.Registries {
		views = append(views, persistence.ViewDefinition{Name: v.Name, Sources: v.Sources})
	}
	return sources, views, persistence.ValidateReconcile(sources, views)
}

func sourceDefinition(cfg *config.Config, s config.SourceConfig) (persistence.SourceDefinition, error) {
	d := persistence.SourceDefinition{Name: s.Name}
	switch s.GetType() {
	case config.SourceTypeGit:
		g := s.Git
		d.Git = &persistence.GitSpec{Repository: g.Repository, Branch: g.Branch, Tag: g.Tag, Commit: g.Commit, Path: g.Path}
		if g.Auth != nil {
			d.Git.Auth = &persistence.GitAuth{Username: g.Auth.Username, PasswordFile: g.Auth.PasswordFile}
		}
	case config.SourceTypeAPI:
		d.API = &persistence.APISpec{Endpoint: s.API.Endpoint, Timeout: s.API.Timeout}
	case config.SourceTypeFile:
		d.File = &persistence.FileSpec{Path: s.File.Path, URL: s.File.URL, Data: s.File.Data, Timeout: s.File.Timeout}
	case config.SourceTypeManaged:
		d.Managed = &persistence.ManagedSpec{}
	case config.SourceTypeKubernetes:
		namespaces, err := storage.EffectiveNamespaces(s.Kubernetes.Namespaces, cfg.WatchNamespace)
		if err != nil {
			return d, err
		}
		d.Kubernetes = &persistence.KubernetesSpec{Namespaces: namespaces}
	default:
		return d, fmt.Errorf("%w: source %q has no type", persistence.ErrInvalid, s.Name)
	}
	applySyncSettings(&d, s)
	return d, sourcePreflight(d)
}

// applySyncSettings copies the schedule and filter, ignoring them (with a
// warning, as v1.5.4 did) on sources they cannot apply to.
func applySyncSettings(d *persistence.SourceDefinition, s config.SourceConfig) {
	inline := s.File != nil && s.File.Data != ""
	polled := !s.IsNonSyncedSource() && !inline
	if s.SyncPolicy != nil {
		if polled {
			d.Schedule = s.SyncPolicy.Interval
		} else {
			slog.Warn("Ignoring syncPolicy on a source that is not polled", "source", s.Name)
		}
	}
	if s.Filter != nil {
		if polled || inline {
			d.Filter = persistenceFilter(s.Filter)
		} else {
			slog.Warn("Ignoring filter on a source that is not synced", "source", s.Name)
		}
	}
}

func persistenceFilter(f *config.FilterConfig) *persistence.Filter {
	out := &persistence.Filter{}
	if f.Names != nil {
		out.Names = &persistence.NameFilter{Include: f.Names.Include, Exclude: f.Names.Exclude}
	}
	if f.Tags != nil {
		out.Tags = &persistence.NameFilter{Include: f.Tags.Include, Exclude: f.Tags.Exclude}
	}
	return out
}

// sourcePreflight rejects a definition the library could not sync.
func sourcePreflight(d persistence.SourceDefinition) error {
	if _, err := d.Kind(); err != nil {
		return fmt.Errorf("source %q: %w", d.Name, err)
	}
	if d.File != nil && d.File.Data != "" {
		snapshot, err := formats.DecodeUpstream([]byte(d.File.Data))
		if err != nil {
			return fmt.Errorf("%w: source %q has invalid inline registry data", persistence.ErrInvalid, d.Name)
		}
		if err := persistence.ValidateSnapshot(snapshot); err != nil {
			return fmt.Errorf("%w: source %q has incompatible inline registry data", persistence.ErrInvalid, d.Name)
		}
	}
	if d.Filter != nil && d.Filter.Names != nil {
		for _, patterns := range [][]string{d.Filter.Names.Include, d.Filter.Names.Exclude} {
			for _, pattern := range patterns {
				if _, err := filepath.Match(pattern, ""); err != nil {
					return fmt.Errorf("%w: source %q has an invalid name filter", persistence.ErrInvalid, d.Name)
				}
				if _, err := glob.Compile(pattern); err != nil {
					return fmt.Errorf("%w: source %q has an invalid name filter", persistence.ErrInvalid, d.Name)
				}
			}
		}
	}
	return nil
}

// applyConfigClaims stores the claims of config-file sources and registries,
// which the claims-free library never writes, then brings entry claims in
// line:
//   - git, api and file sources: entries take the source's claims now, as
//     v1.5.4 did at startup;
//   - kubernetes sources: when the source's claims changed, the next sync
//     commits a full snapshot so annotation overrides are honored;
//   - the managed source: published entries keep their own claims. v1.5.4
//     reset them to the source's claims on every restart; that was a bug.
func applyConfigClaims(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to store config claims: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := sqlc.New(tx)
	for _, s := range cfg.Sources {
		claims := db.SerializeClaims(s.Claims)
		changed, err := q.SetConfigSourceClaims(ctx, sqlc.SetConfigSourceClaimsParams{Name: s.Name, Claims: claims})
		if err != nil {
			return fmt.Errorf("failed to store claims of source %s: %w", s.Name, err)
		}
		switch s.GetType() {
		case config.SourceTypeGit, config.SourceTypeAPI, config.SourceTypeFile:
			source, err := q.GetSourceByName(ctx, s.Name)
			if err != nil {
				return fmt.Errorf("failed to read source %s: %w", s.Name, err)
			}
			if err := q.PropagateSourceClaimsToEntries(ctx, sqlc.PropagateSourceClaimsToEntriesParams{
				SourceID: source.ID, Claims: claims,
			}); err != nil {
				return fmt.Errorf("failed to propagate claims of source %s: %w", s.Name, err)
			}
		case config.SourceTypeKubernetes:
			if changed > 0 {
				if err := q.InvalidateSourceSyncBaseline(ctx, s.Name); err != nil {
					return fmt.Errorf("failed to schedule resync of source %s: %w", s.Name, err)
				}
			}
		case config.SourceTypeManaged:
			// Published entries keep their own claims.
		}
	}
	for _, r := range cfg.Registries {
		if _, err := q.SetConfigRegistryClaims(ctx, sqlc.SetConfigRegistryClaimsParams{
			Name: r.Name, Claims: db.SerializeClaims(r.Claims),
		}); err != nil {
			return fmt.Errorf("failed to store claims of registry %s: %w", r.Name, err)
		}
	}
	return tx.Commit(ctx)
}

// syncRunner runs the library sync runtime for the app's lifetime.
type syncRunner struct {
	runtime *standalone.Runtime
	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// Start runs the runtime until Stop or ctx ends; it blocks. An error that
// surfaces while shutting down (such as a fetch that failed as it was being
// cancelled) is not a failure of the run and is not returned.
func (s *syncRunner) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel, s.done = cancel, make(chan struct{})
	done := s.done
	s.mu.Unlock()
	defer close(done)
	defer cancel()
	err := s.runtime.Run(ctx)
	if err != nil && ctx.Err() != nil {
		slog.Debug("Source synchronization stopped", "error", err)
		return nil
	}
	return err
}

// Stop ends the runtime and waits for its workers to finish. Called before
// Start, it keeps Start from running.
func (s *syncRunner) Stop() error {
	s.mu.Lock()
	s.stopped = true
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	<-done
	return nil
}

// Ready reports whether discovery and every worker have started.
func (s *syncRunner) Ready() bool { return s.runtime.Ready() }
