package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	upstream "github.com/modelcontextprotocol/registry/pkg/api/v0"
	upstreammodel "github.com/modelcontextprotocol/registry/pkg/model"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence/conformance"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

func TestPublishConcurrentDeleteAndCancellation(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	store, err := postgres.NewEntries(t.Context(), pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	makeEntry := func(description string) persistence.Entry {
		return persistence.Entry{Kind: persistence.SkillKind, Name: "item", Version: "1", Skill: &model.Skill{
			Namespace: "com.example", Name: "item", Version: "1", Description: description,
		}}
	}
	for i := range 12 {
		description := fmt.Sprintf("publication-%d", i)
		deleted := make(chan error, 1)
		go func() {
			// The delete may precede the publish; either order must not alter the returned version.
			deleted <- store.DeleteManaged(ctx, persistence.SkillKind, "item", "1")
		}()
		result, err := store.Publish(ctx, makeEntry(description))
		if deletionErr := <-deleted; deletionErr != nil && !errors.Is(deletionErr, persistence.ErrNotFound) {
			t.Fatal(deletionErr)
		}
		if err != nil || result.ID == "" || result.SourceID != managed.ID || result.Kind != persistence.SkillKind ||
			result.Name != "item" || result.Version != "1" || result.Skill == nil || result.Skill.Description != description ||
			result.Skill.ID != result.ID || !result.IsLatest || !result.Skill.IsLatest {
			t.Fatalf("publish/delete iteration %d: %+v / %v", i, result, err)
		}
		if err = store.DeleteManaged(ctx, persistence.SkillKind, "item", "1"); err != nil && !errors.Is(err, persistence.ErrNotFound) {
			t.Fatal(err)
		}
	}
	lock, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	_, err = lock.Exec(ctx, `SELECT pg_advisory_lock(910,1)`)
	if err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { _, e := store.Publish(cancelCtx, makeEntry("canceled")); result <- e }()
	deadline := time.After(5 * time.Second)
	for {
		var waiting bool
		err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted
			AND classid=910 AND objid=1 AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-deadline:
			t.Fatal("publish did not reach the advisory lock")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if err = <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publish: %v", err)
	}
	_, err = lock.Exec(ctx, `SELECT pg_advisory_unlock(910,1)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetEntry(ctx, managed.ID, persistence.SkillKind, "item", persistence.VersionSelector{Exact: "1"}); !errors.Is(err, persistence.ErrNotFound) {
		t.Fatalf("canceled publication persisted: %v", err)
	}
}

func TestLongLegacyPositionCursor(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	store, err := postgres.NewEntries(t.Context(), pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	source, err := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "legacy-cursor", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	var entryID string
	err = pool.QueryRow(ctx, `INSERT INTO registry_entry(source_id,entry_type,name) VALUES($1,'SKILL','item') RETURNING id::text`, source.ID).Scan(&entryID)
	if err != nil {
		t.Fatal(err)
	}
	version := strings.Repeat("<", 800)
	for _, v := range []string{version, version + "a"} {
		var id string
		err = pool.QueryRow(ctx, `INSERT INTO entry_version(entry_id,name,version,description)
 VALUES($1,'item',$2,'legacy') RETURNING id::text`, entryID, v).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO skill(version_id,namespace) VALUES($1,'com.example')`, id)
		if err != nil {
			t.Fatal(err)
		}
	}
	opts := persistence.ListOptions{SourceID: source.ID, Kind: persistence.SkillKind, Limit: 1}
	page, err := store.ListEntries(ctx, opts)
	if !errors.Is(err, persistence.ErrInvalid) || page.NextCursor != "" {
		t.Fatalf("long cursor first page: %+v: %v", page, err)
	}
}

func TestEntriesConformance(t *testing.T) {
	t.Parallel()
	conformance.RunEntries(t, func(t *testing.T) (persistence.Sources, persistence.Entries) {
		t.Helper()
		d, pool := newBackend(t)
		entries, e := postgres.NewEntries(t.Context(), pool, 65536)
		if e != nil {
			t.Fatal(e)
		}
		return d, entries
	})
	conformance.RunEntriesMulti(t, func(t *testing.T) (persistence.Sources, persistence.Entries, persistence.Entries) {
		t.Helper()
		d, pool := newBackend(t)
		a, e := postgres.NewEntries(t.Context(), pool, 65536)
		if e != nil {
			t.Fatal(e)
		}
		otherPool, e := pgxpool.New(t.Context(), pool.Config().ConnString())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(otherPool.Close)
		b, e := postgres.NewEntries(t.Context(), otherPool, 65536)
		if e != nil {
			t.Fatal(e)
		}
		return d, a, b
	})
}

