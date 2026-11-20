package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/stacklok/toolhive-registry-server/internal/app/storage"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

// errAmbiguousGitRef is v1.5.4's sync error for a git source that names more
// than one of branch, tag and commit.
var errAmbiguousGitRef = errors.New("registry validation failed: only one of branch, tag, or commit may be specified")

// reconcileConfig applies the config file's sources and registries, then their
// claims. A conflict with API-created definitions (a config source named like
// an API-created source, a config that drops a source an API-created registry
// uses, a config managed source beside an API-created one, a kind change to or
// from managed) does not stop startup: v1.5.4 logged it and kept serving. The
// conflict is logged, the stored definitions stay in force and keep syncing,
// and config claims are applied only to config sources the database already
// holds with the same type. Any other failure is returned.
func reconcileConfig(
	ctx context.Context, cat *storage.Catalog, cfg *config.Config,
	sources []persistence.SourceDefinition, views []persistence.ViewDefinition,
) error {
	err := cat.Definitions.Reconcile(ctx, sources, views)
	if err == nil {
		return applyConfigClaims(ctx, cat.Pool, cfg)
	}
	if !isConfigConflict(err) {
		return fmt.Errorf("failed to apply config sources and registries: %w", err)
	}
	slog.Error("Config sources and registries were not applied because they conflict with the database; "+
		"serving and syncing the stored definitions until the conflict is resolved", "error", err)
	stored, err := cat.Definitions.ListSources(ctx)
	if err != nil {
		return fmt.Errorf("failed to read stored sources: %w", err)
	}
	return applyConfigClaims(ctx, cat.Pool, configClaimsForStored(cfg, stored))
}

// withAmbiguousGitRefs wraps fetcher so that a config git source whose config
// names more than one ref fails every sync with v1.5.4's error, as v1.5.4 did:
// its git handler refused such a source on each sync while the server kept
// running. It returns fetcher unchanged when no config source is affected.
func withAmbiguousGitRefs(fetcher reconcile.Fetcher, cfg *config.Config) reconcile.Fetcher {
	refused := map[string]persistence.GitSpec{}
	for _, s := range cfg.Sources {
		if s.GetType() != config.SourceTypeGit {
			continue
		}
		if spec, ambiguous := normalizeGitRefs(gitSpec(s.Git)); ambiguous {
			refused[s.Name] = spec
		}
	}
	if len(refused) == 0 {
		return fetcher
	}
	return ambiguousGitRefFetcher{raw: fetcher, refused: refused}
}

// ambiguousGitRefFetcher refuses the config git sources in refused while their
// stored definition is still the one built from the ambiguous config.
type ambiguousGitRefFetcher struct {
	raw     reconcile.Fetcher
	refused map[string]persistence.GitSpec
}

// Fetch fails a refused source with v1.5.4's error and delegates the rest.
func (f ambiguousGitRefFetcher) Fetch(ctx context.Context, d persistence.SourceDefinition) (ingest.Result, error) {
	if f.refuses(d) {
		return ingest.Result{}, ingest.WithCause(ingest.ErrFetch, errAmbiguousGitRef)
	}
	return f.raw.Fetch(ctx, d)
}

// FetchIfChanged refuses the same sources as Fetch and otherwise keeps the
// wrapped fetcher's unchanged-content shortcut (reconcile.ChangeFetcher).
func (f ambiguousGitRefFetcher) FetchIfChanged(
	ctx context.Context, d persistence.SourceDefinition, appliedHash string,
) (ingest.Result, bool, error) {
	changes, ok := f.raw.(reconcile.ChangeFetcher)
	if !ok || f.refuses(d) {
		result, err := f.Fetch(ctx, d)
		return result, false, err
	}
	return changes.FetchIfChanged(ctx, d, appliedHash)
}

// refuses reports whether d is still the definition built from an ambiguous
// git config.
func (f ambiguousGitRefFetcher) refuses(d persistence.SourceDefinition) bool {
	spec, ok := f.refused[d.Name]
	return ok && d.Origin == persistence.OriginConfig && d.Git != nil && sameGitSpec(*d.Git, spec)
}

// isConfigConflict reports whether a reconcile error is a conflict between the
// config and stored definitions rather than a storage failure. A write that
// lost every retry to concurrent writers is a storage failure.
func isConfigConflict(err error) bool {
	if errors.Is(err, persistence.ErrContention) {
		return false
	}
	return errors.Is(err, persistence.ErrConflict) || errors.Is(err, persistence.ErrInUse)
}

// configClaimsForStored returns cfg with only the sources the database holds
// as config sources of the same type, so claims are never written for a
// source the config did not define (an API-created one of the same name) or
// whose new definition was not applied. Registry claims are written only to
// config-owned registries by the query itself.
func configClaimsForStored(cfg *config.Config, stored []persistence.SourceDefinition) *config.Config {
	kinds := make(map[string]string, len(stored))
	for _, d := range stored {
		if d.Origin != persistence.OriginConfig {
			continue
		}
		if kind, err := d.Kind(); err == nil {
			kinds[d.Name] = kind
		}
	}
	out := *cfg
	out.Sources = make([]config.SourceConfig, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		if kind, ok := kinds[s.Name]; ok && kind == string(s.GetType()) {
			out.Sources = append(out.Sources, s)
			continue
		}
		slog.Warn("Not applying config claims to a source whose config definition was not applied", "source", s.Name)
	}
	return &out
}

// normalizeGitRefs keeps one ref when a git spec names several, in the order
// v1.5.4's git client checked them (commit, then branch, then tag), so the
// definition can be stored. It reports whether the spec was ambiguous.
func normalizeGitRefs(g persistence.GitSpec) (persistence.GitSpec, bool) {
	refs := 0
	for _, ref := range []string{g.Branch, g.Tag, g.Commit} {
		if ref != "" {
			refs++
		}
	}
	if refs <= 1 {
		return g, false
	}
	switch {
	case g.Commit != "":
		g.Branch, g.Tag = "", ""
	case g.Branch != "":
		g.Tag = ""
	}
	return g, true
}

// gitSpec maps a config git block to the library spec.
func gitSpec(g *config.GitConfig) persistence.GitSpec {
	spec := persistence.GitSpec{Repository: g.Repository, Branch: g.Branch, Tag: g.Tag, Commit: g.Commit, Path: g.Path}
	if g.Auth != nil {
		spec.Auth = &persistence.GitAuth{Username: g.Auth.Username, PasswordFile: g.Auth.PasswordFile}
	}
	return spec
}

// sameGitSpec compares two git specs by value.
func sameGitSpec(a, b persistence.GitSpec) bool {
	if (a.Auth == nil) != (b.Auth == nil) || (a.Auth != nil && *a.Auth != *b.Auth) {
		return false
	}
	a.Auth, b.Auth = nil, nil
	return a == b
}

// dedupeRegistrySources removes repeated source names from a config registry.
// v1.5.4 linked each occurrence in turn and a repeat overwrote the earlier
// link's position, so each source keeps the position of its last occurrence.
func dedupeRegistrySources(registry string, sources []string) []string {
	last := make(map[string]int, len(sources))
	for i, name := range sources {
		last[name] = i
	}
	if len(last) == len(sources) {
		return sources
	}
	out := make([]string, 0, len(last))
	for i, name := range sources {
		if last[name] == i {
			out = append(out, name)
		}
	}
	slog.Warn("Registry lists a source more than once; as in v1.5.4, each source keeps the position of its last listing",
		"registry", registry, "sources", sources, "effective_sources", out)
	return out
}
