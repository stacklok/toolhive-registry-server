package storage

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// EffectiveNamespaces canonicalizes a Kubernetes source's namespace scope:
// explicit namespaces, else the global watch namespaces, else all. The exact
// scope is persisted; the operator applies no fallback of its own.
func EffectiveNamespaces(explicit []string, watch string) ([]string, error) {
	namespaces := slices.Clone(explicit)
	if len(namespaces) == 0 && watch != "" {
		namespaces = strings.Split(watch, ",")
	}
	for i, n := range namespaces {
		namespaces[i] = strings.TrimSpace(n)
		if namespaces[i] == "" {
			return nil, fmt.Errorf("%w: namespace scope contains an empty namespace; omit the whole scope for all namespaces",
				persistence.ErrInvalid)
		}
		if err := config.ValidateSourceName(namespaces[i]); err != nil {
			return nil, fmt.Errorf("%w: invalid namespace scope", persistence.ErrInvalid)
		}
	}
	slices.Sort(namespaces)
	return slices.Compact(namespaces), nil
}