//nolint:paralleltest,tparallel // Subtests share one database and replace the same trigger serially.
func TestSnapshotLateFailureRollsBackAllKinds(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	store, e := postgres.NewEntries(t.Context(), pool, 65536)
	if e != nil {
		t.Fatal(e)
	}
	source, e := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "rollback", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	snapshot := func(version string) model.Snapshot {
		var r model.Snapshot
		r.Data.Servers = []upstream.ServerJSON{{Schema: "https://example.org/server.json", Name: "com.example/server", Version: version, Description: "server"}}
		r.Data.Skills = []thvregistry.Skill{{Namespace: "com.example", Name: "skill", Version: version, Description: "skill"}}
		r.Data.Plugins = []thvregistry.Plugin{{Namespace: "com.example", Name: "plugin", Version: version, Description: "plugin"}}
		return r
	}
	if e = store.ReplaceSnapshot(t.Context(), source, snapshot("1.0.0")); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		cancel bool
	}{
		{name: "late error"}, {name: "active cancellation", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.cancel {
				_, err = pool.Exec(t.Context(), `CREATE OR REPLACE FUNCTION test_fail_plugin() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(10); RETURN NEW; END $$`)
			} else {
				_, err = pool.Exec(t.Context(), `CREATE OR REPLACE FUNCTION test_fail_plugin() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test failure' USING ERRCODE='P0001'; END $$`)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = pool.Exec(t.Context(), `CREATE TRIGGER test_plugin_failure BEFORE INSERT ON plugin FOR EACH ROW EXECUTE FUNCTION test_fail_plugin()`)
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			cancel := func() {}
			if tc.cancel {
				ctx, cancel = context.WithTimeout(ctx, 3*time.Second)
			}
			err = store.ReplaceSnapshot(ctx, source, snapshot("2.0.0"))
			cancel()
			if tc.cancel {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected canceled write: %v", err)
				}
			} else if !errors.Is(err, persistence.ErrUnavailable) {
				t.Fatalf("expected classified late error: %v", err)
			}
			_, err = pool.Exec(t.Context(), `DROP TRIGGER test_plugin_failure ON plugin`)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range []struct {
				kind persistence.EntryKind
				name string
			}{{persistence.ServerKind, "com.example/server"}, {persistence.SkillKind, "skill"}, {persistence.PluginKind, "plugin"}} {
				version, e := store.GetEntry(t.Context(), source.ID, item.kind, item.name, persistence.VersionSelector{Latest: true})
				if e != nil || version.Version != "1.0.0" {
					t.Fatalf("%s changed: %+v / %v", item.kind, version, e)
				}
				_, e = store.GetEntry(t.Context(), source.ID, item.kind, item.name, persistence.VersionSelector{Exact: "2.0.0"})
				if !errors.Is(e, persistence.ErrNotFound) {
					t.Fatalf("%s partial write: %v", item.kind, e)
				}
			}
		})
	}
}

