package ingest_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest/mocks"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

const apiRawServer = `{"$schema":"https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json","name":"com.example/weather","description":"Weather","version":"1.0","packages":[]}`
const apiOpenAPI = "info:\n  version: 1.0.0\n  description: https://github.com/modelcontextprotocol/registry\n"

func apiRawPage(server string) string {
	return `{"servers":[{"server":` + server + `}],"metadata":{"count":1}}`
}

func TestUpstreamOriginalJSONFailure(t *testing.T) {
	t.Parallel()
	valid := apiRawPage(apiRawServer)
	for _, tc := range []struct {
		name string
		data string
	}{
		{"top-level duplicate", strings.Replace(valid, `"servers":`, `"servers":[],"servers":`, 1)},
		{"nested duplicate", apiRawPage(strings.Replace(apiRawServer, `"name":`, `"name":"ignored","name":`, 1))},
		{"escaped duplicate decoded key", apiRawPage(strings.Replace(apiRawServer, `"name":`, `"na\u006de":"ignored","name":`, 1))},
		{"metadata duplicate", apiRawPage(strings.Replace(apiRawServer, `"packages":[]`, `"_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"com.example/custom":{"nested":[{"n":1,"n":2}]}}}`, 1))},
		{"malformed", valid[:len(valid)-1]},
		{"trailing JSON", valid + `{}`},
		{"invalid UTF-8", strings.Replace(valid, "Weather", "Wea\xffther", 1)},
		{"lone high surrogate escape", strings.Replace(valid, "Weather", `\uD800`, 1)},
		{"lone low surrogate escape", strings.Replace(valid, "Weather", `\uDC00`, 1)},
		{"invalid typed shape", apiRawPage(strings.Replace(apiRawServer, `"packages":[]`, `"packages":{}`, 1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Fail after a good first page: no partial snapshot or change hash may escape.
			client := mocks.NewMockHTTPClient(gomock.NewController(t))
			first := strings.Replace(valid, `"count":1`, `"count":1,"nextCursor":"next"`, 1)
			client.EXPECT().Get(gomock.Any(), "https://example.org/v0.1/servers?limit=100").Return([]byte(first), nil).Times(2)
			client.EXPECT().Get(gomock.Any(), "https://example.org/v0.1/servers?limit=100&cursor=next").Return([]byte(tc.data), nil).Times(2)
			upstream, err := ingest.NewUpstream(client)
			require.NoError(t, err)
			snapshot, err := upstream.Fetch(t.Context(), "https://example.org")
			require.Error(t, err)
			require.Equal(t, model.Snapshot{}, snapshot)
			require.Equal(t, "failed to parse response page", err.Error())

			client.EXPECT().Get(gomock.Any(), "https://example.org/openapi.yaml").Return([]byte(apiOpenAPI), nil)
			result, err := ingest.New(ingest.Options{HTTP: client}).Fetch(t.Context(), persistence.SourceDefinition{
				Name: "api", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h",
			})
			require.ErrorIs(t, err, ingest.ErrFetch)
			require.Equal(t, ingest.Result{}, result)
		})
	}
}

func TestAPIMetadataPrecision(t *testing.T) {
	t.Parallel()
	server := strings.Replace(apiRawServer, `"packages":[]`, `"_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"com.example/custom":{"integer":9007199254740993,"decimal":0.123456789012345678901234567890,"nested":[1.2300]}}}`, 1)
	client := mocks.NewMockHTTPClient(gomock.NewController(t))
	client.EXPECT().Get(gomock.Any(), "https://example.org/openapi.yaml").Return([]byte(apiOpenAPI), nil)
	client.EXPECT().Get(gomock.Any(), "https://example.org/v0.1/servers?limit=100").Return([]byte(apiRawPage(server)), nil).Times(2)
	upstream, err := ingest.NewUpstream(client)
	require.NoError(t, err)
	snapshot, err := upstream.Fetch(t.Context(), "https://example.org")
	require.NoError(t, err)
	result, err := ingest.New(ingest.Options{HTTP: client}).Fetch(t.Context(), persistence.SourceDefinition{
		Name: "api", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Hash)
	for _, server := range []*model.Server{&snapshot.Data.Servers[0], &result.Snapshot.Data.Servers[0]} {
		metadata := server.Meta.PublisherProvided["com.example/custom"].(map[string]any)
		require.Equal(t, json.Number("9007199254740993"), metadata["integer"])
		require.Equal(t, json.Number("0.123456789012345678901234567890"), metadata["decimal"])
		require.Equal(t, json.Number("1.2300"), metadata["nested"].([]any)[0])
		wire, err := json.Marshal(formats.ServerPage([]*model.Server{server}, ""))
		require.NoError(t, err)
		require.Contains(t, string(wire), `"integer":9007199254740993`)
		require.Contains(t, string(wire), `"decimal":0.123456789012345678901234567890`)
		require.Contains(t, string(wire), `"nested":[1.2300]`)
	}
}

func TestAPIRawSchemaValidationRemainsBlocked(t *testing.T) {
	t.Parallel()
	// Core cannot validate a raw single server independently of its extension
	// policy. Typed normalization removes this schema-invalid null array before
	// Fetcher validates the synthesized snapshot; strict JSON is not a schema check.
	server := strings.Replace(apiRawServer, `"packages":[]`, `"packages":null`, 1)
	client := mocks.NewMockHTTPClient(gomock.NewController(t))
	client.EXPECT().Get(gomock.Any(), "https://example.org/openapi.yaml").Return([]byte(apiOpenAPI), nil)
	client.EXPECT().Get(gomock.Any(), "https://example.org/v0.1/servers?limit=100").Return([]byte(apiRawPage(server)), nil)
	result, err := ingest.New(ingest.Options{HTTP: client}).Fetch(t.Context(), persistence.SourceDefinition{
		Name: "api", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h",
	})
	require.NoError(t, err) // Current dependency limitation, not desired schema acceptance.
	require.Nil(t, result.Snapshot.Data.Servers[0].Packages)
}
