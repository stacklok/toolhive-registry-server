package cedar

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cedargo "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
	"github.com/stretchr/testify/require"
)

const (
	permitEngineering = `permit(principal is Registry::Principal, action == Registry::Action::"catalog.read", resource == Registry::View::"engineering");`
	// overflowPolicy validates strictly but fails at evaluation with an
	// integer overflow, which Cedar reports as a diagnostic error.
	overflowPolicy = `permit(principal, action, resource) when { 9223372036854775807 + 1 > 0 };`
)

var allActions = []Action{
	CatalogRead, MetadataRead, SourcesRead, SourcesWrite, SourcesDelete,
	ViewsRead, ViewsWrite, ViewsDelete, EntriesPublish, EntriesDelete,
}

func resourceFor(action Action) types.EntityUID {
	if action == CatalogRead {
		return types.EntityUID{Type: viewType, ID: "engineering"}
	}
	return instance
}

func anonymousPrincipal(t *testing.T) types.Entity {
	t.Helper()
	p, err := principal(Identity{Anonymous: true}, nil)
	require.NoError(t, err)
	return p
}

func writePolicy(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.cedar")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadDefaultPolicy(t *testing.T) {
	t.Parallel()
	engine, err := Load("")
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(DefaultPolicy))), engine.Revision())

	authenticated, err := principal(Identity{Issuer: "https://id.example.org", Subject: "user",
		Claims: map[string]any{"iss": "https://id.example.org", "sub": "user"}}, nil)
	require.NoError(t, err)
	for _, p := range []types.Entity{anonymousPrincipal(t), authenticated} {
		for _, action := range allActions {
			allowed, err := engine.evaluate(p, action, resourceFor(action))
			require.NoError(t, err)
			require.True(t, allowed, "default policy must permit %s", action)
		}
	}
}

func TestLoadPolicyFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		policy string
		valid  bool
	}{
		{name: "valid", policy: permitEngineering, valid: true},
		{name: "empty", policy: "", valid: false},
		{name: "only comments", policy: "// nothing\n/* nothing */", valid: false},
		{name: "syntax error", policy: "permit(", valid: false},
		{name: "unknown action", policy: `permit(principal, action == Registry::Action::"root", resource);`, valid: false},
		{name: "catalog.read on instance", policy: `permit(principal, action == Registry::Action::"catalog.read", resource == Registry::Instance::"catalog");`, valid: false},
		{name: "type error", policy: strings.TrimSuffix(permitEngineering, ";") + ` when { principal.authenticated == "yes" };`, valid: false},
		{name: "unknown attribute", policy: strings.TrimSuffix(permitEngineering, ";") + ` when { principal.password == "x" };`, valid: false},
		{name: "unguarded optional attribute", policy: strings.TrimSuffix(permitEngineering, ";") + ` when { principal.groups.contains("readers") };`, valid: false},
		{name: "guarded optional attribute", policy: strings.TrimSuffix(permitEngineering, ";") + ` when { principal has groups && principal.groups.contains("readers") };`, valid: true},
		{name: "over size limit", policy: strings.Repeat(" ", MaxPolicyBytes+1), valid: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine, err := Load(writePolicy(t, tc.policy))
			if !tc.valid {
				require.Error(t, err)
				require.Nil(t, engine)
				return
			}
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(tc.policy))), engine.Revision())
		})
	}
}

func TestLoadPathRules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.cedar")
	require.NoError(t, os.WriteFile(target, []byte(permitEngineering), 0o600))
	link := filepath.Join(dir, "link.cedar")
	require.NoError(t, os.Symlink(target, link))
	dangling := filepath.Join(dir, "dangling.cedar")
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), dangling))
	dirLink := filepath.Join(dir, "dir-link")
	require.NoError(t, os.Symlink(dir, dirLink))

	tests := []struct {
		name  string
		path  string
		valid bool
	}{
		{name: "relative path", path: "policy.cedar", valid: false},
		{name: "dot relative path", path: "./" + filepath.Base(target), valid: false},
		{name: "missing file", path: filepath.Join(dir, "missing"), valid: false},
		{name: "directory", path: dir, valid: false},
		{name: "symlink to regular file", path: link, valid: true},
		{name: "dangling symlink", path: dangling, valid: false},
		{name: "symlink to directory", path: dirLink, valid: false},
		{name: "absolute regular file", path: target, valid: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine, err := Load(tc.path)
			if tc.valid {
				require.NoError(t, err)
				require.NotNil(t, engine)
				return
			}
			require.Error(t, err)
			require.Nil(t, engine)
		})
	}
}

func TestLoadErrorsDoNotEchoPolicy(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{
		`permit(principal, action, resource) when { SECRET_TOKEN };`,
		`permit(principal, action == Registry::Action::"SECRET_TOKEN", resource);`,
		`permit(principal, action, resource) when { principal.SECRET_TOKEN == "x" };`,
	} {
		_, err := Parse([]byte(policy))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "SECRET_TOKEN")
		_, err = Load(writePolicy(t, policy))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "SECRET_TOKEN")
	}
}

