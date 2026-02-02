package telemetry

import (
	"testing"

	"github.com/aws/smithy-go/ptr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_GetServiceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name:     "returns default when empty",
			config:   &Config{},
			expected: DefaultServiceName,
		},
		{
			name: "returns configured value",
			config: &Config{
				ServiceName: "my-service",
			},
			expected: "my-service",
		},
		{
			name: "returns default when whitespace only",
			config: &Config{
				ServiceName: "",
			},
			expected: DefaultServiceName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.config.GetServiceName()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_GetServiceVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name:     "returns unknown when empty",
			config:   &Config{},
			expected: "unknown",
		},
		{
			name: "returns configured value",
			config: &Config{
				ServiceVersion: "1.2.3",
			},
			expected: "1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.config.GetServiceVersion()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_GetEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name:     "returns default when empty",
			config:   &Config{},
			expected: DefaultEndpoint,
		},
		{
			name: "returns configured value",
			config: &Config{
				Endpoint: "collector.example.com:4318",
			},
			expected: "collector.example.com:4318",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.config.GetEndpoint()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_GetInsecure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   *Config
		expected bool
	}{
		{
			name:     "returns false when not set",
			config:   &Config{},
			expected: false,
		},
		{
			name: "returns true when set",
			config: &Config{
				Insecure: true,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.config.GetInsecure()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name: "disabled config is valid",
			config: &Config{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "enabled config with no tracing or metrics is valid",
			config: &Config{
				Enabled:     true,
				ServiceName: "test",
			},
			wantErr: false,
		},
		{
			name: "valid full config",
			config: &Config{
				Enabled:        true,
				ServiceName:    "test",
				ServiceVersion: "1.0.0",
				Endpoint:       "localhost:4318",
				Insecure:       true,
				Tracing: &TracingConfig{
					Enabled:  true,
					Sampling: ptr.Float64(0.5),
				},
				Metrics: &MetricsConfig{
					Enabled: true,
				},
			},
			wantErr: false,
		},
		{
			name: "disabled tracing with invalid config is valid",
			config: &Config{
				Enabled: true,
				Tracing: &TracingConfig{
					Enabled:  false,
					Sampling: ptr.Float64(-1),
				},
			},
			wantErr: false,
		},
		{
			name: "disabled metrics config is valid",
			config: &Config{
				Enabled: true,
				Metrics: &MetricsConfig{
					Enabled: false,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTracingConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  *TracingConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name: "disabled config is valid",
			config: &TracingConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid enabled config",
			config: &TracingConfig{
				Enabled:  true,
				Sampling: ptr.Float64(1.0),
			},
			wantErr: false,
		},
		{
			name: "valid config with custom sampling",
			config: &TracingConfig{
				Enabled:  true,
				Sampling: ptr.Float64(0.5),
			},
			wantErr: false,
		},
		{
			name: "valid config with nil sampling uses default",
			config: &TracingConfig{
				Enabled:  true,
				Sampling: nil,
			},
			wantErr: false,
		},
		{
			name: "sampling above 1.0",
			config: &TracingConfig{
				Enabled:  true,
				Sampling: ptr.Float64(1.1),
			},
			wantErr: true,
			errMsg:  "sampling must be greater than 0.0",
		},
		{
			name: "negative sampling",
			config: &TracingConfig{
				Enabled:  true,
				Sampling: ptr.Float64(-0.1),
			},
			wantErr: true,
			errMsg:  "sampling must be greater than 0.0",
		},
		{
			name: "zero sampling is invalid when tracing enabled",
			config: &TracingConfig{
				Enabled:  true,
				Sampling: ptr.Float64(0),
			},
			wantErr: true,
			errMsg:  "sampling must be greater than 0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMetricsConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  *MetricsConfig
		wantErr bool
	}{
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name: "disabled config is valid",
			config: &MetricsConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid enabled config",
			config: &MetricsConfig{
				Enabled: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTracingConfig_GetSampling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   *TracingConfig
		expected float64
	}{
		{
			name: "returns default when sampling is nil",
			config: &TracingConfig{
				Enabled:  true,
				Sampling: nil,
			},
			expected: DefaultSampling,
		},
		{
			name: "returns explicit value when set",
			config: &TracingConfig{
				Enabled:  true,
				Sampling: ptr.Float64(0.5),
			},
			expected: 0.5,
		},
		{
			name: "returns 1.0 when set to full sampling",
			config: &TracingConfig{
				Enabled:  true,
				Sampling: ptr.Float64(1.0),
			},
			expected: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.config.GetSampling()
			assert.Equal(t, tt.expected, result)
		})
	}
}
