package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/gobwas/glob"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db/pgtypes"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// normalizeStoredSources rewrites the settings of API-created sources that
// v1.5.4's admin API stored but the library refuses, with the rules config
// sources and new admin requests get (config.NormalizeSyncInterval,
// config.NormalizeFileTimeout and config.NormalizeNameFilter), and logs a
// warning per rewritten source. Only a source these settings make unusable,
// and that is usable once they are rewritten, is changed; any other unusable
// source is left for the sync runtime to log and skip. A rewritten source no
// longer qualifies, so later starts change nothing. The write is the one the
// library's UpdateSource makes: it leaves claims alone, and the definition
// generation advances so the source is synced again.
func normalizeStoredSources(ctx context.Context, pool *pgxpool.Pool) error {
	q := sqlc.New(pool)
	names, err := q.ListAllSourceNames(ctx)
	if err != nil {
		return fmt.Errorf("failed to list stored sources: %w", err)
	}
	rows, err := q.GetAPISourcesByNames(ctx, names)
	if err != nil {
		return fmt.Errorf("failed to read stored API sources: %w", err)
	}
	for _, row := range rows {
		if _, _, ok := normalizeStoredSource(storedSourceFromRow(row)); !ok {
			continue
		}
		if err := rewriteStoredSource(ctx, pool, row.Name); err != nil {
			return fmt.Errorf("failed to normalize stored source %s: %w", row.Name, err)
		}
	}
	return nil
}

// storedSource is the raw stored form of a source's sync settings.
type storedSource struct {
	name     string
	kind     string
	spec     []byte
	filter   []byte
	schedule pgtypes.Interval
}

