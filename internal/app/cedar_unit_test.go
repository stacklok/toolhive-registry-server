package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	auditmw "github.com/stacklok/toolhive-registry-server/internal/audit"
	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/cedar"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

func TestCedarIdentity(t *testing.T) {
	t.Parallel()

	anonymousCtx := func() context.Context {
		var ctx context.Context
		admit := auth.WithAnonymousAdmission(func(h http.Handler) http.Handler { return h }, config.AuthModeAnonymous, true)
		admit(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() })).
			ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		return ctx
	}

	tests := []struct {
		name   string
		ctx    context.Context
		want   cedar.Identity
		wantOK bool
	}{
		{
			name:   "anonymous admission is the anonymous principal",
			ctx:    anonymousCtx(),
			want:   cedar.Identity{Anonymous: true},
			wantOK: true,
		},
		{
			name: "verified token claims become issuer, subject and claims",
			ctx: auth.ContextWithClaims(context.Background(),
				jwt.MapClaims{"iss": "https://idp.example.com", "sub": "alice", "groups": []any{"admins"}}),
			want: cedar.Identity{Issuer: "https://idp.example.com", Subject: "alice",
				Claims: map[string]any{"iss": "https://idp.example.com", "sub": "alice", "groups": []any{"admins"}}},
			wantOK: true,
		},
		{
			name: "non-string issuer and subject are left empty for the authorizer to reject",
			ctx:  auth.ContextWithClaims(context.Background(), jwt.MapClaims{"iss": 1, "sub": true}),
			want: cedar.Identity{Claims: map[string]any{"iss": 1, "sub": true}}, wantOK: true,
		},
		{
			name: "no authentication ran",
			ctx:  context.Background(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/registry/main/v0.1/servers", nil).WithContext(tt.ctx)
			got, ok := cedarIdentity(req)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAdmissionAudit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		decision     cedar.Decision
		wantDecision string
		wantOutcome  string
	}{
		{name: "allow", decision: cedar.Decision{Allowed: true},
			wantDecision: "allow", wantOutcome: "success"},
		{name: "deny", decision: cedar.Decision{Allowed: false},
			wantDecision: "deny", wantOutcome: "denied"},
		{name: "error wins over allow", decision: cedar.Decision{Allowed: true, Err: cedar.ErrEvaluation},
			wantDecision: "error", wantOutcome: "error"},
		{name: "anonymous on an admin action is an error", decision: cedar.Decision{Err: cedar.ErrAuthenticationRequired},
			wantDecision: "error", wantOutcome: "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logFile := filepath.Join(t.TempDir(), "audit.log")
			logger, err := auditmw.NewLogger(logFile)
			require.NoError(t, err)

			d := tt.decision
			d.Principal = cedar.AnonymousPrincipal
			d.Action = cedar.SourcesWrite
			d.Resource = `Registry::Instance::"catalog"`
			d.Revision = "sha256:feed"
			admissionAudit(logger)(httptest.NewRequest(http.MethodPut, "/v1/sources/s", nil), d)
			require.NoError(t, logger.Close())

			events := readJSONLines(t, logFile)
			require.Len(t, events, 1)
			event := events[0]
			assert.Equal(t, "authorization.admission", event["type"])
			assert.Equal(t, tt.wantOutcome, event["outcome"])
			assert.Equal(t, map[string]any{"actor": cedar.AnonymousPrincipal}, event["subjects"])
			assert.Equal(t, map[string]any{"action": "sources.write", "resource": `Registry::Instance::"catalog"`},
				event["target"])
			extra := event["metadata"].(map[string]any)["extra"].(map[string]any)
			assert.Equal(t, tt.wantDecision, extra["decision"])
			assert.Equal(t, "sha256:feed", extra["policy_revision"])
		})
	}
}

// readJSONLines parses every line of a JSON-lines file.
func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is test-controlled
	require.NoError(t, err)
	var lines []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line), scanner.Text())
		lines = append(lines, line)
	}
	require.NoError(t, scanner.Err())
	return lines
}

// recordingSources records the definitions that reach the wrapped backend.
type recordingSources struct {
	persistence.Sources
	created []persistence.SourceDefinition
	updated []persistence.SourceDefinition
	names   []string
}

func (r *recordingSources) CreateSource(_ context.Context, d persistence.SourceDefinition) (persistence.SourceDefinition, error) {
	r.created = append(r.created, d)
	return d, nil
}

