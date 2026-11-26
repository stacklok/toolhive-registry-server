package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/db/pgtypes"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const storedNormalizeMsg = "Normalized stored settings of an API-created source that v1.5.4 accepted but cannot be synced as stored"

// Sources v1.5.4's admin API stored with settings the library refuses are
// rewritten once at startup, keep their claims, and then sync with the
// normalized settings. A source that is unusable for another reason is left
// alone, and a second start rewrites nothing.
//
//nolint:paralleltest // Swaps the process-wide default logger.
func TestBuildCatalog_NormalizesStoredV154APISources(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	require.NoError(t, os.WriteFile(path, []byte(validInlineRegistry), 0o600))
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(validInlineRegistry))
	}))
	t.Cleanup(remote.Close)

	db := newTestDatabaseConfig(t)
	cat := openTestCatalog(t, db)
	// Rows as v1.5.4's admin API stored them, with an initial FAILED sync row.
	insert := func(name, kind, spec, filter, schedule string) {
		t.Helper()
		var filterArg, scheduleArg any
		if filter != "" {
			filterArg = filter
		}
		if schedule != "" {
			scheduleArg = schedule
		}
		_, err := cat.Pool.Exec(ctx, `
			WITH s AS (
				INSERT INTO source (name, creation_type, source_type, source_config, filter_config,
				                    sync_schedule, syncable, claims, created_at, updated_at)
				VALUES ($1, 'API', $2, $3::jsonb, $4::jsonb, $5::interval, true, '{"org":"acme"}', now(), now())
				RETURNING id)
			INSERT INTO registry_sync (source_id, sync_status, error_msg)
			SELECT id, 'FAILED', 'No previous sync status found' FROM s`,
			name, kind, spec, filterArg, scheduleArg)
		require.NoError(t, err)
	}
	insert("zero-interval", "file", `{"path":"`+path+`"}`, "", "0")
	insert("negative-interval", "file", `{"path":"`+path+`"}`, "", "-5 minutes")
	insert("zero-timeout", "file", `{"url":"`+remote.URL+`","timeout":"0s"}`, "", "1 hour")
	insert("bad-glob", "file", `{"path":"`+path+`"}`,
		`{"names":{"include":["io.example/*","[bad"]},"tags":{"exclude":["beta"]}}`, "1 hour")
	// Unusable for another reason too (ambiguous git refs): left as stored.
	insert("other-broken", "git", `{"repository":"https://example.com/r.git","branch":"main","tag":"v1"}`, "", "0")

	type snapshot struct {
		generation int64
		updated    time.Time
	}
	state := func(name string) snapshot {
		t.Helper()
		var s snapshot
		require.NoError(t, cat.Pool.QueryRow(ctx,
			`SELECT definition_generation, updated_at FROM source WHERE name = $1`, name).Scan(&s.generation, &s.updated))
		return s
	}
	untouchedBefore := state("other-broken")

	logs := captureCompatLogs(t)
	cfg := catalogTestConfig(db)
	built, err := buildCatalog(ctx, newBuildCatalogConfig(t, cfg))
	require.NoError(t, err)

	warned := map[string]bool{}
	for _, record := range logs.records(t, storedNormalizeMsg) {
		assert.Equal(t, "WARN", record["level"])
		warned[record["source"].(string)] = true
	}
	assert.Equal(t, map[string]bool{
		"zero-interval": true, "negative-interval": true, "zero-timeout": true, "bad-glob": true,
	}, warned, "one warning names each rewritten source")

	get := func(name string) persistence.SourceDefinition {
		t.Helper()
		d, err := built.catalog.Definitions.GetSource(ctx, name)
		require.NoError(t, err)
		return d
	}
	assert.Equal(t, "2m0s", get("zero-interval").Schedule)
	assert.Equal(t, "2m0s", get("negative-interval").Schedule)
	assert.Equal(t, "10s", get("zero-timeout").File.Timeout)
	assert.Equal(t, "1h0m0s", get("zero-timeout").Schedule)
	filter := get("bad-glob").Filter
	require.NotNil(t, filter)
	assert.Equal(t, &persistence.NameFilter{Include: []string{"io.example/*"}}, filter.Names)
	assert.Equal(t, &persistence.NameFilter{Exclude: []string{"beta"}}, filter.Tags, "other filter settings are kept")

	var claims string
	require.NoError(t, cat.Pool.QueryRow(ctx,
		`SELECT claims::text FROM source WHERE name = 'zero-timeout'`).Scan(&claims))
	assert.JSONEq(t, `{"org":"acme"}`, claims, "claims are kept")
	var micros int64
	require.NoError(t, cat.Pool.QueryRow(ctx,
		`SELECT (extract(epoch from sync_schedule)*1000000)::bigint FROM source WHERE name = 'other-broken'`).Scan(&micros))
	assert.Zero(t, micros, "a source unusable for another reason is left as stored")
	assert.Equal(t, untouchedBefore, state("other-broken"))

	done := make(chan error, 1)
	go func() { done <- built.sync.Start(ctx) }()
	status := func(name string) persistence.JobStatus {
		s, err := built.catalog.Jobs.Status(ctx, name)
		if err != nil {
			return persistence.JobStatus{}
		}
		return s
	}
	require.Eventually(t, func() bool {
		for _, name := range []string{"zero-interval", "negative-interval", "zero-timeout", "bad-glob"} {
			if status(name).Phase != persistence.JobCompleted {
				return false
			}
		}
		return true
	}, 20*time.Second, 50*time.Millisecond, "every rewritten source syncs")
	assert.EqualValues(t, 1, status("zero-timeout").ServerCount)
	assert.EqualValues(t, 1, status("bad-glob").ServerCount, "the include pattern before the invalid one applies")
	require.NoError(t, built.sync.Stop())
	require.NoError(t, <-done)

	// A second start finds nothing to rewrite.
	rewritten := map[string]snapshot{}
	for name := range warned {
		rewritten[name] = state(name)
	}
	before := len(logs.records(t, storedNormalizeMsg))
	_, err = buildCatalog(ctx, newBuildCatalogConfig(t, cfg))
	require.NoError(t, err)
	assert.Len(t, logs.records(t, storedNormalizeMsg), before, "no warning on the second start")
	for name, s := range rewritten {
		assert.Equal(t, s, state(name), "source %s is not rewritten again", name)
	}
}

