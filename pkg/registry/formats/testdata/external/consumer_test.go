package consumer

import (
	"encoding/json"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

func TestFormatOnlyConsumer(t *testing.T) {
	name, err := formats.ValidateServerName("  com.example/weather  ")
	if err != nil || name != "com.example/weather" {
		t.Fatalf("name = %q, error = %v", name, err)
	}
	snapshot, err := formats.DecodeUpstream([]byte(`{
		"$schema":"https://raw.githubusercontent.com/stacklok/toolhive-core/main/registry/types/data/upstream-registry.schema.json",
		"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},
		"data":{"servers":[{"$schema":"https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
		"name":"com.example/weather","description":"Weather","version":"1.0","packages":[]}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	server := &snapshot.Data.Servers[0]
	if server.Name != name {
		t.Fatalf("decoded server name = %q, want %q", server.Name, name)
	}
	skill := &model.Skill{
		Namespace: "com.example", Name: "forecast", Version: "rev1", Description: "Forecast",
		Packages: []model.SkillPackage{{
			RegistryType: model.PackageFormatOCI, Identifier: "ghcr.io/example/forecast", Digest: "sha256:abc",
		}},
	}
	plugin := &model.Plugin{
		Namespace: "com.example", Name: "client", Version: "rev1", Description: "Client",
		Packages: []model.PluginPackage{{
			RegistryType: model.PackageFormatGit, URL: "https://github.com/example/client", Commit: "abc123", Subfolder: "plugin",
		}},
	}
	if err := formats.ValidatePublishedSkill(skill); err != nil {
		t.Fatal(err)
	}
	skillPayload := formats.SkillPayload(skill)
	if err := skillPayload.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := formats.ValidatePublishedPlugin(plugin); err != nil {
		t.Fatal(err)
	}
	pluginPayload := formats.PluginPayload(plugin)
	if err := pluginPayload.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		value any
		want  string
	}{
		{"server", formats.ServerPage([]*model.Server{server}, "host-cursor"), `{"servers":[{"server":{"$schema":"https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json","name":"com.example/weather","description":"Weather","version":"1.0"},"_meta":{}}],"metadata":{"nextCursor":"host-cursor","count":1}}`},
		{"skill", formats.SkillPage([]*model.Skill{skill}, "host-cursor"), `{"skills":[{"namespace":"com.example","name":"forecast","description":"Forecast","version":"rev1","packages":[{"registryType":"oci","identifier":"ghcr.io/example/forecast","digest":"sha256:abc"}]}],"metadata":{"count":1,"nextCursor":"host-cursor"}}`},
		{"plugin", formats.PluginPage([]*model.Plugin{plugin}, ""), `{"plugins":[{"namespace":"com.example","name":"client","description":"Client","version":"rev1","packages":[{"registryType":"git","url":"https://github.com/example/client","commit":"abc123","subfolder":"plugin"}]}],"metadata":{"count":1}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.want {
				t.Fatalf("response = %s, want %s", b, tt.want)
			}
		})
	}
}
