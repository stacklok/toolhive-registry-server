package service_test

import (
	"testing"
	"time"

	"github.com/aws/smithy-go/ptr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

func TestWithCursor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		cursor        string
		expectedValue string
	}{
		{
			name:          "valid cursor",
			cursor:        "cursor123",
			expectedValue: "cursor123",
		},
		{
			name:          "empty cursor",
			cursor:        "",
			expectedValue: "",
		},
		{
			name:          "cursor with special characters",
			cursor:        "cursor-abc_123",
			expectedValue: "cursor-abc_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithCursor(tt.cursor)
			opts := &service.ListServersOptions{}

			err := opt(opts)

			if tt.expectedValue == "" {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, opts.Cursor)
		})
	}
}

func TestWithSearch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		search        string
		expectedValue string
	}{
		{
			name:          "valid search",
			search:        "test server",
			expectedValue: "test server",
		},
		{
			name:          "empty search",
			search:        "",
			expectedValue: "",
		},
		{
			name:          "search with multiple words",
			search:        "mcp server registry",
			expectedValue: "mcp server registry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithSearch(tt.search)
			opts := &service.ListServersOptions{}

			err := opt(opts)

			if tt.expectedValue == "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, opts.Search)
		})
	}
}

func TestWithUpdatedSince(t *testing.T) {
	t.Parallel()
	validTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	zeroTime := time.Time{}

	tests := []struct {
		name          string
		updatedSince  time.Time
		expectedValue time.Time
	}{
		{
			name:          "valid time",
			updatedSince:  validTime,
			expectedValue: validTime,
		},
		{
			name:          "zero time",
			updatedSince:  zeroTime,
			expectedValue: zeroTime,
		},
		{
			name:          "time in the past",
			updatedSince:  time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedValue: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "time in the future",
			updatedSince:  time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedValue: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithUpdatedSince(tt.updatedSince)
			opts := &service.ListServersOptions{}

			err := opt(opts)

			if tt.expectedValue.IsZero() {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, opts.UpdatedSince)
		})
	}
}

func TestWithRegistryNameListServers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		registryName  string
		expectedValue string
	}{
		{
			name:          "valid registry name",
			registryName:  "my-registry",
			expectedValue: "my-registry",
		},
		{
			name:          "empty registry name",
			registryName:  "",
			expectedValue: "",
		},
		{
			name:          "registry name with underscores",
			registryName:  "my_registry_v1",
			expectedValue: "my_registry_v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithRegistryName(tt.registryName)
			opts := &service.ListServersOptions{}
			err := opt(opts)

			if tt.expectedValue == "" {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, *opts.RegistryName)
		})
	}
}

func TestWithRegistryNameListServersVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		registryName  string
		expectedValue string
	}{
		{
			name:          "valid registry name",
			registryName:  "my-registry",
			expectedValue: "my-registry",
		},
		{
			name:          "empty registry name",
			registryName:  "",
			expectedValue: "",
		},
		{
			name:          "registry name with underscores",
			registryName:  "my_registry_v1",
			expectedValue: "my_registry_v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithRegistryName(tt.registryName)
			opts := &service.ListServerVersionsOptions{}
			err := opt(opts)

			if tt.expectedValue == "" {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, *opts.RegistryName)
		})
	}
}

func TestWithRegistryNameGetServerVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		registryName  string
		expectedValue string
	}{
		{
			name:          "valid registry name",
			registryName:  "my-registry",
			expectedValue: "my-registry",
		},
		{
			name:          "empty registry name",
			registryName:  "",
			expectedValue: "",
		},
		{
			name:          "registry name with underscores",
			registryName:  "my_registry_v1",
			expectedValue: "my_registry_v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithRegistryName(tt.registryName)
			opts := &service.GetServerVersionOptions{}
			err := opt(opts)

			if tt.expectedValue == "" {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, *opts.RegistryName)
		})
	}
}

func TestWithNext(t *testing.T) {
	t.Parallel()
	validTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	zeroTime := time.Time{}

	tests := []struct {
		name          string
		next          time.Time
		expectedValue *time.Time
	}{
		{
			name:          "valid time",
			next:          validTime,
			expectedValue: &validTime,
		},
		{
			name:          "zero time",
			next:          zeroTime,
			expectedValue: nil,
		},
		{
			name:          "time with nanoseconds",
			next:          time.Date(2024, 1, 15, 10, 30, 45, 123456789, time.UTC),
			expectedValue: ptr.Time(time.Date(2024, 1, 15, 10, 30, 45, 123456789, time.UTC)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithNext(tt.next)
			opts := &service.ListServerVersionsOptions{}

			err := opt(opts)

			if tt.expectedValue == nil {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, *tt.expectedValue, *opts.Next)
		})
	}
}

