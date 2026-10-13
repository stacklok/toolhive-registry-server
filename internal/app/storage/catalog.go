package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	corepg "github.com/stacklok/toolhive-core/postgres"

	"github.com/stacklok/toolhive-registry-server/database"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/telemetry"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	registrypg "github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// CatalogFactory opens storage without initialization, repair, migrations or workers.
// Any returned storage (including with an error) transfers cleanup to the app.
// With nil storage, the factory cleans partially opened resources itself. Every
// injected factory implements all phases; there is no optional assertion/fallback.
//
//go:generate mockgen -destination=mocks/catalog.go -package=mocks -source=catalog.go
type CatalogFactory interface {
	Open(context.Context, *config.Config) (CatalogStorage, error)
}

// CatalogStorage owns its declared resources. Close is called exactly once after
// failed preparation or after all serving and inline work has joined.
type CatalogStorage interface {
	Preflight(context.Context, []persistence.SourceDefinition, []persistence.ViewDefinition, string) (
		[]persistence.SourceDefinition, error,
	)
	Consume(context.Context, string) error
	ConsumeEmpty(context.Context, string) error
	Initialize(context.Context, []persistence.SourceDefinition, []persistence.ViewDefinition) (CatalogComponents, error)
	Close()
}

// CatalogComponents are initialized policy-free capabilities, never HTTP services.
type CatalogComponents struct {
	Metrics     telemetry.RegistryMetricReader
	Definitions persistence.Definitions
	Entries     interface {
		persistence.ManagedPublisher
		persistence.ViewEntryReader
		persistence.EntryDiagnostics
	}
	Jobs persistence.Jobs
}

// CatalogPostgres opens an owned pool by default. BorrowedPool explicitly transfers
// no ownership; the host must keep it open until the app has stopped and joined.
type CatalogPostgres struct{ BorrowedPool *pgxpool.Pool }
type catalogPostgres struct {
	pool        *pgxpool.Pool
	owned       bool
	limit       int
	definitions *registrypg.Definitions
}

// Open borrows the supplied pool or opens an owned pool without catalog writes.
func (f CatalogPostgres) Open(ctx context.Context, cfg *config.Config) (CatalogStorage, error) {
	pool := f.BorrowedPool
	if pool == nil {
		pgcfg, err := cfg.Database.ToCorePostgresConfig()
		if err != nil {
			return nil, errors.New("database configuration unavailable")
		}
		pool, err = corepg.NewPool(ctx, pgcfg, corepg.WithAfterConnect(database.RegisterEnumArrayCodecs))
		if err != nil {
			return nil, errors.New("catalog database connection unavailable")
		}
	}
	defs, err := registrypg.NewDefinitions(pool)
	if err != nil {
		if f.BorrowedPool == nil {
			pool.Close()
		}
		return nil, err
	}
	return &catalogPostgres{pool: pool, owned: f.BorrowedPool == nil, limit: cfg.Database.GetMaxMetaSize(), definitions: defs}, nil
}

