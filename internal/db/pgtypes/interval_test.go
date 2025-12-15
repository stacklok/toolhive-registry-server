package pgtypes

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInterval_Scan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    interface{}
		expected Interval
		wantErr  bool
	}{
		{
			name:  "nil value",
			input: nil,
			expected: Interval{
				Valid:    false,
				Duration: 0,
			},
			wantErr: false,
		},
		{
			name: "30 minutes",
			input: pgtype.Interval{
				Microseconds: 30 * 60 * 1000000, // 30 minutes in microseconds
				Days:         0,
				Months:       0,
				Valid:        true,
			},
			expected: Interval{
				Valid:    true,
				Duration: 30 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "1 hour",
			input: pgtype.Interval{
				Microseconds: 60 * 60 * 1000000, // 1 hour in microseconds
				Days:         0,
				Months:       0,
				Valid:        true,
			},
			expected: Interval{
				Valid:    true,
				Duration: 1 * time.Hour,
			},
			wantErr: false,
		},
		{
			name: "2 days",
			input: pgtype.Interval{
				Microseconds: 0,
				Days:         2,
				Months:       0,
				Valid:        true,
			},
			expected: Interval{
				Valid:    true,
				Duration: 48 * time.Hour, // 2 days
			},
			wantErr: false,
		},
		{
			name: "invalid pgtype.Interval",
			input: pgtype.Interval{
				Microseconds: 0,
				Days:         0,
				Months:       0,
				Valid:        false,
			},
			expected: Interval{
				Valid:    false,
				Duration: 0,
			},
			wantErr: false,
		},
		{
			name:  "string: 30 minutes",
			input: "00:30:00",
			expected: Interval{
				Valid:    true,
				Duration: 30 * time.Minute,
			},
			wantErr: false,
		},
		{
			name:  "string: 1 hour",
			input: "01:00:00",
			expected: Interval{
				Valid:    true,
				Duration: 1 * time.Hour,
			},
			wantErr: false,
		},
		{
			name:  "string: 2 hours 15 minutes",
			input: "02:15:00",
			expected: Interval{
				Valid:    true,
				Duration: 2*time.Hour + 15*time.Minute,
			},
			wantErr: false,
		},
		{
			name:  "byte slice: 1 hour",
			input: []byte("01:00:00"),
			expected: Interval{
				Valid:    true,
				Duration: 1 * time.Hour,
			},
			wantErr: false,
		},
		{
			name:     "string: invalid format",
			input:    "not-an-interval",
			expected: Interval{},
			wantErr:  true,
		},
		{
			name:     "unsupported type",
			input:    123,
			expected: Interval{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var i Interval
			err := i.Scan(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected.Valid, i.Valid)
			assert.Equal(t, tt.expected.Duration, i.Duration)
		})
	}
}

func TestInterval_Value(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		interval Interval
		expected interface{}
	}{
		{
			name: "null interval",
			interval: Interval{
				Valid:    false,
				Duration: 0,
			},
			expected: nil,
		},
		{
			name: "30 minutes",
			interval: Interval{
				Valid:    true,
				Duration: 30 * time.Minute,
			},
			expected: pgtype.Interval{
				Microseconds: 30 * 60 * 1000000,
				Days:         0,
				Months:       0,
				Valid:        true,
			},
		},
		{
			name: "1 hour",
			interval: Interval{
				Valid:    true,
				Duration: 1 * time.Hour,
			},
			expected: pgtype.Interval{
				Microseconds: 60 * 60 * 1000000,
				Days:         0,
				Months:       0,
				Valid:        true,
			},
		},
		{
			name: "2 hours 15 minutes",
			interval: Interval{
				Valid:    true,
				Duration: 2*time.Hour + 15*time.Minute,
			},
			expected: pgtype.Interval{
				Microseconds: (2*60 + 15) * 60 * 1000000,
				Days:         0,
				Months:       0,
				Valid:        true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			val, err := tt.interval.Value()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, val)
		})
	}
}

func TestInterval_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		interval Interval
		expected string
	}{
		{
			name: "null interval",
			interval: Interval{
				Valid: false,
			},
			expected: "NULL",
		},
		{
			name: "30 minutes",
			interval: Interval{
				Valid:    true,
				Duration: 30 * time.Minute,
			},
			expected: "30m0s",
		},
		{
			name: "1 hour",
			interval: Interval{
				Valid:    true,
				Duration: 1 * time.Hour,
			},
			expected: "1h0m0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.interval.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected Interval
		wantErr  bool
	}{
		{
			name:  "empty string",
			input: "",
			expected: Interval{
				Valid:    false,
				Duration: 0,
			},
			wantErr: false,
		},
		{
			name:  "30 minutes",
			input: "30m",
			expected: Interval{
				Valid:    true,
				Duration: 30 * time.Minute,
			},
			wantErr: false,
		},
		{
			name:  "1 hour",
			input: "1h",
			expected: Interval{
				Valid:    true,
				Duration: 1 * time.Hour,
			},
			wantErr: false,
		},
		{
			name:  "2 hours 30 minutes",
			input: "2h30m",
			expected: Interval{
				Valid:    true,
				Duration: 2*time.Hour + 30*time.Minute,
			},
			wantErr: false,
		},
		{
			name:     "invalid duration",
			input:    "invalid",
			expected: Interval{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ParseDuration(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected.Valid, result.Valid)
			assert.Equal(t, tt.expected.Duration, result.Duration)
		})
	}
}

func TestNewInterval(t *testing.T) {
	t.Parallel()

	d := 30 * time.Minute
	i := NewInterval(d)

	assert.True(t, i.Valid)
	assert.Equal(t, d, i.Duration)
}

func TestNewNullInterval(t *testing.T) {
	t.Parallel()

	i := NewNullInterval()

	assert.False(t, i.Valid)
	assert.Equal(t, time.Duration(0), i.Duration)
}
