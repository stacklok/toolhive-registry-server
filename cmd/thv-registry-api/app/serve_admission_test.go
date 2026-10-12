package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest,tparallel // runServe consumes the CLI's global Viper configuration.
func TestServeAdmissionPreflightAfterOverride(t *testing.T) {
	previous := viper.Get("config")
	t.Cleanup(func() { viper.Set("config", previous) })
	for _, tc := range []struct{ name, override, want string }{
		{"invalid CLI mode", "invalid", "invalid auth.mode"},
		{"anonymous CLI override with admin", "anonymous", "admin.enabled requires OAuth"},
		{"invalid policy before migrations", "", "policy schema validation failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			policyPath := filepath.Join(dir, "policy.cedar")
			require.NoError(t, os.WriteFile(policyPath, []byte(`permit(principal,action == Registry::Action::"unknown",resource);`), 0600))
			configPath := filepath.Join(dir, "config.yaml")
			data := fmt.Sprintf(`sources:
 - name: managed
   managed: {}
registries:
 - name: default
   sources: [managed]
database:
 host: 127.0.0.1
 port: 1
 user: registry
 database: registry
 sslMode: disable
auth:
 mode: oauth
 oauth:
  providers:
   - name: test
     issuerUrl: https://id.example.org
     audience: registry-api
admin:
 enabled: true
authorization:
 policyFile: %s
 allowAnonymousCatalog: true
`, policyPath)
			require.NoError(t, os.WriteFile(configPath, []byte(data), 0600))
			viper.Set("config", configPath)
			command := &cobra.Command{}
			command.Flags().String("auth-mode", tc.override, "")
			require.ErrorContains(t, runServe(command, nil), tc.want)
		})
	}
}
