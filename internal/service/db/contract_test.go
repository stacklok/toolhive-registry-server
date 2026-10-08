package database

import (
	"context"
	"fmt"
	"testing"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	toolhivetypes "github.com/stacklok/toolhive-core/registry/types"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
)

// TestCurrentLimitationVersionScopedSourceSelection records the observed
// row/version-level winner, NOT the intended whole-name priority contract.
// Correct this deliberately before exposing selection as a catalog interface.
func TestCurrentLimitationVersionScopedSourceSelection(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestService(t)
	t.Cleanup(cleanup)
	svc.skipAuthz = true
	setupShadowingRegistry(t, svc)
	ctx := context.Background()
	w, err := writer.NewDBSyncWriter(svc.pool, 65536)
	require.NoError(t, err)
	makeSnapshot := func(origin string, versions []string) *toolhivetypes.UpstreamRegistry {
		reg := &toolhivetypes.UpstreamRegistry{}
		for _, version := range versions {
			reg.Data.Servers = append(reg.Data.Servers, upstreamv0.ServerJSON{Name: "com.example/alpha", Version: version, Description: origin, Title: origin})
			reg.Data.Skills = append(reg.Data.Skills, toolhivetypes.Skill{Namespace: "com.example", Name: "skill", Version: version, Description: origin, Title: origin})
			reg.Data.Plugins = append(reg.Data.Plugins, toolhivetypes.Plugin{Namespace: "com.example", Name: "plugin", Version: version, Description: origin, Title: origin})
		}
		return reg
	}
	require.NoError(t, w.Store(ctx, "claims-src-a", makeSnapshot("A", []string{"2.0.0", "3.0.0"})))
	b := makeSnapshot("B", []string{"1.0.0", "2.0.0"})
	b.Data.Servers = append(b.Data.Servers, upstreamv0.ServerJSON{Name: "com.example/beta", Version: "1.0.0", Description: "B", Title: "B"})
	require.NoError(t, w.Store(ctx, "claims-src-b", b))
	base := []service.Option{service.WithRegistryName("claims-registry")}
	for _, tc := range []struct {
		kind string
		get  func(string) (string, error)
		list func(string) ([]string, error)
	}{
		{"server", func(version string) (string, error) {
			row, err := svc.GetServerVersion(ctx, service.WithRegistryName("claims-registry"), service.WithName("com.example/alpha"), service.WithVersion(version))
			if err != nil {
				return "", err
			}
			return row.Description, nil
		}, func(search string) ([]string, error) {
			opts := append([]service.Option{}, base...)
			opts = append(opts, service.WithLimit(10))
			if search != "" {
				opts = append(opts, service.WithSearch(search))
			}
			result, err := svc.ListServers(ctx, opts...)
			if err != nil {
				return nil, err
			}
			out := make([]string, 0, len(result.Servers))
			for _, row := range result.Servers {
				out = append(out, fmt.Sprintf("%s:%s:%s", row.Name, row.Version, row.Description))
			}
			return out, nil
		}},
		{"skill", func(version string) (string, error) {
			row, err := svc.GetSkillVersion(ctx, service.WithRegistryName("claims-registry"), service.WithNamespace("com.example"), service.WithName("skill"), service.WithVersion(version))
			if err != nil {
				return "", err
			}
			return row.Description, nil
		}, func(search string) ([]string, error) {
			opts := append([]service.Option{}, base...)
			opts = append(opts, service.WithLimit(10))
			if search != "" {
				opts = append(opts, service.WithSearch(search))
			}
			result, err := svc.ListSkills(ctx, opts...)
			if err != nil {
				return nil, err
			}
			out := make([]string, 0, len(result.Skills))
			for _, row := range result.Skills {
				out = append(out, fmt.Sprintf("%s:%s:%s", row.Name, row.Version, row.Description))
			}
			return out, nil
		}},
		{"plugin", func(version string) (string, error) {
			row, err := svc.GetPluginVersion(ctx, service.WithRegistryName("claims-registry"), service.WithNamespace("com.example"), service.WithName("plugin"), service.WithVersion(version))
			if err != nil {
				return "", err
			}
			return row.Description, nil
		}, func(search string) ([]string, error) {
			opts := append([]service.Option{}, base...)
			opts = append(opts, service.WithLimit(10))
			if search != "" {
				opts = append(opts, service.WithSearch(search))
			}
			result, err := svc.ListPlugins(ctx, opts...)
			if err != nil {
				return nil, err
			}
			out := make([]string, 0, len(result.Plugins))
			for _, row := range result.Plugins {
				out = append(out, fmt.Sprintf("%s:%s:%s", row.Name, row.Version, row.Description))
			}
			return out, nil
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			for version, want := range map[string]string{"1.0.0": "B", "2.0.0": "A", "latest": "A"} {
				got, err := tc.get(version)
				require.NoError(t, err)
				require.Equal(t, want, got, version)
			}
			rows, err := tc.list("")
			require.NoError(t, err)
			require.Contains(t, rows, fmt.Sprintf("%s:1.0.0:B", map[string]string{"server": "com.example/alpha", "skill": "skill", "plugin": "plugin"}[tc.kind]))
			rows, err = tc.list("B")
			require.NoError(t, err)
			require.NotEmpty(t, rows)
			for _, row := range rows {
				require.Contains(t, row, ":B")
			}
			if tc.kind == "server" {
				var cursor string
				var pages []string
				for {
					opts := []service.Option{service.WithRegistryName("claims-registry"), service.WithLimit(1)}
					if cursor != "" {
						opts = append(opts, service.WithCursor(cursor))
					}
					result, listErr := svc.ListServers(ctx, opts...)
					require.NoError(t, listErr)
					for _, server := range result.Servers {
						pages = append(pages, server.Name+":"+server.Version+":"+server.Description)
					}
					if result.NextCursor == "" {
						break
					}
					require.NotEqual(t, cursor, result.NextCursor)
					cursor = result.NextCursor
					require.Less(t, len(pages), 10)
				}
				require.Equal(t, []string{"com.example/alpha:1.0.0:B", "com.example/alpha:2.0.0:A", "com.example/alpha:3.0.0:A", "com.example/beta:1.0.0:B"}, pages)
			}
		})
	}
}
func TestRealServiceVersionHistoryBoundaries(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestService(t)
	t.Cleanup(cleanup)
	svc.skipAuthz = true
	setupShadowingRegistry(t, svc)
	ctx := context.Background()
	w, err := writer.NewDBSyncWriter(svc.pool, 65536)
	require.NoError(t, err)
	reg := &toolhivetypes.UpstreamRegistry{}
	for _, count := range []int{999, 1000, 1001} {
		name := fmt.Sprintf("com.example/history-%d", count)
		for i := 0; i < count; i++ {
			reg.Data.Servers = append(reg.Data.Servers, upstreamv0.ServerJSON{Name: name, Version: fmt.Sprintf("%04d", i), Title: name})
		}
	}
	for _, count := range []int{29, 30, 31} {
		name := fmt.Sprintf("history-%d", count)
		for i := 0; i < count; i++ {
			version := fmt.Sprintf("%04d", i)
			reg.Data.Skills = append(reg.Data.Skills, toolhivetypes.Skill{Namespace: "com.example", Name: name, Version: version, Title: name})
			reg.Data.Plugins = append(reg.Data.Plugins, toolhivetypes.Plugin{Namespace: "com.example", Name: name, Version: version, Title: name})
		}
	}
	require.NoError(t, w.Store(ctx, "claims-src-a", reg))
	shadowed := &toolhivetypes.UpstreamRegistry{}
	for _, count := range []int{999, 1000, 1001} {
		shadowed.Data.Servers = append(shadowed.Data.Servers, upstreamv0.ServerJSON{Name: fmt.Sprintf("com.example/history-%d", count), Version: "0000", Title: "lower-priority"})
	}
	for _, count := range []int{29, 30, 31} {
		name := fmt.Sprintf("history-%d", count)
		shadowed.Data.Skills = append(shadowed.Data.Skills, toolhivetypes.Skill{Namespace: "com.example", Name: name, Version: "0000", Title: "lower-priority"})
		shadowed.Data.Plugins = append(shadowed.Data.Plugins, toolhivetypes.Plugin{Namespace: "com.example", Name: name, Version: "0000", Title: "lower-priority"})
	}
	require.NoError(t, w.Store(ctx, "claims-src-b", shadowed))
	for _, count := range []int{999, 1000, 1001} {
		name := fmt.Sprintf("com.example/history-%d", count)
		versions, listErr := svc.ListServerVersions(ctx, service.WithRegistryName("claims-registry"), service.WithName(name), service.WithLimit(1000))
		require.NoError(t, listErr)
		want := min(count, 999) // CURRENT LIMITATION: SQL limits before cross-source dedup.
		require.Equal(t, want, len(versions))
		seen := make(map[string]bool, len(versions))
		for _, row := range versions {
			require.Equal(t, name, row.Name)
			require.Equal(t, name, row.Title)
			require.False(t, seen[row.Version])
			seen[row.Version] = true
		}
		for i := 0; i < want; i++ {
			require.True(t, seen[fmt.Sprintf("%04d", i)])
		}
	}
	for _, count := range []int{29, 30, 31} {
		name := fmt.Sprintf("history-%d", count)
		for _, kind := range []string{"skill", "plugin"} {
			t.Run(fmt.Sprintf("%s-%d", kind, count), func(t *testing.T) {
				t.Parallel()
				opts := []service.Option{service.WithRegistryName("claims-registry"), service.WithNamespace("com.example"), service.WithName(name)}
				var versions []string
				var cursor string
				pageSizes := []int{count}
				if count == 31 {
					pageSizes = []int{30, 1}
				}
				for page, size := range pageSizes {
					before := len(versions)
					pageOpts := append([]service.Option{}, opts...)
					if cursor != "" {
						pageOpts = append(pageOpts, service.WithCursor(cursor))
					}
					var next string
					if kind == "skill" {
						result, listErr := svc.ListSkills(ctx, pageOpts...)
						require.NoError(t, listErr)
						for _, row := range result.Skills {
							require.Equal(t, name, row.Name)
							require.Equal(t, name, row.Title)
							versions = append(versions, row.Version)
						}
						next = result.NextCursor
					} else {
						result, listErr := svc.ListPlugins(ctx, pageOpts...)
						require.NoError(t, listErr)
						for _, row := range result.Plugins {
							require.Equal(t, name, row.Name)
							require.Equal(t, name, row.Title)
							versions = append(versions, row.Version)
						}
						next = result.NextCursor
					}
					require.Equal(t, size, len(versions)-before, "page %d", page+1)
					if page == len(pageSizes)-1 {
						require.Empty(t, next, "final page")
					} else {
						require.NotEmpty(t, next, "first page")
						require.NotEqual(t, cursor, next)
						cursor = next
					}
				}
				require.Len(t, versions, count)
				seen := make(map[string]bool, count)
				for _, version := range versions {
					require.False(t, seen[version])
					seen[version] = true
				}
				for i := 0; i < count; i++ {
					require.True(t, seen[fmt.Sprintf("%04d", i)])
				}
			})
		}
	}
}