func TestEntryPayloadRoundTrip(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	store, e := postgres.NewEntries(t.Context(), pool, 65536)
	if e != nil {
		t.Fatal(e)
	}
	s, e := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "catalog", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	server := upstream.ServerJSON{Schema: "https://example.org/schema.json", Name: "com.example/catalog", Version: "release", Description: "server", Title: "Catalog"}
	server.Meta = &upstream.ServerMeta{PublisherProvided: map[string]any{"com.example/property": map[string]any{"on": true}}}
	theme := "dark"
	mime := "image/png"
	server.Icons = []upstreammodel.Icon{{Src: "https://example.org/icon.png", MimeType: &mime, Theme: &theme, Sizes: []string{"32x32"}}}
	server.Remotes = []upstreammodel.Transport{{Type: "streamable-http", URL: "https://example.org/mcp", Variables: map[string]upstreammodel.Input{"region": {Description: "region"}}}}
	server.Packages = []upstreammodel.Package{{RegistryType: "npm", Identifier: "package", Version: "1.0.0", Transport: upstreammodel.Transport{Type: "stdio"}, RuntimeArguments: []upstreammodel.Argument{{Type: "named", Name: "--yes"}}}}
	skill := thvregistry.Skill{Namespace: "com.example", Name: "catalog", Version: "1.0.0", Description: "skill", Metadata: map[string]any{"nested": map[string]any{"one": "two"}}, Meta: map[string]any{"extension": "value"},
		Provenance: &thvregistry.Provenance{RepositoryURI: "https://github.com/example/catalog"}, Packages: []thvregistry.SkillPackage{{RegistryType: "oci", Identifier: "example/catalog:1.0.0"}}}
	plugin := thvregistry.Plugin{Namespace: "com.example", Name: "catalog", Version: "1.0.0", Description: "plugin", Metadata: map[string]any{"key": "value"}, Packages: []thvregistry.SkillPackage{{RegistryType: "git", URL: "https://github.com/example/plugin.git", Commit: "abc"}}}
	snapshot := model.Snapshot{}
	snapshot.Data.Servers = []upstream.ServerJSON{server}
	snapshot.Data.Skills = []thvregistry.Skill{skill}
	snapshot.Data.Plugins = []thvregistry.Plugin{plugin}
	if e = store.ReplaceSnapshot(t.Context(), s, snapshot); e != nil {
		t.Fatal(e)
	}
	a, e := store.GetEntry(t.Context(), s.ID, persistence.ServerKind, server.Name, persistence.VersionSelector{Exact: server.Version})
	if e != nil {
		t.Fatal(e)
	}
	if a.Server.Schema != server.Schema || !reflect.DeepEqual(a.Server.Icons, server.Icons) || len(a.Server.Remotes) != 1 || !reflect.DeepEqual(a.Server.Remotes[0].Variables, server.Remotes[0].Variables) || !reflect.DeepEqual(a.Server.Meta, server.Meta) || len(a.Server.Packages) != 1 || len(a.Server.Packages[0].RuntimeArguments) != 1 {
		t.Fatalf("round trip: schema=%t icons=%t remotes=%t meta=%t metaGot=%+v metaWant=%+v", a.Server.Schema == server.Schema, reflect.DeepEqual(a.Server.Icons, server.Icons), reflect.DeepEqual(a.Server.Remotes, server.Remotes), reflect.DeepEqual(a.Server.Meta, server.Meta), a.Server.Meta, server.Meta)
	}
	b, e := store.GetEntry(t.Context(), s.ID, persistence.SkillKind, skill.Name, persistence.VersionSelector{Latest: true})
	if e != nil {
		t.Fatal(e)
	}
	if b.Skill.Provenance == nil || !reflect.DeepEqual(b.Skill.Metadata, skill.Metadata) || !reflect.DeepEqual(b.Skill.Meta, skill.Meta) || len(b.Skill.Packages) != 1 {
		t.Fatalf("skill round trip: %+v", b.Skill)
	}
	c, e := store.GetEntry(t.Context(), s.ID, persistence.PluginKind, plugin.Name, persistence.VersionSelector{Latest: true})
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(c.Plugin.Metadata, plugin.Metadata) || len(c.Plugin.Packages) != 1 {
		t.Fatalf("plugin round trip: %+v", c.Plugin)
	}
}

