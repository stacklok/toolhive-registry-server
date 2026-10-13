package postgres_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExternalCompleteAssembly(t *testing.T) {
	t.Parallel()
	_, pool := newBackend(t)
	cmd := exec.CommandContext(t.Context(), "go", "test", "-race", "-mod=readonly", "-count=1", "./...")
	cmd.Dir = "testdata/external"
	cmd.Env = append(os.Environ(), "GOWORK=off", "REGISTRY_FIXTURE_DSN="+pool.Config().ConnString())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external assembly: %v\n%s", err, out)
	}
	// Adapter transitive internal SQL/pgx imports are intentional. The portable
	// component graph is checked separately, not inferred from this full assembly.
	cmd = exec.CommandContext(t.Context(), "go", "list", "-mod=readonly", "-deps", "-f", "{{.ImportPath}}",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/model",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/formats",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/catalog",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi")
	cmd.Dir = "testdata/external"
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("portable graph: %v\n%s", err, out)
	}
	for _, path := range strings.Fields(string(out)) {
		for _, forbidden := range []string{"github.com/stacklok/toolhive-registry-server/internal/", "github.com/stacklok/toolhive-registry-server/database", "github.com/stacklok/toolhive-registry-server/pkg/registry/postgres", "github.com/jackc/pgx", "github.com/spf13/viper", "k8s.io/", "sigs.k8s.io/controller-runtime"} {
			if strings.HasPrefix(path, forbidden) {
				t.Fatalf("portable components import %s", path)
			}
		}
	}
}
