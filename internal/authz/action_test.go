package authz

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRouteAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		// Read operations (GET requests)
		{
			name:   "GET servers list is read",
			method: "GET",
			path:   "/registry/test/v0.1/servers",
			want:   ActionRead,
		},
		{
			name:   "GET registries list is read",
			method: "GET",
			path:   "/extension/v0/registries",
			want:   ActionRead,
		},
		{
			name:   "GET specific registry is read",
			method: "GET",
			path:   "/extension/v0/registries/myregistry",
			want:   ActionRead,
		},
		{
			name:   "GET server versions is read",
			method: "GET",
			path:   "/registry/test/v0.1/servers/myserver/versions",
			want:   ActionRead,
		},
		{
			name:   "GET health is read",
			method: "GET",
			path:   "/health",
			want:   ActionRead,
		},
		{
			name:   "GET root is read",
			method: "GET",
			path:   "/",
			want:   ActionRead,
		},
		{
			name:   "GET arbitrary path is read",
			method: "GET",
			path:   "/some/arbitrary/path",
			want:   ActionRead,
		},

		// Write operations
		{
			name:   "POST publish is write",
			method: "POST",
			path:   "/registry/test/v0.1/publish",
			want:   ActionWrite,
		},
		{
			name:   "DELETE server version is write",
			method: "DELETE",
			path:   "/registry/test/v0.1/servers/myserver/versions/1.0",
			want:   ActionWrite,
		},
		{
			name:   "DELETE server version with complex version is write",
			method: "DELETE",
			path:   "/registry/test/v0.1/servers/myserver/versions/1.0.0-beta",
			want:   ActionWrite,
		},

		// Skills write operations
		{
			name:   "POST skills publish is write",
			method: "POST",
			path:   "/registry/test/v0.1/x/dev.toolhive/skills",
			want:   ActionWrite,
		},
		{
			name:   "DELETE skill version is write",
			method: "DELETE",
			path:   "/registry/test/v0.1/x/dev.toolhive/skills/io.github.test/my-skill/versions/1.0.0",
			want:   ActionWrite,
		},
		{
			name:   "GET skills list is read",
			method: "GET",
			path:   "/registry/test/v0.1/x/dev.toolhive/skills",
			want:   ActionRead,
		},
		{
			name:   "GET specific skill is read",
			method: "GET",
			path:   "/registry/test/v0.1/x/dev.toolhive/skills/io.github.test/my-skill",
			want:   ActionRead,
		},

		// Admin operations (extension registry mutations)
		{
			name:   "PUT specific registry is admin",
			method: "PUT",
			path:   "/extension/v0/registries/myregistry",
			want:   ActionAdmin,
		},
		{
			name:   "DELETE specific registry is admin",
			method: "DELETE",
			path:   "/extension/v0/registries/myregistry",
			want:   ActionAdmin,
		},

		// Deeper extension paths are NOT admin (isExtensionRegistryMutation checks for no slash after name)
		{
			name:   "PUT registry server is not extension mutation - defaults to admin",
			method: "PUT",
			path:   "/extension/v0/registries/myregistry/servers/foo",
			want:   ActionAdmin,
		},
		{
			name:   "PUT registry server version is not extension mutation - defaults to admin",
			method: "PUT",
			path:   "/extension/v0/registries/myregistry/servers/foo/versions/1.0",
			want:   ActionAdmin,
		},
		{
			name:   "DELETE deeper extension path falls through to v0.1 servers check",
			method: "DELETE",
			path:   "/extension/v0/registries/myregistry/servers/foo/versions/1.0",
			want:   ActionAdmin,
		},

		// Default: unknown mutating operations require admin
		{
			name:   "POST to unknown path defaults to admin",
			method: "POST",
			path:   "/some/unknown/endpoint",
			want:   ActionAdmin,
		},
		{
			name:   "PUT to unknown path defaults to admin",
			method: "PUT",
			path:   "/some/unknown/endpoint",
			want:   ActionAdmin,
		},
		{
			name:   "DELETE to unknown path defaults to admin",
			method: "DELETE",
			path:   "/some/unknown/path",
			want:   ActionAdmin,
		},
		{
			name:   "PATCH to unknown path defaults to admin",
			method: "PATCH",
			path:   "/some/unknown/endpoint",
			want:   ActionAdmin,
		},

		// Edge cases for extension registry mutation detection
		{
			name:   "PUT extension registries trailing slash is treated as admin mutation",
			method: "PUT",
			path:   "/extension/v0/registries/",
			want:   ActionAdmin,
		},
		{
			name:   "GET extension registry is read not admin",
			method: "GET",
			path:   "/extension/v0/registries/myregistry",
			want:   ActionRead,
		},

		// Edge cases for registry write detection
		{
			name:   "POST with publish in different context is still write",
			method: "POST",
			path:   "/registry/other/v0.1/publish",
			want:   ActionWrite,
		},
		{
			name:   "DELETE server in v0.1 path is write",
			method: "DELETE",
			path:   "/registry/other/v0.1/servers/foo",
			want:   ActionWrite,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := RouteAction(tt.method, tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsExtensionRegistryMutation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{
			name:   "PUT extension registry is mutation",
			method: "PUT",
			path:   "/extension/v0/registries/myregistry",
			want:   true,
		},
		{
			name:   "DELETE extension registry is mutation",
			method: "DELETE",
			path:   "/extension/v0/registries/myregistry",
			want:   true,
		},
		{
			name:   "GET extension registry is not mutation",
			method: "GET",
			path:   "/extension/v0/registries/myregistry",
			want:   false,
		},
		{
			name:   "POST extension registry is not mutation",
			method: "POST",
			path:   "/extension/v0/registries/myregistry",
			want:   false,
		},
		{
			name:   "PUT deeper path is not mutation",
			method: "PUT",
			path:   "/extension/v0/registries/myregistry/servers/foo",
			want:   false,
		},
		{
			name:   "DELETE deeper path is not mutation",
			method: "DELETE",
			path:   "/extension/v0/registries/myregistry/servers/foo",
			want:   false,
		},
		{
			name:   "PUT wrong prefix is not mutation",
			method: "PUT",
			path:   "/other/v0/registries/myregistry",
			want:   false,
		},
		{
			name:   "PUT trailing slash with empty name is treated as mutation",
			method: "PUT",
			path:   "/extension/v0/registries/",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := isExtensionRegistryMutation(tt.method, tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsRegistryWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{
			name:   "POST publish is write",
			method: "POST",
			path:   "/registry/test/v0.1/publish",
			want:   true,
		},
		{
			name:   "DELETE server version is write",
			method: "DELETE",
			path:   "/registry/test/v0.1/servers/myserver/versions/1.0",
			want:   true,
		},
		{
			name:   "GET publish path is not write",
			method: "GET",
			path:   "/registry/test/v0.1/publish",
			want:   false,
		},
		{
			name:   "POST non-publish path is not write",
			method: "POST",
			path:   "/registry/test/v0.1/something",
			want:   false,
		},
		{
			name:   "DELETE non-servers path is not write",
			method: "DELETE",
			path:   "/registry/test/v0.1/other/thing",
			want:   false,
		},
		{
			name:   "PUT servers path is not write (wrong method)",
			method: "PUT",
			path:   "/registry/test/v0.1/servers/myserver",
			want:   false,
		},
		// Skills write operations
		{
			name:   "POST skills is write",
			method: "POST",
			path:   "/registry/test/v0.1/x/dev.toolhive/skills",
			want:   true,
		},
		{
			name:   "DELETE skill version is write",
			method: "DELETE",
			path:   "/registry/test/v0.1/x/dev.toolhive/skills/io.github.test/my-skill/versions/1.0.0",
			want:   true,
		},
		{
			name:   "GET skills is not write",
			method: "GET",
			path:   "/registry/test/v0.1/x/dev.toolhive/skills",
			want:   false,
		},
		{
			name:   "PUT skills path is not write (wrong method)",
			method: "PUT",
			path:   "/registry/test/v0.1/x/dev.toolhive/skills/io.github.test/my-skill",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := isRegistryWrite(tt.method, tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}
