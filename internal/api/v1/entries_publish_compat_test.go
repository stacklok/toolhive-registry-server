package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database/dbtest"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
	database "github.com/stacklok/toolhive-registry-server/internal/service/db"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// These tests drive POST /v1/entries through the real service, the library
// adapter with the claims hook, and PostgreSQL, and pin the v1.5.4 answers
// for bodies v1.5.4 accepted or rejected differently from the library.

// publishEnv is one handler wired to a fresh database with a managed source.
type publishEnv struct {
	router http.Handler
	pool   *pgxpool.Pool
}

type publishEnvConfig struct {
	authCfg      *config.AuthConfig
	sourceClaims map[string]any
	maxMetaSize  int
	// wrap, when set, replaces the library adapter the service publishes with.
	wrap func(persistence.ManagedPublisher) persistence.ManagedPublisher
}

func newPublishEnv(t *testing.T, cfg publishEnvConfig) publishEnv {
	t.Helper()
	ctx := context.Background()
	conn, cleanup := dbtest.SetupTestDB(t)
	t.Cleanup(cleanup)
	pool, err := pgxpool.New(ctx, conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	_, err = sqlc.New(pool).InsertSource(ctx, sqlc.InsertSourceParams{
		Name:         "internal",
		CreationType: sqlc.CreationTypeCONFIG,
		SourceType:   "managed",
		Claims:       db.SerializeClaims(cfg.sourceClaims),
	})
	require.NoError(t, err)

	maxMeta := cfg.maxMetaSize
	if maxMeta == 0 {
		maxMeta = config.DefaultMaxMetaSize
	}
	entries, err := postgres.NewEntries(ctx, pool, maxMeta,
		postgres.WithEntryWriteHook(database.NewClaimsHook(nil)))
	require.NoError(t, err)
	var publisher persistence.ManagedPublisher = entries
	if cfg.wrap != nil {
		publisher = cfg.wrap(entries)
	}
	opts := []database.Option{
		database.WithConnectionPool(pool),
		database.WithMaxMetaSize(maxMeta),
		database.WithPublisher(publisher),
	}
	if cfg.authCfg == nil || cfg.authCfg.Authz == nil {
		opts = append(opts, database.WithSkipAuthz())
	}
	svc, err := database.New(opts...)
	require.NoError(t, err)
	return publishEnv{router: Router(svc, cfg.authCfg), pool: pool}
}

// post publishes body; a non-nil caller is an authenticated publisher with
// the manageEntries role only (not super-admin, so claim gates apply).
func (e publishEnv) post(t *testing.T, body string, caller jwt.MapClaims) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/entries", bytes.NewReader([]byte(body)))
	if caller != nil {
		ctx := auth.ContextWithClaims(req.Context(), caller)
		ctx = auth.ContextWithRoles(ctx, []auth.Role{auth.RoleManageEntries})
		req = req.WithContext(ctx)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	var out map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out), rr.Body.String())
	return rr.Code, out
}

func skillBody(name, extra string) string {
	return fmt.Sprintf(`{"skill":{"namespace":"com.test","name":%q,"version":"1.0.0",`+
		`"description":"test skill"%s}}`, name, extra)
}

func pluginBody(name, extra string) string {
	return fmt.Sprintf(`{"plugin":{"namespace":"com.test","name":%q,"version":"1.0.0",`+
		`"description":"test plugin"%s}}`, name, extra)
}

func serverBody(name, extra string) string {
	return fmt.Sprintf(`{"server":{"name":%q,"version":"1.0.0","description":"test server"%s}}`, name, extra)
}

