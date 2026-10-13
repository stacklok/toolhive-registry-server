package authorization

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/authorization/policydata"
)

func TestDefaultPolicyAndCustomReplacement(t *testing.T) {
	t.Parallel()
	engine, err := Load("")
	require.NoError(t, err)
	require.NotNil(t, engine.policies, "default must use the actual Cedar evaluator")
	require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(policydata.Default))), engine.Revision())
	for _, identity := range []auth.VerifiedIdentity{{Anonymous: true}, {Issuer: "https://id.example.org", Subject: "user", Claims: map[string]any{"iss": "https://id.example.org", "sub": "user"}}} {
		p, err := principal(identity, nil)
		require.NoError(t, err)
		for _, action := range []Action{CatalogRead, MetadataRead, SourcesRead, SourcesWrite, SourcesDelete, ViewsRead, ViewsWrite, ViewsDelete, EntriesPublish, EntriesDelete} {
			require.NoError(t, engine.evaluate(p, action))
		}
		require.ErrorIs(t, engine.evaluate(p, "unknown"), ErrEvidence)
	}
	path := filepath.Join(t.TempDir(), "restrictive.cedar")
	require.NoError(t, os.WriteFile(path, []byte(permitCatalog), 0600))
	custom, err := Load(path)
	require.NoError(t, err)
	p, err := principal(auth.VerifiedIdentity{Anonymous: true}, nil)
	require.NoError(t, err)
	require.NoError(t, custom.evaluate(p, CatalogRead))
	require.ErrorIs(t, custom.evaluate(p, SourcesWrite), ErrDenied, "permit-only custom policy must replace, not union with, default")
}
