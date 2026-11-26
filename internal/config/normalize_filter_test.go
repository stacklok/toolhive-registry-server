package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/filtering"
)

// TestNormalizeNameFilter_SelectsWhatV154Selected checks the rewrite against
// the name filter itself (unchanged since v1.5.4): for every name, the
// normalized filter includes exactly what v1.5.4's filter included with the
// invalid patterns in place.
func TestNormalizeNameFilter_SelectsWhatV154Selected(t *testing.T) {
	t.Parallel()

	names := []string{
		"io.a/server", "io.b/server", "io.c/server", "io.d/server-beta", "io.a/server-beta",
		"com.example/x", "[bad", "{a,b", "a",
	}
	filters := []config.NameFilterConfig{
		{Exclude: []string{"[bad"}},
		{Exclude: []string{"*-beta", "{a,b"}},
		{Include: []string{"io.a/*"}, Exclude: []string{"[bad"}},
		{Include: []string{"[bad"}},
		{Include: []string{"[bad"}, Exclude: []string{"*-beta"}},
		{Include: []string{"io.a/*", "[bad", "io.c/*"}},
		{Include: []string{"io.a/*", "io.b/*", "{x", "io.c/*"}, Exclude: []string{"*-beta"}},
		{Include: []string{"io.*", "\\"}, Exclude: []string{"io.b/*"}},
	}
	filter := filtering.NewDefaultNameFilter()
	for _, original := range filters {
		normalized, warning := config.NormalizeNameFilter(&original)
		assert.NotEmpty(t, warning, "filter %+v holds an invalid pattern", original)
		for _, name := range names {
			want, _ := filter.ShouldInclude(name, original.Include, original.Exclude)
			got, _ := filter.ShouldInclude(name, normalized.Include, normalized.Exclude)
			assert.Equal(t, want, got, "filter %+v normalized to %+v, name %q", original, *normalized, name)
		}
	}
}