// TestPublishEntryAcceptsV154Bodies covers F1 (status in any case) and F2
// (storage-assigned fields in the body are ignored). v1.5.4 answered 201 for
// every case below and stored the status upper-cased.
func TestPublishEntryAcceptsV154Bodies(t *testing.T) {
	t.Parallel()
	env := newPublishEnv(t, publishEnvConfig{})
	const storageFields = `,"id":"11111111-1111-1111-1111-111111111111","isLatest":false,` +
		`"createdAt":"2020-01-01T00:00:00Z","updatedAt":"2020-01-02T00:00:00Z"`

	tests := []struct {
		name       string
		body       string
		wantStatus string
	}{
		{name: "skill upper-case status", body: skillBody("upper-status", `,"status":"ACTIVE"`), wantStatus: "ACTIVE"},
		{name: "skill mixed-case status", body: skillBody("mixed-status", `,"status":"Deprecated"`), wantStatus: "DEPRECATED"},
		{name: "skill lower-case status", body: skillBody("lower-status", `,"status":"archived"`), wantStatus: "ARCHIVED"},
		{name: "plugin upper-case status", body: pluginBody("upper-status", `,"status":"ARCHIVED"`), wantStatus: "ARCHIVED"},
		{name: "plugin mixed-case status", body: pluginBody("mixed-status", `,"status":"Active"`), wantStatus: "ACTIVE"},
		{name: "skill with storage fields", body: skillBody("storage-fields", storageFields)},
		{name: "skill with isLatest true", body: skillBody("is-latest", `,"isLatest":true`)},
		{name: "plugin with storage fields", body: pluginBody("storage-fields", storageFields)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, got := env.post(t, tt.body, nil)
			require.Equal(t, http.StatusCreated, code, got)
			if tt.wantStatus != "" {
				assert.Equal(t, tt.wantStatus, got["status"])
			}
			// The response carries the stored identity, never the body's.
			assert.NotEqual(t, "11111111-1111-1111-1111-111111111111", got["id"])
			assert.NotEqual(t, "2020-01-01T00:00:00Z", got["createdAt"])
		})
	}
}

// TestPublishEntryRejectsUnknownStatus documents the one status difference:
// v1.5.4 answered 500 for a status outside active/deprecated/archived (the
// database enum refused it); the library's schema check answers 400.
func TestPublishEntryRejectsUnknownStatus(t *testing.T) {
	t.Parallel()
	env := newPublishEnv(t, publishEnvConfig{})
	code, got := env.post(t, skillBody("bogus-status", `,"status":"BOGUS"`), nil)
	assert.Equal(t, http.StatusBadRequest, code, got)
}

// TestPublishEntryDuplicateVersionText covers F4: v1.5.4's 409 text names a
// skill or plugin as "name version" and a server as "name@version".
func TestPublishEntryDuplicateVersionText(t *testing.T) {
	t.Parallel()
	env := newPublishEnv(t, publishEnvConfig{})

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "skill", body: skillBody("my-skill", ""), wantErr: "version already exists: my-skill 1.0.0"},
		{name: "plugin", body: pluginBody("my-plugin", ""), wantErr: "version already exists: my-plugin 1.0.0"},
		{name: "server", body: serverBody("io.test/my-server", ""), wantErr: "version already exists: io.test/my-server@1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, got := env.post(t, tt.body, nil)
			require.Equal(t, http.StatusCreated, code, got)
			code, got = env.post(t, tt.body, nil)
			assert.Equal(t, http.StatusConflict, code)
			assert.Equal(t, tt.wantErr, got["error"])
		})
	}
}

// TestPublishEntryOversizedMeta covers F5: oversized publisher-provided _meta
// is a 400 whose message, and log line, name the actual and maximum sizes.
// Not parallel: it captures the default logger.
//
//nolint:paralleltest // replaces the process-wide default slog logger
func TestPublishEntryOversizedMeta(t *testing.T) {
	env := newPublishEnv(t, publishEnvConfig{maxMetaSize: 64})
	meta := `,"_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"blob":"` +
		strings.Repeat("x", 100) + `"}}`
	const size = 111 // len(`{"blob":"` + 100 x + `"}`)

	records := captureLogs(t)
	code, got := env.post(t, serverBody("io.test/big-meta", meta), nil)

	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, fmt.Sprintf("invalid entry payload: publisher-provided metadata size %d bytes "+
		"exceeds maximum allowed size of 64 bytes", size), got["error"])
	rec := records.find("Publisher-provided metadata exceeds maximum size")
	require.NotNil(t, rec, "size rejection must be logged")
	assert.EqualValues(t, size, rec["size_bytes"])
	assert.EqualValues(t, 64, rec["max_bytes"])
	assert.Equal(t, "io.test/big-meta", rec["server"])

	code, got = env.post(t, serverBody("io.test/small-meta",
		`,"_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"k":"v"}}`), nil)
	assert.Equal(t, http.StatusCreated, code, got)
}

