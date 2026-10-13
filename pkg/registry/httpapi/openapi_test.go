package httpapi

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultOpenAPIUsesActualClaimsFreeAdminDTOs(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../docs/thv-registry-api/swagger.json")
	require.NoError(t, err)
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
		Paths map[string]map[string]struct {
			Responses map[string]struct {
				Content map[string]struct {
					Schema struct {
						Ref   string            `json:"$ref"`
						OneOf []json.RawMessage `json:"oneOf"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(data, &spec))
	for name, value := range map[string]any{
		"sourceRequest": sourceRequest{}, "sourceInfo": sourceInfo{}, "viewInfo": viewInfo{},
		"kubernetesConfig": kubernetesConfig{}, "sourceSyncStatus": sourceSyncStatus{},
		"versionInfo": versionInfo{}, "sourceEntryInfo": sourceEntryInfo{},
		"publishRequest": publishRequest{}, "ViewRequest": ViewRequest{},
		"SourcesResponse": SourcesResponse{}, "ViewsResponse": ViewsResponse{},
		"SourceEntriesResponse": SourceEntriesResponse{}, "ViewEntriesResponse": ViewEntriesResponse{},
		"CommittedErrorResponse": CommittedErrorResponse{}, "ErrorResponse": ErrorResponse{},
	} {
		schema, ok := spec.Components.Schemas["httpapi."+name]
		require.True(t, ok, name)
		fields := reflect.TypeOf(value)
		want := make([]string, 0, fields.NumField())
		for i := range fields.NumField() {
			want = append(want, strings.Split(fields.Field(i).Tag.Get("json"), ",")[0])
		}
		got := make([]string, 0, len(schema.Properties))
		for key := range schema.Properties {
			got = append(got, key)
		}
		require.ElementsMatch(t, want, got, name)
		require.NotContains(t, schema.Properties, "claims", name)
	}
	for path, operations := range spec.Paths {
		for method, operation := range operations {
			for status, response := range operation.Responses {
				schema := response.Content["application/json"].Schema
				if path == "/v1/entries" && method == "post" && status == "201" {
					require.Len(t, schema.OneOf, 3, "raw server/skill/plugin publication alternatives")
				} else {
					require.Empty(t, schema.OneOf, "unintended generator oneOf: %s %s %s", method, path, status)
				}
			}
		}
	}
}
