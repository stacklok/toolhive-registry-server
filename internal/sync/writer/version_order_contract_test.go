package writer

import (
	"context"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"
	"github.com/stretchr/testify/require"
)

// TestLatestSpellingOrder verifies latest is independent of snapshot ordering
// for all three entry kinds.
func TestLatestSpellingOrder(t *testing.T) {
	t.Parallel()
	pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()
	source := createTestRegistry(t, pool, "order-source")
	w, err := NewDBSyncWriter(pool, testMaxMetaSize)
	require.NoError(t, err)
	latest := func(kind string) string {
		t.Helper()
		var version string
		require.NoError(t, pool.QueryRow(ctx, `SELECT v.version FROM latest_entry_version l JOIN entry_version v ON v.id=l.latest_version_id JOIN registry_entry e ON e.id=v.entry_id WHERE l.source_id=$1 AND e.entry_type=$2`, source.sourceID, kind).Scan(&version))
		return version
	}
	for _, tc := range []struct {
		name     string
		versions []string
		want     string
	}{
		{"semver equal plain first", []string{"1.0.0", "v1.0.0"}, "v1.0.0"},
		{"semver equal prefixed first", []string{"v1.0.0", "1.0.0"}, "v1.0.0"},
		{"mixed parseability plain first", []string{"1.0.0", "custom"}, "1.0.0"},
		{"mixed parseability custom first", []string{"custom", "1.0.0"}, "1.0.0"},
	} {
		reg := createTestUpstreamRegistry(nil)
		for _, version := range tc.versions {
			reg.Data.Servers = append(reg.Data.Servers, createTestServer("example.com/order", version))
			reg.Data.Skills = append(reg.Data.Skills, createTestSkill("com.example", "skill-order", version))
			reg.Data.Plugins = append(reg.Data.Plugins, createTestPlugin("com.example", "plugin-order", version))
		}
		require.NoError(t, w.Store(ctx, "order-source", reg))
		for _, kind := range []string{"MCP", "SKILL", "PLUGIN"} {
			require.Equal(t, tc.want, latest(kind), kind)
		}
		// Retain only the other spelling. The original version strings remain
		// exact identifiers, even when semver considers them equivalent.
		replacement := createTestUpstreamRegistry(nil)
		remaining := tc.versions[0]
		if remaining == tc.want {
			remaining = tc.versions[1]
		}
		replacement.Data.Servers = []upstreamv0.ServerJSON{createTestServer("example.com/order", remaining)}
		replacement.Data.Skills = []toolhivetypes.Skill{createTestSkill("com.example", "skill-order", remaining)}
		replacement.Data.Plugins = []toolhivetypes.Plugin{createTestPlugin("com.example", "plugin-order", remaining)}
		require.NoError(t, w.Store(ctx, "order-source", replacement))
		for _, kind := range []string{"MCP", "SKILL", "PLUGIN"} {
			require.Equal(t, remaining, latest(kind), kind)
		}
	}
}