func TestManagedExternalOwnershipAllKinds(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestService(t)
	defer cleanup()
	svc.skipAuthz = true
	const registry = "ownership"
	const external = "external-ownership"
	const namespace = "com.example"
	createManagedSourceWithRegistryClaims(t, svc, registry, nil)
	ctx := context.Background()
	q := sqlc.New(svc.pool)
	externalID, err := q.UpsertSource(ctx, sqlc.UpsertSourceParams{Name: external, SourceType: "git", CreationType: sqlc.CreationTypeCONFIG, Syncable: true})
	require.NoError(t, err)
	reg, err := q.GetRegistryByName(ctx, registry)
	require.NoError(t, err)
	require.NoError(t, q.LinkRegistrySource(ctx, sqlc.LinkRegistrySourceParams{RegistryID: reg.ID, SourceID: externalID, Position: 1}))
	w, err := writer.NewDBSyncWriter(svc.pool, 65536)
	require.NoError(t, err)
	snapshot := &toolhivetypes.UpstreamRegistry{}
	for _, version := range []string{"1.0.0", "2.0.0"} {
		snapshot.Data.Servers = append(snapshot.Data.Servers, upstreamv0.ServerJSON{Name: "com.example/owned", Version: version, Description: "external", Title: "external"})
		snapshot.Data.Skills = append(snapshot.Data.Skills, toolhivetypes.Skill{Namespace: namespace, Name: "owned-skill", Version: version, Description: "external", Title: "external"})
		snapshot.Data.Plugins = append(snapshot.Data.Plugins, toolhivetypes.Plugin{Namespace: namespace, Name: "owned-plugin", Version: version, Description: "external", Title: "external"})
	}
	require.NoError(t, w.Store(ctx, external, snapshot))
	// Every attempt to delete an exclusively external version must fail.
	require.Error(t, svc.DeleteServerVersion(ctx, service.WithName("com.example/owned"), service.WithVersion("2.0.0")))
	require.Error(t, svc.DeleteSkillVersion(ctx, service.WithNamespace(namespace), service.WithName("owned-skill"), service.WithVersion("2.0.0")))
	require.Error(t, svc.DeletePluginVersion(ctx, service.WithNamespace(namespace), service.WithName("owned-plugin"), service.WithVersion("2.0.0")))
	for _, version := range []string{"1.0.0", "3.0.0"} {
		server := &upstreamv0.ServerJSON{Name: "com.example/owned", Version: version, Title: "managed", Description: "managed"}
		_, err := svc.PublishServerVersion(ctx, service.WithServerData(server))
		require.NoError(t, err)
		_, err = svc.PublishSkill(ctx, &service.Skill{Namespace: namespace, Name: "owned-skill", Version: version, Title: "managed", Description: "managed"})
		require.NoError(t, err)
		_, err = svc.PublishPlugin(ctx, &service.Plugin{Namespace: namespace, Name: "owned-plugin", Version: version, Title: "managed", Description: "managed"})
		require.NoError(t, err)
	}
	// Removing a non-latest managed plugin must not move the 3.0 pointer.
	require.NoError(t, svc.DeletePluginVersion(ctx, service.WithNamespace(namespace), service.WithName("owned-plugin"), service.WithVersion("1.0.0")))
	latest, err := svc.GetPluginVersion(ctx, service.WithRegistryName(registry), service.WithNamespace(namespace), service.WithName("owned-plugin"), service.WithVersion("latest"))
	require.NoError(t, err)
	require.Equal(t, "3.0.0", latest.Version)
	require.NoError(t, svc.DeleteServerVersion(ctx, service.WithName("com.example/owned"), service.WithVersion("1.0.0")))
	require.NoError(t, svc.DeleteSkillVersion(ctx, service.WithNamespace(namespace), service.WithName("owned-skill"), service.WithVersion("1.0.0")))
	// The external row, its payload and latest pointer are untouched even when
	// a managed source publishes and deletes an identical name and version.
	for _, kind := range []string{"MCP", "SKILL", "PLUGIN"} {
		var count int
		require.NoError(t, svc.pool.QueryRow(ctx, `SELECT count(*) FROM registry_entry e JOIN entry_version v ON v.entry_id=e.id WHERE e.source_id=$1 AND e.entry_type=$2 AND v.description='external'`, externalID, kind).Scan(&count))
		require.Equal(t, 2, count, kind)
		var version string
		name := map[string]string{"MCP": "com.example/owned", "SKILL": "owned-skill", "PLUGIN": "owned-plugin"}[kind]
		require.NoError(t, svc.pool.QueryRow(ctx, `SELECT v.version FROM latest_entry_version l JOIN entry_version v ON v.id=l.latest_version_id WHERE l.source_id=$1 AND l.name=$2`, externalID, name).Scan(&version))
		require.Equal(t, "2.0.0", version, kind)
	}
}