func TestNormalizeStoredSource(t *testing.T) {
	t.Parallel()
	hour := pgtypes.NewInterval(time.Hour)
	tests := []struct {
		name         string
		in           storedSource
		wantOK       bool
		wantSpec     string
		wantFilter   string
		wantSchedule pgtypes.Interval
		wantWarnings int
	}{
		{
			name:   "usable source is left alone",
			in:     storedSource{name: "ok", kind: "file", spec: []byte(`{"path":"/r.json"}`), schedule: hour},
			wantOK: false,
		},
		{
			name:   "zero interval becomes two minutes",
			in:     storedSource{name: "s", kind: "api", spec: []byte(`{"endpoint":"https://r.example.com"}`), schedule: pgtypes.NewInterval(0)},
			wantOK: true, wantSpec: `{"endpoint":"https://r.example.com"}`,
			wantSchedule: pgtypes.NewInterval(2 * time.Minute), wantWarnings: 1,
		},
		{
			name:   "null interval is not v1.5.4's zero and is left alone",
			in:     storedSource{name: "s", kind: "api", spec: []byte(`{"endpoint":"https://r.example.com"}`)},
			wantOK: false,
		},
		{
			name: "timeout on a path source is dropped, other fields kept",
			in: storedSource{name: "s", kind: "file", spec: []byte(`{"path":"/r.json","timeout":"30s","extra":1}`),
				schedule: hour},
			wantOK: true, wantSpec: `{"path":"/r.json","extra":1}`, wantSchedule: hour, wantWarnings: 1,
		},
		{
			name: "invalid exclude glob excludes everything",
			in: storedSource{name: "s", kind: "file", spec: []byte(`{"path":"/r.json"}`),
				filter: []byte(`{"names":{"exclude":["["]}}`), schedule: hour},
			wantOK: true, wantSpec: `{"path":"/r.json"}`, wantFilter: `{"names":{"exclude":["*"]}}`,
			wantSchedule: hour, wantWarnings: 1,
		},
		{
			name: "several settings at once",
			in: storedSource{name: "s", kind: "file", spec: []byte(`{"url":"https://e.com/r.json","timeout":"0s"}`),
				filter: []byte(`{"names":{"include":["["]}}`), schedule: pgtypes.NewInterval(-time.Minute)},
			wantOK: true, wantSpec: `{"url":"https://e.com/r.json","timeout":"10s"}`,
			wantFilter: `{"names":{"exclude":["*"]}}`, wantSchedule: pgtypes.NewInterval(2 * time.Minute), wantWarnings: 3,
		},
		{
			name: "still unusable after normalization",
			in: storedSource{name: "s", kind: "git", spec: []byte(`{"repository":"https://e.com/r.git","branch":"a","tag":"b"}`),
				schedule: pgtypes.NewInterval(0)},
			wantOK: false,
		},
		{
			name:   "unreadable row is left alone",
			in:     storedSource{name: "s", kind: "file", spec: []byte(`not json`), schedule: pgtypes.NewInterval(0)},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, warnings, ok := normalizeStoredSource(tt.in)
			require.Equal(t, tt.wantOK, ok)
			if !ok {
				return
			}
			assert.Len(t, warnings, tt.wantWarnings)
			assert.JSONEq(t, tt.wantSpec, string(out.spec))
			if tt.wantFilter == "" {
				assert.Equal(t, tt.in.filter, out.filter)
			} else {
				assert.JSONEq(t, tt.wantFilter, string(out.filter))
			}
			assert.Equal(t, tt.wantSchedule, out.schedule)
			_, _, again := normalizeStoredSource(out)
			assert.False(t, again, "normalizing is idempotent")
		})
	}
}
