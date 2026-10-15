package operator_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestExternalOperator(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"test", "-mod=readonly", "./..."}, {"list", "-mod=readonly", "-deps", "-test", "-f", "{{.ImportPath}}", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = "testdata/external"
		cmd.Env = append(os.Environ(), "GOWORK=off")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("external %v: %v\n%s", args, err, out)
		}
		if args[0] == "list" {
			for _, path := range strings.Fields(string(out)) {
				for _, forbidden := range []string{"github.com/jackc/pgx", "github.com/spf13/viper", "github.com/stacklok/toolhive-registry-server/internal/config", "github.com/stacklok/toolhive-registry-server/internal/auth", "github.com/stacklok/toolhive-registry-server/internal/app", "github.com/stacklok/toolhive-registry-server/database"} {
					if strings.HasPrefix(path, forbidden) {
						t.Fatalf("operator assembly imports %s", path)
					}
				}
			}
		}
	}
}