func TestManagedMixedAndTieLatestAllKinds(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestService(t)
	defer cleanup()
	const registry = "managed-order"
	const namespace = "com.example"
	createManagedSourceWithRegistry(t, svc, registry)
	ctx := context.Background()
	for _, tc := range []struct {
		kind, name string
		publish    func(string) error
		delete     func(string) error
	}{
		{"MCP", "com.example/order", func(v string) error {
			_, err := svc.PublishServerVersion(ctx, service.WithServerData(&upstreamv0.ServerJSON{Name: "com.example/order", Version: v}))
			return err
		}, func(v string) error {
			return svc.DeleteServerVersion(ctx, service.WithName("com.example/order"), service.WithVersion(v))
		}},
		{"SKILL", "order-skill", func(v string) error {
			_, err := svc.PublishSkill(ctx, &service.Skill{Namespace: namespace, Name: "order-skill", Version: v, Title: "Skill"})
			return err
		}, func(v string) error {
			return svc.DeleteSkillVersion(ctx, service.WithNamespace(namespace), service.WithName("order-skill"), service.WithVersion(v))
		}},
		{"PLUGIN", "order-plugin", func(v string) error {
			_, err := svc.PublishPlugin(ctx, &service.Plugin{Namespace: namespace, Name: "order-plugin", Version: v, Title: "Plugin"})
			return err
		}, func(v string) error {
			return svc.DeletePluginVersion(ctx, service.WithNamespace(namespace), service.WithName("order-plugin"), service.WithVersion(v))
		}},
	} {
		for _, v := range []string{"v1.0.0", "custom", "1.0.0", "1.0.0+z"} {
			require.NoError(t, tc.publish(v), tc.kind+" "+v)
		}
		latest := func(want string) {
			t.Helper()
			var got string
			require.NoError(t, svc.pool.QueryRow(ctx, `SELECT l.version FROM latest_entry_version l JOIN source s ON s.id=l.source_id WHERE s.name=$1 AND l.name=$2`, registry, tc.name).Scan(&got))
			require.Equal(t, want, got, tc.kind)
		}
		latest("v1.0.0")
		for _, step := range []struct{ remove, want string }{
			{"custom", "v1.0.0"},
			{"v1.0.0", "1.0.0+z"},
			{"1.0.0+z", "1.0.0"},
		} {
			require.NoError(t, tc.delete(step.remove), tc.kind+" "+step.remove)
			latest(step.want)
		}
	}
}

