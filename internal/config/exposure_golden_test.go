package config

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// Frozen producer encoding from schema-29's v1 projection at c2f073f. In
// particular, absent claim/authz/public-path fields are encoded as JSON null,
// not omitted. This literal must not be generated from current config structs.
const exposureV1Golden = `{"Version":1,"Sources":[{"Name":"future","Git":null,"API":null,"File":null,"Managed":{},"Kubernetes":null,"SyncPolicy":null,"Filter":null,"Claims":null}],"Views":[{"Name":"public","Claims":null,"Sources":["future"]}],"Auth":{"Mode":"anonymous","PublicPaths":null,"OAuth":null,"Authz":null,"InsecureAllowHTTP":false},"Authorization":null,"Admin":{"Enabled":false},"Namespace":"","Retired":{"auth.publicpaths":null,"sources[0].claims":null},"Environment":{"AUTH_PUBLICPATHS":"[]"},"Policy":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`

func TestSchema29ConfigFingerprintGolden(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Sources:         []SourceConfig{{Name: "future", Managed: &ManagedConfig{}}},
		Registries:      []RegistryConfig{{Name: "public", Sources: []string{"future"}}},
		Auth:            &AuthConfig{Mode: AuthModeAnonymous},
		exposureRetired: map[string]any{"auth.publicpaths": nil, "sources[0].claims": nil},
		exposureEnv:     map[string]string{"AUTH_PUBLICPATHS": "[]"},
	}
	review, err := cfg.ExposureReview("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(exposureV1Golden))), review.Revision)
	require.Equal(t, []string{"auth.publicpaths", "env.auth.publicpaths", "sources[0].claims"}, review.RetiredFields)
	require.Error(t, cfg.ValidateCatalogCutover(), "compatibility decoding never activates retired input")
}