func TestLoadSizeLimitBoundary(t *testing.T) {
	t.Parallel()
	// A policy padded with trailing whitespace to exactly the limit is accepted.
	exact := permitEngineering + strings.Repeat(" ", MaxPolicyBytes-len(permitEngineering))
	_, err := Load(writePolicy(t, exact))
	require.NoError(t, err)
	_, err = Load(writePolicy(t, exact+" "))
	require.Error(t, err)
	_, err = Parse([]byte(exact + " "))
	require.Error(t, err)
}

func TestRevisionStability(t *testing.T) {
	t.Parallel()
	path := writePolicy(t, permitEngineering)
	first, err := Load(path)
	require.NoError(t, err)
	second, err := Parse([]byte(permitEngineering))
	require.NoError(t, err)
	require.Equal(t, first.Revision(), second.Revision(), "same bytes must give the same revision")
	require.True(t, strings.HasPrefix(first.Revision(), "sha256:"))
	require.Len(t, first.Revision(), len("sha256:")+64)

	// The engine is immutable: rewriting the file changes neither the loaded
	// revision nor its decisions; only a reload sees the new policy.
	require.NoError(t, os.WriteFile(path, []byte(`forbid(principal, action, resource);`), 0o600))
	allowed, err := first.evaluate(anonymousPrincipal(t), CatalogRead, resourceFor(CatalogRead))
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, second.Revision(), first.Revision())

	reloaded, err := Load(path)
	require.NoError(t, err)
	require.NotEqual(t, first.Revision(), reloaded.Revision())
	allowed, err = reloaded.evaluate(anonymousPrincipal(t), CatalogRead, resourceFor(CatalogRead))
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestEvaluate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		policy   string
		action   Action
		resource types.EntityUID
		allowed  bool
		err      error
	}{
		{name: "permit", policy: permitEngineering, action: CatalogRead, resource: resourceFor(CatalogRead), allowed: true},
		{name: "other view denied", policy: permitEngineering, action: CatalogRead,
			resource: types.EntityUID{Type: viewType, ID: "finance"}},
		{name: "no matching permit", policy: permitEngineering, action: SourcesWrite, resource: instance},
		{name: "forbid wins", policy: permitEngineering + `forbid(principal, action, resource);`,
			action: CatalogRead, resource: resourceFor(CatalogRead)},
		{name: "custom policy replaces default", policy: permitEngineering, action: EntriesPublish, resource: instance},
		{name: "runtime overflow fails closed", policy: overflowPolicy, action: CatalogRead,
			resource: resourceFor(CatalogRead), err: ErrEvaluation},
		{name: "unknown action", policy: DefaultPolicy, action: "root", resource: instance, err: ErrEvaluation},
		{name: "catalog.read on instance", policy: DefaultPolicy, action: CatalogRead, resource: instance, err: ErrEvaluation},
		{name: "catalog.read on empty view", policy: DefaultPolicy, action: CatalogRead,
			resource: types.EntityUID{Type: viewType}, err: ErrEvaluation},
		{name: "instance action on view", policy: DefaultPolicy, action: SourcesRead,
			resource: resourceFor(CatalogRead), err: ErrEvaluation},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine, err := Parse([]byte(tc.policy))
			require.NoError(t, err)
			allowed, err := engine.evaluate(anonymousPrincipal(t), tc.action, tc.resource)
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.allowed, allowed)
		})
	}
}

func TestEvaluateAllowWithDiagnosticFailsClosed(t *testing.T) {
	t.Parallel()
	// Parse rejects this ill-typed policy, so build the engine directly to
	// prove that a raw Allow with a diagnostic error is still a failure.
	ps, err := cedargo.NewPolicySetFromBytes("test",
		[]byte(permitEngineering+`permit(principal, action, resource) when { principal.missing == true };`))
	require.NoError(t, err)
	p := anonymousPrincipal(t)
	decision, diagnostic := cedargo.Authorize(ps, types.EntityMap{p.UID: p}, cedargo.Request{
		Principal: p.UID, Action: types.EntityUID{Type: actionType, ID: "catalog.read"}, Resource: resourceFor(CatalogRead),
	})
	require.Equal(t, cedargo.Allow, decision)
	require.NotEmpty(t, diagnostic.Errors)

	allowed, err := (&Engine{policies: ps}).evaluate(p, CatalogRead, resourceFor(CatalogRead))
	require.ErrorIs(t, err, ErrEvaluation)
	require.False(t, allowed)

	allowed, err = (*Engine)(nil).evaluate(p, CatalogRead, resourceFor(CatalogRead))
	require.ErrorIs(t, err, ErrEvaluation)
	require.False(t, allowed)
}

func TestActionValid(t *testing.T) {
	t.Parallel()
	for _, action := range allActions {
		require.True(t, action.Valid(), action)
	}
	for _, action := range []Action{"", "root", "Catalog.Read", "catalog.read "} {
		require.False(t, action.Valid(), action)
	}
}
