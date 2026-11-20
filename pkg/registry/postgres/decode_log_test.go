package postgres_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// TestUndecodableEntryIsLogged checks that a stored row the read path cannot
// decode is logged with its kind, name, version and cause before the caller
// gets the bare ErrUnavailable category.
//
// It replaces the default slog logger, so it must not run in parallel.
//
//nolint:paralleltest // Swaps the process-wide default logger.
func TestUndecodableEntryIsLogged(t *testing.T) {
	defs, pool := newBackend(t)
	ctx := t.Context()
	entries, err := postgres.NewEntries(ctx, pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	source, err := defs.CreateSource(ctx, persistence.SourceDefinition{Name: "undecodable", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	snap := model.Snapshot{}
	for _, name := range []string{"com.example/corrupt", "com.example/orphan"} {
		snap.Data.Servers = append(snap.Data.Servers, model.Server{Schema: "https://example.org/server.schema.json", Name: name, Version: "1.2.3", Description: "x"})
	}
	if err = entries.ReplaceSnapshot(ctx, source, snap); err != nil {
		t.Fatal(err)
	}
	if _, err = defs.CreateView(ctx, persistence.ViewDefinition{Name: "consumer", Sources: []string{source.Name}}); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE mcp_server SET server_meta='[]' WHERE version_id IN (SELECT id FROM entry_version WHERE name='com.example/corrupt')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM mcp_server WHERE version_id IN (SELECT id FROM entry_version WHERE name='com.example/orphan')`); err != nil {
		t.Fatal(err)
	}
	reader, err := catalog.NewReader(entries)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	for _, tc := range []struct {
		name, entry, cause string
		read               func(string) error
	}{
		{"view get, bad payload", "com.example/corrupt", "json", func(name string) error {
			_, e := reader.Get(ctx, "consumer", catalog.EntryName{Kind: persistence.ServerKind, Name: name}, catalog.VersionSelector{Latest: true})
			return e
		}},
		{"view list, missing payload row", "com.example/orphan", "missing kind payload row", func(name string) error {
			_, e := reader.List(ctx, "consumer", catalog.Query{Kind: persistence.ServerKind, Name: name, Limit: 5})
			return e
		}},
		{"source get, bad payload", "com.example/corrupt", "json", func(name string) error {
			_, e := entries.GetEntry(ctx, source.ID, persistence.ServerKind, name, persistence.VersionSelector{Exact: "1.2.3"})
			return e
		}},
	} {
		buf.Reset()
		if e := tc.read(tc.entry); !errors.Is(e, persistence.ErrUnavailable) {
			t.Fatalf("%s: %v", tc.name, e)
		}
		var record map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			var r map[string]any
			if json.Unmarshal([]byte(line), &r) == nil && r["msg"] == "Stored registry entry could not be decoded" {
				record = r
			}
		}
		if record == nil {
			t.Fatalf("%s: no decode log in %q", tc.name, buf.String())
		}
		cause, _ := record["error"].(string)
		if record["level"] != "ERROR" || record["kind"] != string(persistence.ServerKind) || record["name"] != tc.entry ||
			record["version"] != "1.2.3" || !strings.Contains(cause, tc.cause) {
			t.Fatalf("%s: log %v", tc.name, record)
		}
	}
}
