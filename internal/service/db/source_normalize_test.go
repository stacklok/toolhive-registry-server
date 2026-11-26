package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// TestSourceWrites_NormalizeV154Settings checks that settings v1.5.4's admin
// API accepted, which the library would refuse to sync, are stored in a form
// the library syncs, on both create and update.
func TestSourceWrites_NormalizeV154Settings(t *testing.T) {
	t.Parallel()

	const (
		url      = "https://example.com/registry.json"
		endpoint = "https://registry.example.com"
		repo     = "https://github.com/example/repo.git"
	)
	tests := []struct {
		name string
		req  *service.SourceCreateRequest
		// want is the stored definition without its identity and timestamps.
		want persistence.SourceDefinition
	}{
		{
			name: "zero interval",
			req: &service.SourceCreateRequest{
				API: &config.APIConfig{Endpoint: endpoint}, SyncPolicy: &config.SyncPolicyConfig{Interval: "0s"},
			},
			want: persistence.SourceDefinition{API: &persistence.APISpec{Endpoint: endpoint}, Schedule: "2m0s"},
		},
		{
			name: "negative interval",
			req: &service.SourceCreateRequest{
				API: &config.APIConfig{Endpoint: endpoint}, SyncPolicy: &config.SyncPolicyConfig{Interval: "-1h"},
			},
			want: persistence.SourceDefinition{API: &persistence.APISpec{Endpoint: endpoint}, Schedule: "2m0s"},
		},
		{
			name: "sub-second interval",
			req: &service.SourceCreateRequest{
				API: &config.APIConfig{Endpoint: endpoint}, SyncPolicy: &config.SyncPolicyConfig{Interval: "100ms"},
			},
			want: persistence.SourceDefinition{API: &persistence.APISpec{Endpoint: endpoint}, Schedule: "1s"},
		},
		{
			name: "zero URL timeout",
			req: &service.SourceCreateRequest{
				File: &config.FileConfig{URL: url, Timeout: "0s"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
			},
			want: persistence.SourceDefinition{File: &persistence.FileSpec{URL: url, Timeout: "10s"}, Schedule: "1h0m0s"},
		},
		{
			name: "negative URL timeout",
			req: &service.SourceCreateRequest{
				File: &config.FileConfig{URL: url, Timeout: "-1s"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
			},
			want: persistence.SourceDefinition{File: &persistence.FileSpec{URL: url}, Schedule: "1h0m0s"},
		},
		{
			name: "invalid include glob",
			req: &service.SourceCreateRequest{
				Git:        &config.GitConfig{Repository: repo},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
				Filter:     &config.FilterConfig{Names: &config.NameFilterConfig{Include: []string{"io.a/*", "[bad"}}},
			},
			want: persistence.SourceDefinition{
				Git: &persistence.GitSpec{Repository: repo}, Schedule: "1h0m0s",
				Filter: &persistence.Filter{Names: &persistence.NameFilter{Include: []string{"io.a/*"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, cleanup := setupTestServiceWithCodecs(t)
			defer cleanup()
			ctx := context.Background()
			defs, err := postgres.NewDefinitions(svc.pool)
			require.NoError(t, err)
			want := tt.want
			want.Name, want.Origin = "legacy", persistence.OriginAPI
			stored := func() persistence.SourceDefinition {
				t.Helper()
				d, err := defs.GetSource(ctx, "legacy")
				require.NoError(t, err)
				_, err = d.Kind()
				require.NoError(t, err, "the stored definition is one the library syncs")
				d.ID, d.CreatedAt, d.UpdatedAt = "", time.Time{}, time.Time{}
				return d
			}

			_, err = svc.CreateSource(ctx, "legacy", tt.req)
			require.NoError(t, err)
			assert.Equal(t, want, stored())

			// Update the source to a valid definition, then back again.
			valid := normalizedOnce(t, tt.req)
			_, err = svc.UpdateSource(ctx, "legacy", valid)
			require.NoError(t, err)
			_, err = svc.UpdateSource(ctx, "legacy", tt.req)
			require.NoError(t, err)
			assert.Equal(t, want, stored())
		})
	}

	t.Run("settings v1.5.4 rejected stay rejected", func(t *testing.T) {
		t.Parallel()
		svc, cleanup := setupTestServiceWithCodecs(t)
		defer cleanup()
		_, err := svc.CreateSource(context.Background(), "path-timeout", &service.SourceCreateRequest{
			File: &config.FileConfig{Path: "/data/registry.json", Timeout: "30s"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
		})
		require.ErrorIs(t, err, service.ErrInvalidSourceConfig)
		_, err = svc.CreateSource(context.Background(), "two-refs", &service.SourceCreateRequest{
			Git:        &config.GitConfig{Repository: "https://github.com/example/repo.git", Branch: "main", Tag: "v1"},
			SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
		})
		require.ErrorIs(t, err, service.ErrInvalidSourceConfig)
	})
}

// normalizedOnce returns a copy of req with a plain, valid interval, so the
// update path is exercised from a different stored definition.
func normalizedOnce(t *testing.T, req *service.SourceCreateRequest) *service.SourceCreateRequest {
	t.Helper()
	out := service.NormalizeSourceRequest(context.Background(), "legacy", req)
	policy := *out.SyncPolicy
	policy.Interval = "3h"
	out.SyncPolicy = &policy
	return out
}
