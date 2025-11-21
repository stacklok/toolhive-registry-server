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

func TestWithRegistryName(t *testing.T) {
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
			assert.Equal(t, tt.expectedValue, opts.RegistryName)
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

func TestWithVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		version       string
		optionType    string // "ListServersOptions" or "GetServerVersionOptions"
		expectedValue string
	}{
		{
			name:          "valid version for ListServersOptions",
			version:       "1.0.0",
			optionType:    "ListServersOptions",
			expectedValue: "1.0.0",
		},
		{
			name:          "valid version for GetServerVersionOptions",
			version:       "2.3.4",
			optionType:    "GetServerVersionOptions",
			expectedValue: "2.3.4",
		},
		{
			name:          "empty version for ListServersOptions",
			version:       "",
			optionType:    "ListServersOptions",
			expectedValue: "",
		},
		{
			name:          "empty version for GetServerVersionOptions",
			version:       "",
			optionType:    "GetServerVersionOptions",
			expectedValue: "",
		},
		{
			name:          "version with pre-release for ListServersOptions",
			version:       "1.0.0-alpha.1",
			optionType:    "ListServersOptions",
			expectedValue: "1.0.0-alpha.1",
		},
		{
			name:          "version with build metadata for GetServerVersionOptions",
			version:       "1.0.0+build.123",
			optionType:    "GetServerVersionOptions",
			expectedValue: "1.0.0+build.123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			switch tt.optionType {
			case "ListServersOptions":
				opt := service.WithVersion[service.ListServersOptions](tt.version)
				opts := &service.ListServersOptions{}
				err := opt(opts)

				if tt.expectedValue == "" {
					require.Error(t, err)
					return
				}

				require.NoError(t, err)
				assert.Equal(t, tt.expectedValue, opts.Version)
			case "GetServerVersionOptions":
				opt := service.WithVersion[service.GetServerVersionOptions](tt.version)
				opts := &service.GetServerVersionOptions{}
				err := opt(opts)

				if tt.expectedValue == "" {
					require.Error(t, err)
					return
				}

				require.NoError(t, err)
				assert.Equal(t, tt.expectedValue, opts.Version)
			}
		})
	}
}

func TestWithName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		serverName    string
		optionType    string // "ListServerVersionsOptions" or "GetServerVersionOptions"
		expectedValue string
	}{
		{
			name:          "valid name for ListServerVersionsOptions",
			serverName:    "my-server",
			optionType:    "ListServerVersionsOptions",
			expectedValue: "my-server",
		},
		{
			name:          "valid name for GetServerVersionOptions",
			serverName:    "test-server",
			optionType:    "GetServerVersionOptions",
			expectedValue: "test-server",
		},
		{
			name:          "empty name for ListServerVersionsOptions",
			serverName:    "",
			optionType:    "ListServerVersionsOptions",
			expectedValue: "",
		},
		{
			name:          "empty name for GetServerVersionOptions",
			serverName:    "",
			optionType:    "GetServerVersionOptions",
			expectedValue: "",
		},
		{
			name:          "name with underscores for ListServerVersionsOptions",
			serverName:    "my_server_v1",
			optionType:    "ListServerVersionsOptions",
			expectedValue: "my_server_v1",
		},
		{
			name:          "name with hyphens for GetServerVersionOptions",
			serverName:    "my-server-v2",
			optionType:    "GetServerVersionOptions",
			expectedValue: "my-server-v2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			switch tt.optionType {
			case "ListServerVersionsOptions":
				opt := service.WithName[service.ListServerVersionsOptions](tt.serverName)
				opts := &service.ListServerVersionsOptions{}
				err := opt(opts)

				if tt.expectedValue == "" {
					require.Error(t, err)
					return
				}

				require.NoError(t, err)
				assert.Equal(t, tt.expectedValue, opts.Name)
			case "GetServerVersionOptions":
				opt := service.WithName[service.GetServerVersionOptions](tt.serverName)
				opts := &service.GetServerVersionOptions{}
				err := opt(opts)

				if tt.expectedValue == "" {
					require.Error(t, err)
					return
				}

				require.NoError(t, err)
				assert.Equal(t, tt.expectedValue, opts.Name)
			}
		})
	}
}

func TestWithLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		limit         int
		optionType    string // "ListServersOptions" or "ListServerVersionsOptions"
		expectedValue int
	}{
		{
			name:          "valid limit for ListServersOptions",
			limit:         10,
			optionType:    "ListServersOptions",
			expectedValue: 10,
		},
		{
			name:          "valid limit for ListServerVersionsOptions",
			limit:         20,
			optionType:    "ListServerVersionsOptions",
			expectedValue: 20,
		},
		{
			name:          "zero limit for ListServersOptions",
			limit:         0,
			optionType:    "ListServersOptions",
			expectedValue: 0,
		},
		{
			name:          "zero limit for ListServerVersionsOptions",
			limit:         0,
			optionType:    "ListServerVersionsOptions",
			expectedValue: 0,
		},
		{
			name:          "negative limit for ListServersOptions",
			limit:         -5,
			optionType:    "ListServersOptions",
			expectedValue: 0,
		},
		{
			name:          "negative limit for ListServerVersionsOptions",
			limit:         -10,
			optionType:    "ListServerVersionsOptions",
			expectedValue: 0,
		},
		{
			name:          "large limit for ListServersOptions",
			limit:         1000,
			optionType:    "ListServersOptions",
			expectedValue: 1000,
		},
		{
			name:          "limit of 1 for ListServerVersionsOptions",
			limit:         1,
			optionType:    "ListServerVersionsOptions",
			expectedValue: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			switch tt.optionType {
			case "ListServersOptions":
				opt := service.WithLimit[service.ListServersOptions](tt.limit)
				opts := &service.ListServersOptions{}
				err := opt(opts)

				if tt.expectedValue == 0 {
					require.Error(t, err)
					return
				}

				require.NoError(t, err)
				assert.Equal(t, tt.expectedValue, opts.Limit)
			case "ListServerVersionsOptions":
				opt := service.WithLimit[service.ListServerVersionsOptions](tt.limit)
				opts := &service.ListServerVersionsOptions{}
				err := opt(opts)

				if tt.expectedValue == 0 {
					require.Error(t, err)
					return
				}

				require.NoError(t, err)
				assert.Equal(t, tt.expectedValue, opts.Limit)
			}
		})
	}
}