//nolint:gocyclo // Check all historical origins, immutable specs and retained API references before writes.
func (s *catalogPostgres) Preflight(
	ctx context.Context, desired []persistence.SourceDefinition, views []persistence.ViewDefinition, watch string,
) ([]persistence.SourceDefinition, error) {
	persisted, err := s.definitions.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("inspect historical source definitions before exposure consumption: %w", err)
	}
	configured := map[string]persistence.SourceDefinition{}
	for _, d := range desired {
		configured[d.Name] = d
	}
	managed := 0
	for _, d := range desired {
		if d.Managed != nil {
			managed++
		}
	}
	for _, d := range persisted {
		if replacement, retained := configured[d.Name]; retained && d.Origin == persistence.OriginConfig {
			oldKind, oldErr := d.Kind()
			newKind, newErr := replacement.Kind()
			if oldErr == nil && newErr == nil && (oldKind != newKind || differentFileLocation(d.File, replacement.File)) {
				return nil, fmt.Errorf("CONFIG source %q changes immutable kind/location; explicitly remediate before cutover", d.Name)
			}
		}
		if d.Origin == persistence.OriginAPI && d.Managed != nil {
			managed++
		}
		if _, err := d.Kind(); err != nil {
			return nil, fmt.Errorf("historical %s source %q is incompatible; explicitly remediate "+
				"its name, schedule/filter or specification offline before cutover: %w", d.Origin, d.Name, err)
		}
		if err := s.definitions.CheckSourceCompatibility(ctx, d.Name); err != nil {
			return nil, fmt.Errorf("historical source %q has unsupported fields; explicitly remediate offline: %w", d.Name, err)
		}
		if d.Origin == persistence.OriginAPI {
			if _, collision := configured[d.Name]; collision {
				return nil, fmt.Errorf("CONFIG source %q collides with API ownership", d.Name)
			}
			if d.Kubernetes != nil && len(d.Kubernetes.Namespaces) == 0 && watch != "" {
				return nil, fmt.Errorf("API Kubernetes source %q has implicit all-namespace scope with global watch fallback; "+
					"explicitly materialize the intended namespaces in its stored definition before cutover", d.Name)
			}
		}
	}
	if managed > 1 {
		return nil, errors.New("final CONFIG/API catalog would contain multiple managed sources; explicitly remediate before cutover")
	}
	historicalViews, err := s.definitions.ListViews(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range historicalViews {
		if err := persistence.ValidateView(v); err != nil {
			return nil, fmt.Errorf("historical registry %q requires explicit remediation: %w", v.Name, err)
		}
		if v.Origin == persistence.OriginAPI {
			for _, configuredView := range views {
				if v.Name == configuredView.Name {
					return nil, fmt.Errorf("CONFIG registry %q collides with API ownership", v.Name)
				}
			}
			for _, name := range v.Sources {
				for _, d := range persisted {
					if d.Name == name && d.Origin == persistence.OriginConfig {
						if _, retained := configured[name]; !retained {
							return nil, fmt.Errorf("API registry %q references removed CONFIG source %q; "+
								"explicitly update its links before cutover", v.Name, name)
						}
					}
				}
			}
		}
	}
	return persisted, nil
}

func (s *catalogPostgres) Consume(ctx context.Context, revision string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return errors.New("exposure connection unavailable")
	}
	defer conn.Release()
	return db.ConsumeExposure(ctx, conn.Conn(), revision)
}

func (s *catalogPostgres) ConsumeEmpty(ctx context.Context, revision string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return errors.New("exposure connection unavailable")
	}
	defer conn.Release()
	return db.ConsumeEmptyExposure(ctx, conn.Conn(), revision)
}

func (s *catalogPostgres) Initialize(
	ctx context.Context, sources []persistence.SourceDefinition, views []persistence.ViewDefinition,
) (CatalogComponents, error) {
	if err := s.definitions.Reconcile(ctx, sources, views); err != nil {
		return CatalogComponents{}, err
	}
	entries, err := registrypg.NewEntries(ctx, s.pool, s.limit)
	if err != nil {
		return CatalogComponents{}, err
	}
	jobs, err := registrypg.NewJobs(s.pool, s.limit)
	if err != nil {
		return CatalogComponents{}, err
	}
	return CatalogComponents{
		Definitions: s.definitions, Entries: entries, Jobs: jobs, Metrics: &registryMetricsReader{pool: s.pool},
	}, nil
}
func (s *catalogPostgres) Close() {
	if s.owned {
		s.pool.Close()
	}
}

func differentFileLocation(a, b *persistence.FileSpec) bool {
	if a == nil || b == nil {
		return false
	}
	return (a.Path != "") != (b.Path != "") || (a.URL != "") != (b.URL != "") || (a.Data != "") != (b.Data != "")
}

// EffectiveNamespaces canonicalizes explicit > global watch > all. Callers persist
// this exact scope; neither the operator nor Jobs applies a client-only fallback.
func EffectiveNamespaces(explicit []string, watch string) ([]string, error) {
	namespaces := slices.Clone(explicit)
	if len(namespaces) == 0 && watch != "" {
		namespaces = strings.Split(watch, ",")
	}
	for i, n := range namespaces {
		namespaces[i] = strings.TrimSpace(n)
		if namespaces[i] == "" {
			return nil, fmt.Errorf("%w: namespace scope contains an empty namespace; omit the whole scope for all namespaces",
				persistence.ErrInvalid)
		}
		if err := config.ValidateSourceName(namespaces[i]); err != nil {
			return nil, fmt.Errorf("%w: invalid namespace scope", persistence.ErrInvalid)
		}
	}
	slices.Sort(namespaces)
	return slices.Compact(namespaces), nil
}
