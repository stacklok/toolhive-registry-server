package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeSyncInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		interval string
		want     string
		warning  string
	}{
		{name: "positive interval is kept", interval: "30m", want: "30m"},
		{name: "one second is kept", interval: "1s", want: "1s"},
		{name: "zero uses v1.5.4 cadence", interval: "0s", want: "2m0s", warning: "is not positive"},
		{name: "bare zero uses v1.5.4 cadence", interval: "0", want: "2m0s", warning: "is not positive"},
		{name: "negative uses v1.5.4 cadence", interval: "-5m", want: "2m0s", warning: "is not positive"},
		{name: "sub-second is raised to one second", interval: "500ms", want: "1s", warning: "shorter than 1s"},
		{name: "nanoseconds are raised to one second", interval: "1ns", want: "1s", warning: "shorter than 1s"},
		{name: "sub-microsecond precision is truncated", interval: "1h0.0000015s", want: "1h0m0.000001s",
			warning: "sub-microsecond"},
		{name: "unparseable is left for validation", interval: "soon", want: "soon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, warning := NormalizeSyncInterval(tt.interval)
			assert.Equal(t, tt.want, got)
			if tt.warning == "" {
				assert.Empty(t, warning)
			} else {
				assert.Contains(t, warning, tt.warning)
			}
		})
	}
}

func TestNormalizeFileTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    *FileConfig
		want    string
		warning string
	}{
		{name: "nil file", file: nil, want: ""},
		{name: "no timeout", file: &FileConfig{URL: "https://example.com/r.json"}, want: ""},
		{name: "URL timeout is kept", file: &FileConfig{URL: "https://example.com/r.json", Timeout: "45s"}, want: "45s"},
		{name: "path timeout is ignored", file: &FileConfig{Path: "/data/r.json", Timeout: "30s"}, want: "",
			warning: "applies only to file.url"},
		{name: "unparseable path timeout is ignored", file: &FileConfig{Path: "/data/r.json", Timeout: "later"}, want: "",
			warning: "applies only to file.url"},
		{name: "inline timeout is ignored", file: &FileConfig{Data: "{}", Timeout: "30s"}, want: "",
			warning: "applies only to file.url"},
		{name: "zero URL timeout uses v1.5.4's 10s", file: &FileConfig{URL: "https://example.com/r.json", Timeout: "0s"},
			want: "10s", warning: "the timeout v1.5.4 applied"},
		{name: "negative URL timeout uses the default", file: &FileConfig{URL: "https://example.com/r.json", Timeout: "-1s"},
			want: "", warning: "can no longer be disabled"},
		{name: "unparseable URL timeout is left for validation",
			file: &FileConfig{URL: "https://example.com/r.json", Timeout: "later"}, want: "later"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, warning := NormalizeFileTimeout(tt.file)
			assert.Equal(t, tt.want, got)
			if tt.warning == "" {
				assert.Empty(t, warning)
			} else {
				assert.Contains(t, warning, tt.warning)
			}
		})
	}
}

func TestNormalizeNameFilter(t *testing.T) {
	t.Parallel()

	excludeAll := &NameFilterConfig{Exclude: []string{"*"}}
	tests := []struct {
		name    string
		in      *NameFilterConfig
		want    *NameFilterConfig
		warning string
	}{
		{name: "nil filter", in: nil, want: nil},
		{
			name: "valid patterns are kept",
			in:   &NameFilterConfig{Include: []string{"io.example/*"}, Exclude: []string{"*-beta"}},
			want: &NameFilterConfig{Include: []string{"io.example/*"}, Exclude: []string{"*-beta"}},
		},
		{
			name:    "invalid exclude pattern excludes everything",
			in:      &NameFilterConfig{Include: []string{"io.example/*"}, Exclude: []string{"*-beta", "[unclosed"}},
			want:    excludeAll,
			warning: `exclude pattern "[unclosed"`,
		},
		{
			name:    "glob-only invalid exclude pattern excludes everything",
			in:      &NameFilterConfig{Exclude: []string{"{a,b"}},
			want:    excludeAll,
			warning: `exclude pattern "{a,b"`,
		},
		{
			name:    "first include pattern invalid excludes everything",
			in:      &NameFilterConfig{Include: []string{"[unclosed", "io.example/*"}, Exclude: []string{"*-beta"}},
			want:    excludeAll,
			warning: `include pattern "[unclosed"`,
		},
		{
			name:    "later include pattern invalid keeps the patterns before it",
			in:      &NameFilterConfig{Include: []string{"io.a/*", "io.b/*", "[bad", "io.c/*"}, Exclude: []string{"*-beta"}},
			want:    &NameFilterConfig{Include: []string{"io.a/*", "io.b/*"}, Exclude: []string{"*-beta"}},
			warning: `include pattern "[bad"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, warning := NormalizeNameFilter(tt.in)
			assert.Equal(t, tt.want, got)
			if tt.warning == "" {
				assert.Empty(t, warning)
			} else {
				assert.Contains(t, warning, tt.warning)
			}
		})
	}
}