// normalizeStoredSource returns s with v1.5.4's settings normalized and a
// warning per change. ok is false when s is usable as stored, cannot be read,
// or stays unusable after normalization.
func normalizeStoredSource(s storedSource) (storedSource, []string, bool) {
	before, err := s.definition()
	if err != nil || usableDefinition(before) {
		return s, nil, false
	}
	out := s
	var warnings []string
	for _, step := range []func(*storedSource, persistence.SourceDefinition) (string, error){
		normalizeStoredSchedule, normalizeStoredTimeout, normalizeStoredNameFilter,
	} {
		warning, err := step(&out, before)
		if err != nil {
			return s, nil, false
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	if len(warnings) == 0 {
		return s, nil, false
	}
	after, err := out.definition()
	if err != nil || !usableDefinition(after) {
		return s, nil, false
	}
	return out, warnings, true
}

// normalizeStoredSchedule applies config.NormalizeSyncInterval to a polled
// source's stored interval. A NULL interval is left alone: v1.5.4 never
// synced such a source on a schedule.
func normalizeStoredSchedule(s *storedSource, d persistence.SourceDefinition) (string, error) {
	inline := d.File != nil && d.File.Data != ""
	if d.Managed != nil || d.Kubernetes != nil || inline || !s.schedule.Valid {
		return "", nil
	}
	interval, warning := config.NormalizeSyncInterval(s.schedule.Duration.String())
	if warning == "" {
		return "", nil
	}
	parsed, err := pgtypes.ParseDuration(interval)
	if err != nil {
		return "", err
	}
	s.schedule = parsed
	return warning, nil
}

// normalizeStoredTimeout applies config.NormalizeFileTimeout to a file
// source's stored timeout.
func normalizeStoredTimeout(s *storedSource, d persistence.SourceDefinition) (string, error) {
	if d.File == nil {
		return "", nil
	}
	timeout, warning := config.NormalizeFileTimeout(&config.FileConfig{
		Path: d.File.Path, URL: d.File.URL, Data: d.File.Data, Timeout: d.File.Timeout,
	})
	if warning == "" {
		return "", nil
	}
	spec, err := setJSONField(s.spec, "timeout", timeout)
	if err != nil {
		return "", err
	}
	s.spec = spec
	return warning, nil
}

// normalizeStoredNameFilter applies config.NormalizeNameFilter to a source's
// stored name filter.
func normalizeStoredNameFilter(s *storedSource, d persistence.SourceDefinition) (string, error) {
	if d.Filter == nil || d.Filter.Names == nil {
		return "", nil
	}
	names, warning := config.NormalizeNameFilter(&config.NameFilterConfig{
		Include: d.Filter.Names.Include, Exclude: d.Filter.Names.Exclude,
	})
	if warning == "" {
		return "", nil
	}
	filter, err := setJSONField(s.filter, "names", persistence.NameFilter{Include: names.Include, Exclude: names.Exclude})
	if err != nil {
		return "", err
	}
	s.filter = filter
	return warning, nil
}

// usableDefinition reports whether the library can sync d: its definition is
// valid and its name filter compiles.
func usableDefinition(d persistence.SourceDefinition) bool {
	if _, err := d.Kind(); err != nil {
		return false
	}
	return validNameFilter(d.Filter)
}

// validNameFilter reports whether every name pattern compiles the way the
// library's sync compiles it.
func validNameFilter(f *persistence.Filter) bool {
	if f == nil || f.Names == nil {
		return true
	}
	for _, patterns := range [][]string{f.Names.Include, f.Names.Exclude} {
		for _, pattern := range patterns {
			if _, err := filepath.Match(pattern, ""); err != nil {
				return false
			}
			if _, err := glob.Compile(pattern); err != nil {
				return false
			}
		}
	}
	return true
}

// definition decodes s as the library's storage reads decode a source row.
func (s storedSource) definition() (persistence.SourceDefinition, error) {
	d := persistence.SourceDefinition{Name: s.name, Origin: persistence.OriginAPI}
	if s.schedule.Valid && s.schedule.Duration != 0 {
		d.Schedule = s.schedule.Duration.String()
	}
	var target any
	switch config.SourceType(s.kind) {
	case config.SourceTypeGit:
		d.Git = &persistence.GitSpec{}
		target = d.Git
	case config.SourceTypeAPI:
		d.API = &persistence.APISpec{}
		target = d.API
	case config.SourceTypeFile:
		d.File = &persistence.FileSpec{}
		target = d.File
	case config.SourceTypeManaged:
		d.Managed = &persistence.ManagedSpec{}
	case config.SourceTypeKubernetes:
		d.Kubernetes = &persistence.KubernetesSpec{}
		target = d.Kubernetes
	default:
		return d, fmt.Errorf("%w: unknown stored source kind", persistence.ErrInvalid)
	}
	if target != nil && !emptyJSON(s.spec) {
		if err := json.Unmarshal(s.spec, target); err != nil {
			return d, fmt.Errorf("%w: unreadable stored definition", persistence.ErrInvalid)
		}
	}
	if !emptyJSON(s.filter) {
		d.Filter = &persistence.Filter{}
		if err := json.Unmarshal(s.filter, d.Filter); err != nil {
			return d, fmt.Errorf("%w: unreadable stored filter", persistence.ErrInvalid)
		}
	}
	d, _ = d.WithoutUnusableSync()
	return d, nil
}

// rewriteStoredSource normalizes one API-created source under a row lock, so
// a concurrent admin update or another replica's startup is not overwritten
// with stale values.
func rewriteStoredSource(ctx context.Context, pool *pgxpool.Pool, name string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := sqlc.New(tx)
	if _, err := q.DefLockSource(ctx, name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	rows, err := q.GetAPISourcesByNames(ctx, []string{name})
	if err != nil || len(rows) == 0 {
		return err
	}
	out, warnings, ok := normalizeStoredSource(storedSourceFromRow(rows[0]))
	if !ok {
		return nil
	}
	raw, err := q.DefSourceRaw(ctx, name)
	if err != nil {
		return err
	}
	if err := q.DefUpdateSource(ctx, sqlc.DefUpdateSourceParams{
		Name: name, Column2: out.spec, Column3: out.filter, Column4: out.schedule, Syncable: raw.Syncable,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.Warn("Normalized stored settings of an API-created source that v1.5.4 accepted but cannot be synced as stored",
		"source", name, "changes", warnings)
	return nil
}

func storedSourceFromRow(row sqlc.GetAPISourcesByNamesRow) storedSource {
	return storedSource{
		name: row.Name, kind: row.SourceType, spec: row.SourceConfig, filter: row.FilterConfig,
		schedule: row.SyncSchedule,
	}
}

// setJSONField returns the JSON object raw with key set to value, or removed
// when value is empty. Other fields are kept as stored.
func setJSONField(raw []byte, key string, value any) ([]byte, error) {
	fields := map[string]json.RawMessage{}
	if !emptyJSON(raw) {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	switch string(encoded) {
	case `""`, "{}", "null":
		delete(fields, key)
	default:
		fields[key] = encoded
	}
	return json.Marshal(fields)
}

func emptyJSON(raw []byte) bool {
	return len(raw) == 0 || string(raw) == "null"
}
