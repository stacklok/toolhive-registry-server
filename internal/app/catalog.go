package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

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
// warning instead of rejected, and settings v1.5.4 accepted but the library
// refuses are normalized with a warning (see docs/configuration.md).
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
		views = append(views, persistence.ViewDefinition{Name: v.Name, Sources: dedupeRegistrySources(v.Name, v.Sources)})
	}
	return sources, views, persistence.ValidateReconcile(sources, views)
}

func sourceDefinition(cfg *config.Config, s config.SourceConfig) (persistence.SourceDefinition, error) {
	d := persistence.SourceDefinition{Name: s.Name}
	switch s.GetType() {
	case config.SourceTypeGit:
		spec, ambiguous := normalizeGitRefs(gitSpec(s.Git))
		if ambiguous {
			slog.Warn("Git source sets more than one of branch, tag and commit; as in v1.5.4, "+
				"every sync of it fails until only one is set", "source", s.Name)
		}
		d.Git = &spec
	case config.SourceTypeAPI:
		d.API = &persistence.APISpec{Endpoint: s.API.Endpoint, Timeout: s.API.Timeout}
	case config.SourceTypeFile:
		timeout, warning := config.NormalizeFileTimeout(s.File)
		if warning != "" {
			slog.Warn("Normalized file.timeout", "source", s.Name, "detail", warning)
		}
		d.File = &persistence.FileSpec{Path: s.File.Path, URL: s.File.URL, Data: s.File.Data, Timeout: timeout}
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
			interval, warning := config.NormalizeSyncInterval(s.SyncPolicy.Interval)
			if warning != "" {
				slog.Warn("Normalized syncPolicy.interval", "source", s.Name, "detail", warning)
			}
			d.Schedule = interval
		} else {
			slog.Warn("Ignoring syncPolicy on a source that is not polled", "source", s.Name)
		}
	}
	if s.Filter != nil {
		if polled || inline {
			d.Filter = persistenceFilter(s.Name, s.Filter)
		} else {
			slog.Warn("Ignoring filter on a source that is not synced", "source", s.Name)
		}
	}
}

// persistenceFilter maps a filter, rewriting invalid name patterns to the
// selection v1.5.4 made with them (see config.NormalizeNameFilter).
func persistenceFilter(source string, f *config.FilterConfig) *persistence.Filter {
	out := &persistence.Filter{}
	if names, warning := config.NormalizeNameFilter(f.Names); names != nil {
		if warning != "" {
			slog.Error("Invalid name filter pattern", "source", source, "detail", warning)
		}
		out.Names = &persistence.NameFilter{Include: names.Include, Exclude: names.Exclude}
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
	if !validNameFilter(d.Filter) {
		return fmt.Errorf("%w: source %q has an invalid name filter", persistence.ErrInvalid, d.Name)
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
//     Its initial sync status is recorded here too, as v1.5.4 did.
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
			// Published entries keep their own claims. The library never syncs a
			// managed source, so record v1.5.4's initial status for the admin API;
			// an existing row is left alone.
			msg := fmt.Sprintf("Non-synced source (type: %s)", config.SourceTypeManaged)
			if err := q.InitializeSourceSync(ctx, sqlc.InitializeSourceSyncParams{
				Name: s.Name, SyncStatus: sqlc.SyncStatusCOMPLETED, ErrorMsg: &msg,
			}); err != nil {
				return fmt.Errorf("failed to record sync status of source %s: %w", s.Name, err)
			}
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

// Restart delays after the sync runtime stops on an error.
const (
	defaultSyncRestartMin = time.Second
	defaultSyncRestartMax = time.Minute
)

// syncRunner runs the library sync runtime for the app's lifetime.
type syncRunner struct {
	runtime *standalone.Runtime
	// restartMin and restartMax bound the delay before a failed runtime is
	// restarted; zero means the defaults.
	restartMin, restartMax time.Duration

	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// Start runs the runtime until Stop or ctx ends; it blocks and returns nil.
// The runtime stops on a storage failure it cannot attribute to one source (the
// database unreachable, a privilege missing); Start logs that and restarts it
// with exponential backoff, so sync resumes once the cause clears. An error that
// surfaces while shutting down (such as a fetch that failed as it was being
// cancelled) is not a failure of the run.
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
	low, high := s.restartMin, s.restartMax
	if low <= 0 {
		low = defaultSyncRestartMin
	}
	if high < low {
		high = max(low, defaultSyncRestartMax)
	}
	delay := low
	for {
		began := time.Now()
		err := s.runtime.Run(ctx)
		if ctx.Err() != nil {
			if err != nil {
				slog.Debug("Source synchronization stopped", "error", err)
			}
			return nil
		}
		if time.Since(began) > high {
			delay = low // a run that lasted counts as recovered
		}
		slog.Error("Source synchronization stopped; restarting", "error", err, "retry_in", delay.String())
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		delay = min(2*delay, high)
	}
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