func TestPluginManagedLatestAfterDelete(t *testing.T) {
	t.Parallel()
	svc, cleanup := setupTestService(t)
	defer cleanup()
	const registry = "plugin-contract"
	const namespace = "com.example"
	const name = "test-plugin"
	createManagedSourceWithRegistry(t, svc, registry)
	ctx := context.Background()
	for _, version := range []string{"1.0.0", "2.0.0"} {
		_, err := svc.PublishPlugin(ctx, &service.Plugin{Namespace: namespace, Name: name, Version: version, Title: "Plugin"})
		require.NoError(t, err)
	}
	getLatest := func() (*service.Plugin, error) {
		return svc.GetPluginVersion(ctx, service.WithRegistryName(registry), service.WithNamespace(namespace), service.WithName(name), service.WithVersion("latest"))
	}
	latest, err := getLatest()
	require.NoError(t, err)
	require.Equal(t, "2.0.0", latest.Version)
	require.NoError(t, svc.DeletePluginVersion(ctx, service.WithNamespace(namespace), service.WithName(name), service.WithVersion("2.0.0")))
	latest, err = getLatest()
	require.NoError(t, err)
	require.Equal(t, "1.0.0", latest.Version)
	require.NoError(t, svc.DeletePluginVersion(ctx, service.WithNamespace(namespace), service.WithName(name), service.WithVersion("1.0.0")))
	_, err = getLatest()
	require.ErrorIs(t, err, service.ErrNotFound)
}