func TestWithPrev(t *testing.T) {
	t.Parallel()
	validTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	zeroTime := time.Time{}

	tests := []struct {
		name          string
		prev          time.Time
		expectedValue *time.Time
	}{
		{
			name:          "valid time",
			prev:          validTime,
			expectedValue: &validTime,
		},
		{
			name:          "zero time",
			prev:          zeroTime,
			expectedValue: nil,
		},
		{
			name:          "time in the past",
			prev:          time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedValue: ptr.Time(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithPrev(tt.prev)
			opts := &service.ListServerVersionsOptions{}

			err := opt(opts)

			if tt.expectedValue == nil {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, *tt.expectedValue, *opts.Prev)
		})
	}
}

func TestWithVersionListServers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		version       string
		expectedValue string
	}{
		{
			name:          "valid version",
			version:       "1.0.0",
			expectedValue: "1.0.0",
		},
		{
			name:          "empty version",
			version:       "",
			expectedValue: "",
		},
		{
			name:          "version with pre-release",
			version:       "1.0.0-alpha.1",
			expectedValue: "1.0.0-alpha.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithVersion(tt.version)
			opts := &service.ListServersOptions{}
			err := opt(opts)

			if tt.expectedValue == "" {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, opts.Version)
		})
	}
}

func TestWithVersionGetServerVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		version       string
		expectedValue string
	}{
		{
			name:          "valid version",
			version:       "2.3.4",
			expectedValue: "2.3.4",
		},
		{
			name:          "empty version",
			version:       "",
			expectedValue: "",
		},
		{
			name:          "version with build metadata",
			version:       "1.0.0+build.123",
			expectedValue: "1.0.0+build.123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithVersion(tt.version)
			opts := &service.GetServerVersionOptions{}
			err := opt(opts)

			if tt.expectedValue == "" {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, opts.Version)
		})
	}
}

func TestWithNameListServerVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		serverName    string
		expectedValue string
	}{
		{
			name:          "valid name",
			serverName:    "my-server",
			expectedValue: "my-server",
		},
		{
			name:          "empty name",
			serverName:    "",
			expectedValue: "",
		},
		{
			name:          "name with underscores",
			serverName:    "my_server_v1",
			expectedValue: "my_server_v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithName(tt.serverName)
			opts := &service.ListServerVersionsOptions{}
			err := opt(opts)

			if tt.expectedValue == "" {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, opts.Name)
		})
	}
}

func TestWithNameGetServerVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		serverName    string
		expectedValue string
	}{
		{
			name:          "valid name",
			serverName:    "test-server",
			expectedValue: "test-server",
		},
		{
			name:          "empty name",
			serverName:    "",
			expectedValue: "",
		},
		{
			name:          "name with hyphens",
			serverName:    "my-server-v2",
			expectedValue: "my-server-v2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithName(tt.serverName)
			opts := &service.GetServerVersionOptions{}
			err := opt(opts)

			if tt.expectedValue == "" {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, opts.Name)
		})
	}
}

func TestWithLimitListServers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		limit         int
		expectedValue int
	}{
		{
			name:          "valid limit",
			limit:         10,
			expectedValue: 10,
		},
		{
			name:          "zero limit",
			limit:         0,
			expectedValue: 0,
		},
		{
			name:          "negative limit",
			limit:         -5,
			expectedValue: 0,
		},
		{
			name:          "large limit",
			limit:         1000,
			expectedValue: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithLimit(tt.limit)
			opts := &service.ListServersOptions{}
			err := opt(opts)

			if tt.expectedValue == 0 {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, opts.Limit)
		})
	}
}

func TestWithLimitListServerVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		limit         int
		expectedValue int
	}{
		{
			name:          "valid limit",
			limit:         20,
			expectedValue: 20,
		},
		{
			name:          "zero limit",
			limit:         0,
			expectedValue: 0,
		},
		{
			name:          "negative limit",
			limit:         -10,
			expectedValue: 0,
		},
		{
			name:          "limit of 1",
			limit:         1,
			expectedValue: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opt := service.WithLimit(tt.limit)
			opts := &service.ListServerVersionsOptions{}
			err := opt(opts)

			if tt.expectedValue == 0 {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedValue, opts.Limit)
		})
	}
}
