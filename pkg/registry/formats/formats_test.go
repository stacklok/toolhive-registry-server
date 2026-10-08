package formats_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

func TestPublicationValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		skill  *model.Skill
		plugin *model.Plugin
		valid  bool
	}{
		{"skill version can be non-semver", &model.Skill{Namespace: "io.example", Name: "s", Version: "git-rev"}, nil, true},
		{"skill needs namespace", &model.Skill{Name: "s", Version: "1"}, nil, false},
		{"skill needs name", &model.Skill{Namespace: "io.example", Version: "1"}, nil, false},
		{"skill needs version", &model.Skill{Namespace: "io.example", Name: "s"}, nil, false},
		{"plugin version can be non-semver", nil, &model.Plugin{Namespace: "io.example", Name: "p", Version: "git-rev"}, true},
		{"plugin needs namespace", nil, &model.Plugin{Name: "p", Version: "1"}, false},
		{"plugin needs name", nil, &model.Plugin{Namespace: "io.example", Version: "1"}, false},
		{"plugin needs version", nil, &model.Plugin{Namespace: "io.example", Name: "p"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var err error
			if tc.skill != nil {
				err = formats.ValidatePublishedSkill(tc.skill)
			} else {
				err = formats.ValidatePublishedPlugin(tc.plugin)
			}
			if (err == nil) != tc.valid {
				t.Fatalf("validation error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestDecodeUpstream(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		data  string
		valid bool
	}{
		{"empty", "", false},
		{"malformed", "{", false},
		{"no servers", `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[]}}`, false},
		{"server", `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"name":"com.example/weather","description":"Weather","version":"1.0","packages":[]}]}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := formats.DecodeUpstream([]byte(tc.data))
			if (err == nil) != tc.valid {
				t.Fatalf("registry = %v, error = %v, valid = %v", got, err, tc.valid)
			}
			if tc.valid && len(got.Data.Servers) != 1 {
				t.Fatalf("servers = %d", len(got.Data.Servers))
			}
		})
	}
}

func TestPersistenceExternalModule(t *testing.T) {
	t.Parallel()
	fixtureDir := "../persistence/testdata/external"
	compileExternalFixture(t, fixtureDir)
	output := runExternalGo(t, fixtureDir, "list", "-mod=readonly", "-deps", "-f", "{{.ImportPath}}",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/model",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/formats")
	paths := make(map[string]bool)
	for _, path := range strings.Fields(string(output)) {
		paths[path] = true
	}
	for _, expected := range []string{
		"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/model",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/formats",
	} {
		if !paths[expected] {
			t.Fatalf("neutral package missing from import graph: %s", expected)
		}
	}
	for path := range paths {
		for _, forbidden := range []string{
			"github.com/stacklok/toolhive-registry-server/internal",
			"github.com/stacklok/toolhive-registry-server/database",
			"github.com/jackc/pgx",
			"sigs.k8s.io/controller-runtime", "k8s.io",
		} {
			if isPackageOrChild(path, forbidden) {
				t.Fatalf("neutral package imports %s", path)
			}
		}
	}
}

func TestFormatOnlyExternalModule(t *testing.T) {
	t.Parallel()
	// testdata/external is a separate module; a same-module _test package
	// cannot establish that these APIs are importable by another module.
	fixtureDir := "testdata/external"
	compileExternalFixture(t, fixtureDir)

	paths := listPublicPackageDependencies(t, fixtureDir)
	for _, publicPackage := range []string{
		"github.com/stacklok/toolhive-registry-server/pkg/registry/formats",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/model",
	} {
		if !paths[publicPackage] {
			t.Fatalf("public package missing from dependency graph: %s", publicPackage)
		}
	}

	for path := range paths {
		for _, forbidden := range []string{
			"github.com/jackc/pgx",
			"github.com/stacklok/toolhive-registry-server/internal",
			"github.com/stacklok/toolhive-registry-server/database",
			"sigs.k8s.io/controller-runtime",
			"k8s.io",
			"github.com/spf13/viper",
		} {
			if isPackageOrChild(path, forbidden) {
				t.Fatalf("format-only package import graph includes forbidden package %s", path)
			}
		}
	}
}

func compileExternalFixture(t *testing.T, fixtureDir string) {
	t.Helper()
	runExternalGo(t, fixtureDir, "test", "-mod=readonly", "./...")
}

func listPublicPackageDependencies(t *testing.T, fixtureDir string) map[string]bool {
	t.Helper()
	output := runExternalGo(t, fixtureDir,
		"list", "-mod=readonly", "-deps", "-test", "-f", "{{.ImportPath}}",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/formats",
		"github.com/stacklok/toolhive-registry-server/pkg/registry/model",
	)
	paths := make(map[string]bool)
	for _, path := range strings.Fields(string(output)) {
		paths[path] = true
	}
	return paths
}

func runExternalGo(t *testing.T, fixtureDir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = fixtureDir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

func isPackageOrChild(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func TestMetadataDoesNotLeakToWire(t *testing.T) {
	t.Parallel()
	skill := &model.Skill{ID: "db-id", Namespace: "io.example", Name: "s", Version: "v1", IsLatest: true}
	plugin := &model.Plugin{ID: "db-id", Namespace: "io.example", Name: "p", Version: "v1", IsLatest: true}
	for _, payload := range []any{formats.SkillPayload(skill), formats.PluginPayload(plugin)} {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "db-id") || strings.Contains(string(b), "isLatest") || strings.Contains(string(b), "createdAt") {
			t.Fatalf("catalog metadata leaked: %s", b)
		}
	}
}
