package httpapi_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExternalHTTPAssemblyAndImportGraph(t *testing.T) {
	t.Parallel()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = "testdata/external"
		cmd.Env = append(os.Environ(), "GOWORK=off")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("external %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("test", "-race", "-mod=readonly", "./...")
	graph := run("list", "-mod=readonly", "-deps", "-test", "-f", "{{.ImportPath}}", "./...")
	for _, path := range strings.Fields(graph) {
		for _, forbidden := range []string{
			"github.com/stacklok/toolhive-registry-server/internal/",
			"github.com/stacklok/toolhive-registry-server/database",
			"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres",
			"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest",
			"github.com/stacklok/toolhive-registry-server/pkg/registry/operator",
			"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile",
			"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence/conformance",
			"github.com/cedar-policy/", "github.com/golang-jwt/",
			"github.com/jackc/pgx", "github.com/spf13/viper", "k8s.io/", "sigs.k8s.io/controller-runtime",
		} {
			if strings.HasPrefix(path, forbidden) {
				t.Fatalf("HTTP core imports %s", path)
			}
		}
	}
}
