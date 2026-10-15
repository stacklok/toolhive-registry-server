package chart_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomPolicyMountPathValidation(t *testing.T) {
	t.Parallel()
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm is required for chart rendering")
	}
	for _, path := range []string{"/etc/registry-policies/policies.cedar", "/other/policy.cedar"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			output, err := exec.CommandContext(t.Context(), helm, "template", "custom", ".",
				"--set", "image.registryServerUrl=example.invalid/registry:catalog",
				"--set", "policy.existingConfigMap=custom-policy",
				"--set", "config.authorization.policyFile="+path).CombinedOutput()
			if path == "/other/policy.cedar" {
				require.Error(t, err)
				require.Contains(t, string(output), "policy.existingConfigMap requires config.authorization.policyFile")
			} else {
				require.NoError(t, err, "%s", output)
				require.Contains(t, string(output), "cedar-policy")
				require.Contains(t, string(output), "policyFile: "+path)
			}
		})
	}
}
