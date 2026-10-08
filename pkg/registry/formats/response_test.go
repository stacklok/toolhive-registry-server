package formats_test

import (
	"encoding/json"
	"testing"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

func TestExtensionPayloadMetadataAndSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		response any
		want     string
	}{
		{"skill", formats.SkillPage([]*model.Skill{
			{ID: "internal", Namespace: "io.example", Name: "z", Description: "first", Version: "rev-2", IsLatest: true,
				Repository: &model.SkillRepository{URL: "https://example.org/repo", Type: "git"},
				Packages:   []model.SkillPackage{{RegistryType: "git", URL: "https://example.org/repo", Ref: "main"}},
				Icons:      []model.SkillIcon{{Src: "https://example.org/icon.png", Label: "icon"}},
				Metadata:   map[string]any{"category": "dev"}, Meta: map[string]any{"vendor": "value"}},
			{Namespace: "io.example", Name: "z", Version: "rev-1"},
		}, "opaque"), `{"skills":[{"namespace":"io.example","name":"z","description":"first","version":"rev-2","repository":{"url":"https://example.org/repo","type":"git"},"icons":[{"src":"https://example.org/icon.png","label":"icon"}],"packages":[{"registryType":"git","url":"https://example.org/repo","ref":"main"}],"metadata":{"category":"dev"},"_meta":{"vendor":"value"}},{"namespace":"io.example","name":"z","description":"","version":"rev-1"}],"metadata":{"count":2,"nextCursor":"opaque"}}`},
		{"plugin", formats.PluginPage([]*model.Plugin{
			{ID: "internal", Namespace: "io.example", Name: "z", Description: "first", Version: "rev-2", IsLatest: true,
				Repository: &model.PluginRepository{URL: "https://example.org/repo", Type: "git"},
				Packages:   []model.PluginPackage{{RegistryType: "git", URL: "https://example.org/repo", Ref: "main"}},
				Icons:      []model.PluginIcon{{Src: "https://example.org/icon.png", Label: "icon"}},
				Metadata:   map[string]any{"category": "dev"}, Meta: map[string]any{"vendor": "value"}},
			{Namespace: "io.example", Name: "z", Version: "rev-1"},
		}, "opaque"), `{"plugins":[{"namespace":"io.example","name":"z","description":"first","version":"rev-2","repository":{"url":"https://example.org/repo","type":"git"},"icons":[{"src":"https://example.org/icon.png","label":"icon"}],"packages":[{"registryType":"git","url":"https://example.org/repo","ref":"main"}],"metadata":{"category":"dev"},"_meta":{"vendor":"value"}},{"namespace":"io.example","name":"z","description":"","version":"rev-1"}],"metadata":{"count":2,"nextCursor":"opaque"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(tc.response)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("response = %s\nwant = %s", got, tc.want)
			}
		})
	}
}
