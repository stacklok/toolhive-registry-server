package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A server name annotated by several kinds takes the claims of the last kind
// in v1.5.4's order: MCPServer, VirtualMCPServer, MCPRemoteProxy. Namespace
// order only breaks ties within a kind.
func TestAnnotationClaimsKindPrecedence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		scopes []listScope
		want   string
	}{
		{
			name:   "proxy beats server in an earlier namespace order",
			scopes: []listScope{{"b", "MCPServer"}, {"a", "MCPRemoteProxy"}},
			want:   "MCPRemoteProxy",
		},
		{
			name:   "virtual server beats server",
			scopes: []listScope{{"a", "VirtualMCPServer"}, {"b", "MCPServer"}},
			want:   "VirtualMCPServer",
		},
		{
			name:   "all three kinds",
			scopes: []listScope{{"z", "MCPRemoteProxy"}, {"a", "VirtualMCPServer"}, {"m", "MCPServer"}},
			want:   "MCPRemoteProxy",
		},
		{
			name:   "same kind: later namespace wins",
			scopes: []listScope{{"b", "MCPServer"}, {"a", "MCPServer"}},
			want:   "MCPServer/b",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			claims := NewAnnotationClaims()
			for _, scope := range tc.scopes {
				value := scope.kind
				if tc.want == "MCPServer/b" {
					value = scope.kind + "/" + scope.namespace
				}
				claims.replace("src", scope, map[string][]byte{"io.example/server": []byte(value)})
			}
			require.Equal(t, tc.want, string(claims.EntryClaims("src")["io.example/server"]))
		})
	}
}
