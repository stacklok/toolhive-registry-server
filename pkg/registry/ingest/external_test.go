package ingest_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExternalAssembly(t *testing.T) {
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
	run("test", "-mod=readonly", "./...")
	graph := run("list", "-mod=readonly", "-deps", "-test", "-f", "{{.ImportPath}}", "./...")
	for _, path := range strings.Fields(graph) {
		for _, forbidden := range []string{"k8s.io/", "sigs.k8s.io/controller-runtime", "github.com/jackc/pgx", "github.com/spf13/viper", "github.com/stacklok/toolhive-registry-server/database", "github.com/stacklok/toolhive-registry-server/internal/config", "github.com/stacklok/toolhive-registry-server/internal/auth", "github.com/stacklok/toolhive-registry-server/internal/app"} {
			if strings.HasPrefix(path, forbidden) {
				t.Fatalf("core ingestion assembly imports %s", path)
			}
		}
	}
}