func TestMixedKindSameName(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	store, e := postgres.NewEntries(t.Context(), pool, 65536)
	if e != nil {
		t.Fatal(e)
	}
	source, e := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}})
	if e != nil {
		t.Fatal(e)
	}
	// Legacy catalogs can contain shared names even though Skill/Plugin payload
	// schema validation rejects slash-containing names at the new write boundary.
	for _, kind := range []string{"MCP", "SKILL", "PLUGIN"} {
		var id string
		e = pool.QueryRow(t.Context(), `INSERT INTO registry_entry(source_id,entry_type,name) VALUES($1,$2::entry_type,'com.example/shared') RETURNING id::text`, source.ID, kind).Scan(&id)
		if e != nil {
			t.Fatal(e)
		}
		var versionID string
		e = pool.QueryRow(t.Context(), `INSERT INTO entry_version(entry_id,name,version,description) VALUES($1,'com.example/shared','1','entry') RETURNING id::text`, id).Scan(&versionID)
		if e != nil {
			t.Fatal(e)
		}
		switch kind {
		case "MCP":
			_, e = pool.Exec(t.Context(), `INSERT INTO mcp_server(version_id) VALUES($1)`, versionID)
		case "SKILL":
			_, e = pool.Exec(t.Context(), `INSERT INTO skill(version_id,namespace) VALUES($1,'com.example')`, versionID)
		case "PLUGIN":
			_, e = pool.Exec(t.Context(), `INSERT INTO plugin(version_id,namespace) VALUES($1,'com.example')`, versionID)
		}
		if e != nil {
			t.Fatal(e)
		}
		_, e = pool.Exec(t.Context(), `INSERT INTO latest_entry_version(source_id,entry_type,name,version,latest_version_id) VALUES($1,$2::entry_type,'com.example/shared','1',$3)`, source.ID, kind, versionID)
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.SkillKind, persistence.PluginKind} {
		v, err := store.GetEntry(t.Context(), source.ID, kind, "com.example/shared", persistence.VersionSelector{Latest: true})
		if err != nil || v.Version != "1" || v.Kind != kind {
			t.Fatalf("%s: %+v / %v", kind, v, err)
		}
	}
	if e = store.DeleteManaged(t.Context(), persistence.SkillKind, "com.example/shared", "1"); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []persistence.EntryKind{persistence.ServerKind, persistence.PluginKind} {
		if _, e = store.GetEntry(t.Context(), source.ID, kind, "com.example/shared", persistence.VersionSelector{Latest: true}); e != nil {
			t.Fatal(e)
		}
	}
}

func TestEntryClaimsCompatibility(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	store, e := postgres.NewEntries(t.Context(), pool, 65536)
	if e != nil {
		t.Fatal(e)
	}
	s, e := d.CreateSource(t.Context(), persistence.SourceDefinition{Name: "legacy", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(t.Context(), `INSERT INTO registry_entry(source_id,entry_type,name,claims) VALUES($1,'SKILL','private','{"team":"a"}')`, s.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.ReplaceSnapshot(t.Context(), s, model.Snapshot{}); !errors.Is(e, persistence.ErrConflict) {
		t.Fatalf("claimed entry: %v", e)
	}
	var n int
	if e = pool.QueryRow(t.Context(), `SELECT count(*) FROM registry_entry WHERE source_id=$1`, s.ID).Scan(&n); e != nil || n != 1 {
		t.Fatalf("claimed entry lost: %d %v", n, e)
	}
	_, e = pool.Exec(t.Context(), `UPDATE source SET claims='{"team":"a"}' WHERE id=$1`, s.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.ReplaceSnapshot(t.Context(), s, model.Snapshot{}); !errors.Is(e, persistence.ErrConflict) {
		t.Fatalf("claimed source: %v", e)
	}
}

func TestEntrySnapshotAndManaged(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	entries, e := postgres.NewEntries(t.Context(), pool, 65536)
	if e != nil {
		t.Fatal(e)
	}
	ctx := t.Context()
	source, e := d.CreateSource(ctx, persistence.SourceDefinition{Name: "upstream", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if e != nil {
		t.Fatal(e)
	}
	reg := model.Snapshot{}
	reg.Data.Skills = append(reg.Data.Skills, thvregistry.Skill{Namespace: "com.example", Name: "sample", Version: "1.0.0", Description: "skill"})
	if e = entries.ReplaceSnapshot(ctx, source, reg); e != nil {
		t.Fatal(e)
	}
	v, e := entries.GetEntry(ctx, source.ID, persistence.SkillKind, "sample", persistence.VersionSelector{Latest: true})
	if e != nil || v.Skill == nil || v.Skill.Version != "1.0.0" {
		t.Fatalf("entry=%+v err=%v", v, e)
	}
	if e = entries.ReplaceSnapshot(ctx, source, model.Snapshot{}); e != nil {
		t.Fatal(e)
	}
	_, e = entries.GetEntry(ctx, source.ID, persistence.SkillKind, "sample", persistence.VersionSelector{Latest: true})
	if !errors.Is(e, persistence.ErrNotFound) {
		t.Fatalf("after clear: %v", e)
	}
	_, e = d.CreateSource(ctx, persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = entries.Publish(ctx, persistence.Entry{Kind: persistence.SkillKind, Name: "sample", Version: "1.0.0", Skill: &model.Skill{Namespace: "com.example", Name: "sample", Version: "1.0.0", Description: "skill"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = entries.DeleteManaged(ctx, persistence.SkillKind, "sample", "1.0.0"); e != nil {
		t.Fatal(e)
	}
}
