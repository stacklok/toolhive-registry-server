package service

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestNormalizeSourceRequest(t *testing.T) {
	t.Parallel()

	const url = "https://example.com/registry.json"
	tests := []struct {
		name     string
		req      *SourceCreateRequest
		want     *SourceCreateRequest
		warnings int
	}{
		{name: "nil request", req: nil, want: nil},
		{
			name: "valid request is unchanged",
			req: &SourceCreateRequest{
				File: &config.FileConfig{URL: url, Timeout: "30s"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
			},
			want: &SourceCreateRequest{
				File: &config.FileConfig{URL: url, Timeout: "30s"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "1h"},
			},
		},
		{
			name: "zero interval and zero URL timeout",
			req: &SourceCreateRequest{
				File: &config.FileConfig{URL: url, Timeout: "0s"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "0s"},
			},
			want: &SourceCreateRequest{
				File: &config.FileConfig{URL: url, Timeout: "10s"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "2m0s"},
			},
			warnings: 2,
		},
		{
			name: "sub-second interval and invalid exclude glob",
			req: &SourceCreateRequest{
				API:        &config.APIConfig{Endpoint: "https://registry.example.com"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "10ms"},
				Filter: &config.FilterConfig{
					Names: &config.NameFilterConfig{Exclude: []string{"[bad"}},
					Tags:  &config.TagFilterConfig{Include: []string{"prod"}},
				},
			},
			want: &SourceCreateRequest{
				API:        &config.APIConfig{Endpoint: "https://registry.example.com"},
				SyncPolicy: &config.SyncPolicyConfig{Interval: "1s"},
				Filter: &config.FilterConfig{
					Names: &config.NameFilterConfig{Exclude: []string{"*"}},
					Tags:  &config.TagFilterConfig{Include: []string{"prod"}},
				},
			},
			warnings: 2,
		},
		{
			name: "interval on a non-synced source is left alone",
			req:  &SourceCreateRequest{Managed: &config.ManagedConfig{}, SyncPolicy: &config.SyncPolicyConfig{Interval: "0s"}},
			want: &SourceCreateRequest{Managed: &config.ManagedConfig{}, SyncPolicy: &config.SyncPolicyConfig{Interval: "0s"}},
		},
		{
			name: "interval on an inline source is left alone",
			req: &SourceCreateRequest{
				File: &config.FileConfig{Data: "{}"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "0s"},
			},
			want: &SourceCreateRequest{
				File: &config.FileConfig{Data: "{}"}, SyncPolicy: &config.SyncPolicyConfig{Interval: "0s"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var before SourceCreateRequest
			if tt.req != nil {
				before = deepCopyRequest(*tt.req)
			}
			got, warnings := normalizeSourceRequest(tt.req)
			assert.Equal(t, tt.want, got)
			assert.Len(t, warnings, tt.warnings)
			if tt.req != nil {
				assert.Equal(t, before, *tt.req, "the input request is not modified")
			}
		})
	}
}

func deepCopyRequest(r SourceCreateRequest) SourceCreateRequest {
	if r.File != nil {
		f := *r.File
		r.File = &f
	}
	if r.SyncPolicy != nil {
		p := *r.SyncPolicy
		r.SyncPolicy = &p
	}
	if r.Filter != nil {
		f := *r.Filter
		r.Filter = &f
	}
	return r
}