// TestPublishEntryGateBeforeValidation covers F7: as in v1.5.4, a caller who
// does not cover the managed source's claims gets 403 whatever the body; a
// caller who does gets the body's 400.
func TestPublishEntryGateBeforeValidation(t *testing.T) {
	t.Parallel()
	env := newPublishEnv(t, publishEnvConfig{
		authCfg:      &config.AuthConfig{Mode: config.AuthModeOAuth, Authz: &config.AuthzConfig{}},
		sourceClaims: map[string]any{"org": "acme"},
	})
	outsider := jwt.MapClaims{"sub": "outsider", "org": "other"}
	member := jwt.MapClaims{"sub": "member", "org": "acme"}
	withClaims := func(body, org string) string {
		return strings.TrimSuffix(body, "}") + fmt.Sprintf(`,"claims":{"org":%q}}`, org)
	}

	tests := []struct {
		name     string
		body     string
		caller   jwt.MapClaims
		wantCode int
	}{
		{name: "outsider, server without description",
			body: withClaims(`{"server":{"name":"io.test/no-desc","version":"1.0.0"}}`, "other"), caller: outsider,
			wantCode: http.StatusForbidden},
		{name: "outsider, skill with invalid name",
			body: withClaims(skillBody("Not A Valid Name", ""), "other"), caller: outsider,
			wantCode: http.StatusForbidden},
		{name: "outsider, oversized meta is not examined",
			body: withClaims(serverBody("io.test/meta", `,"_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"k":"`+
				strings.Repeat("x", config.DefaultMaxMetaSize)+`"}}`), "other"), caller: outsider,
			wantCode: http.StatusForbidden},
		{name: "outsider, valid server",
			body: withClaims(serverBody("io.test/valid", ""), "other"), caller: outsider,
			wantCode: http.StatusForbidden},
		{name: "member, server without description",
			body: withClaims(`{"server":{"name":"io.test/no-desc","version":"1.0.0"}}`, "acme"), caller: member,
			wantCode: http.StatusBadRequest},
		{name: "member, skill with invalid name",
			body: withClaims(skillBody("Not A Valid Name", ""), "acme"), caller: member,
			wantCode: http.StatusBadRequest},
		{name: "member, valid server",
			body: withClaims(serverBody("io.test/member-valid", ""), "acme"), caller: member,
			wantCode: http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, got := env.post(t, tt.body, tt.caller)
			assert.Equal(t, tt.wantCode, code, got)
		})
	}
}

// contendedPublisher reports every publish as abandoned after repeated
// serialization failures, as the library does when writers keep colliding.
type contendedPublisher struct {
	persistence.ManagedPublisher
}

func (contendedPublisher) Publish(context.Context, persistence.Entry) (persistence.Entry, error) {
	return persistence.Entry{}, persistence.ErrContention
}

// TestPublishEntryContentionIsInternalError covers F3: a write that lost to
// concurrent writers on every retry is 500 "failed to publish entry", as in
// v1.5.4, not 409 "version already exists".
func TestPublishEntryContentionIsInternalError(t *testing.T) {
	t.Parallel()
	env := newPublishEnv(t, publishEnvConfig{
		wrap: func(p persistence.ManagedPublisher) persistence.ManagedPublisher {
			return contendedPublisher{ManagedPublisher: p}
		},
	})
	for _, body := range []string{serverBody("io.test/contended", ""), skillBody("contended", ""), pluginBody("contended", "")} {
		code, got := env.post(t, body, nil)
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, "failed to publish entry", got["error"])
	}
}

// logRecords collects JSON slog records written while a test runs.
type logRecords struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logRecords) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logRecords) find(msg string) map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range strings.Split(strings.TrimSpace(l.buf.String()), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil && rec["msg"] == msg {
			return rec
		}
	}
	return nil
}

func captureLogs(t *testing.T) *logRecords {
	t.Helper()
	records := &logRecords{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(records, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return records
}