func (r *recordingSources) UpdateSource(
	_ context.Context, name string, d persistence.SourceDefinition,
) (persistence.SourceDefinition, error) {
	r.names = append(r.names, name)
	r.updated = append(r.updated, d)
	return d, nil
}

func TestScopedSources(t *testing.T) {
	t.Parallel()
	preflightErr := errors.New("CRDs not installed")

	tests := []struct {
		name           string
		watch          string
		def            persistence.SourceDefinition
		preflightErr   error
		wantNamespaces []string
		wantErr        error
		wantPreflight  bool
	}{
		{
			name:           "kubernetes without namespaces takes the watch scope, sorted and trimmed",
			watch:          "ns-b, ns-a,ns-b",
			def:            persistence.SourceDefinition{Name: "k8s", Kubernetes: &persistence.KubernetesSpec{}},
			wantNamespaces: []string{"ns-a", "ns-b"}, wantPreflight: true,
		},
		{
			name:  "explicit namespaces win over the watch scope",
			watch: "ns-a",
			def: persistence.SourceDefinition{Name: "k8s",
				Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"team-z", "team-y", "team-z"}}},
			wantNamespaces: []string{"team-y", "team-z"}, wantPreflight: true,
		},
		{
			name:          "no watch scope and no namespaces means all namespaces",
			def:           persistence.SourceDefinition{Name: "k8s", Kubernetes: &persistence.KubernetesSpec{}},
			wantPreflight: true,
		},
		{
			name: "an empty namespace is rejected before preflight",
			def: persistence.SourceDefinition{Name: "k8s",
				Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"ns-a", " "}}},
			wantErr: persistence.ErrInvalid,
		},
		{
			name:    "an empty watch entry is rejected before preflight",
			watch:   "ns-a,,ns-b",
			def:     persistence.SourceDefinition{Name: "k8s", Kubernetes: &persistence.KubernetesSpec{}},
			wantErr: persistence.ErrInvalid,
		},
		{
			name:          "preflight failure is returned and nothing is written",
			def:           persistence.SourceDefinition{Name: "k8s", Kubernetes: &persistence.KubernetesSpec{}},
			preflightErr:  preflightErr,
			wantErr:       preflightErr,
			wantPreflight: true,
		},
		{
			name:          "non-kubernetes source is passed through after preflight",
			watch:         "ns-a",
			def:           persistence.SourceDefinition{Name: "managed", Managed: &persistence.ManagedSpec{}},
			wantPreflight: true,
		},
	}

	for _, tt := range tests {
		for _, op := range []string{"create", "update"} {
			t.Run(tt.name+"/"+op, func(t *testing.T) {
				t.Parallel()
				backend := &recordingSources{}
				var preflighted []persistence.SourceDefinition
				scoped := &scopedSources{Sources: backend, watch: tt.watch,
					preflight: func(_ context.Context, d persistence.SourceDefinition) error {
						preflighted = append(preflighted, d)
						return tt.preflightErr
					}}
				var original []string
				if tt.def.Kubernetes != nil {
					original = append([]string(nil), tt.def.Kubernetes.Namespaces...)
				}

				var err error
				if op == "create" {
					_, err = scoped.CreateSource(context.Background(), tt.def)
				} else {
					_, err = scoped.UpdateSource(context.Background(), tt.def.Name, tt.def)
				}

				if tt.def.Kubernetes != nil {
					assert.Equal(t, original, tt.def.Kubernetes.Namespaces, "the caller's definition is not mutated")
				}
				assert.Equal(t, tt.wantPreflight, len(preflighted) == 1, "preflight calls: %d", len(preflighted))
				if tt.wantErr != nil {
					require.ErrorIs(t, err, tt.wantErr)
					assert.Empty(t, backend.created)
					assert.Empty(t, backend.updated)
					return
				}
				require.NoError(t, err)
				written := backend.created
				if op == "update" {
					written = backend.updated
					assert.Equal(t, []string{tt.def.Name}, backend.names)
				} else {
					assert.Empty(t, backend.updated)
				}
				require.Len(t, written, 1)
				assert.Equal(t, preflighted[0], written[0], "preflight sees exactly what is written")
				if tt.def.Kubernetes == nil {
					assert.Nil(t, written[0].Kubernetes)
					return
				}
				require.NotNil(t, written[0].Kubernetes)
				if len(tt.wantNamespaces) == 0 {
					assert.Empty(t, written[0].Kubernetes.Namespaces, "an empty scope means all namespaces")
				} else {
					assert.Equal(t, tt.wantNamespaces, written[0].Kubernetes.Namespaces)
				}
			})
		}
	}
}
