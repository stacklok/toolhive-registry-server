package chart_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/authorization"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func TestCIPolicyMount(t *testing.T) {
	t.Parallel()
	valuesFiles, err := filepath.Glob("ci/*-values.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, valuesFiles, "chart-testing values fixtures must exist")

	for _, valuesFile := range valuesFiles {
		t.Run(filepath.Base(valuesFile), func(t *testing.T) {
			t.Parallel()
			helm, err := exec.LookPath("helm")
			if err != nil {
				t.Skip("helm is required for the CI chart rendering test")
			}
			output, err := exec.CommandContext(t.Context(), helm, "template", "ci", ".", "--namespace", "toolhive-test", "-f", valuesFile).CombinedOutput()
			require.NoError(t, err, "%s", output)
			decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
			var deployment appsv1.Deployment
			var configMap corev1.ConfigMap
			for {
				var document json.RawMessage
				err := decoder.Decode(&document)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				var kind struct{ Kind string }
				require.NoError(t, json.Unmarshal(document, &kind))
				switch kind.Kind {
				case "Deployment":
					require.NoError(t, json.Unmarshal(document, &deployment))
				case "ConfigMap":
					require.NoError(t, json.Unmarshal(document, &configMap))
				}
			}
			require.NotEmpty(t, deployment.Name)
			require.NotEmpty(t, configMap.Data["config.yaml"])
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(configMap.Data["config.yaml"]), 0600))
			cfg, err := config.LoadConfig(config.WithConfigPath(configPath))
			require.NoError(t, err)
			require.NoError(t, cfg.ValidateAdmission())
			require.False(t, cfg.Admin.Enabled)
			require.Equal(t, config.AuthModeAnonymous, cfg.Auth.Mode)
			require.True(t, cfg.Authorization.AllowAnonymousCatalog)

			policyFixture, err := os.ReadFile("../../../.github/registry-ci-policy.yaml")
			require.NoError(t, err)
			var policyMap corev1.ConfigMap
			require.NoError(t, k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(policyFixture), 4096).Decode(&policyMap))
			pod := deployment.Spec.Template.Spec
			require.Len(t, pod.Containers, 1)
			container := pod.Containers[0]
			require.Contains(t, container.Args, "/etc/registry-server/config.yaml")
			require.Equal(t, "/readiness", container.ReadinessProbe.HTTPGet.Path)
			require.Equal(t, "internal-http", container.ReadinessProbe.HTTPGet.Port.StrVal)
			mounted := false
			for _, mount := range container.VolumeMounts {
				for _, volume := range pod.Volumes {
					if volume.Name != mount.Name || volume.ConfigMap == nil || volume.ConfigMap.Name != policyMap.Name {
						continue
					}
					require.True(t, mount.ReadOnly)
					require.Len(t, volume.ConfigMap.Items, 1)
					item := volume.ConfigMap.Items[0]
					require.Equal(t, cfg.Authorization.PolicyFile, filepath.Join(mount.MountPath, item.Path))
					require.Equal(t, "forbid(principal, action, resource);\n", policyMap.Data[item.Key])
					policyPath := filepath.Join(t.TempDir(), item.Path)
					require.NoError(t, os.WriteFile(policyPath, []byte(policyMap.Data[item.Key]), 0600))
					engine, err := authorization.Load(policyPath)
					require.NoError(t, err)
					gate, err := authorization.NewRegistrar(engine, nil, nil)
					require.NoError(t, err)
					mw, _, err := auth.NewAdmissionAuthMiddleware(context.Background(), cfg.Auth, cfg.Authorization.AllowAnonymousCatalog, nil)
					require.NoError(t, err)
					router := chi.NewRouter()
					require.NoError(t, gate.Register(router, "GET", "/openapi.json", authorization.MetadataRead,
						http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("CI policy allowed access") }), mw))
					response := httptest.NewRecorder()
					router.ServeHTTP(response, httptest.NewRequest("GET", "/openapi.json", nil))
					require.Equal(t, http.StatusForbidden, response.Code)
					mounted = true
				}
			}
			require.True(t, mounted, "policy ConfigMap must be mounted at the configured file path")
		})
	}
}
