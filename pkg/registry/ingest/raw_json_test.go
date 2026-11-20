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

func apiRawPage(server string) string {
	return `{"servers":[{"server":` + server + `}],"metadata":{"count":1}}`
}

func TestUpstreamLegacyJSONAcceptance(t *testing.T) {
	t.Parallel()
	valid := apiRawPage(apiRawServer)
	for _, tc := range []struct {
		name  string
		data  string
		check func(*model.Server)
	}{
		{
			name: "top-level uses last value",
			data: strings.Replace(valid, `"servers":`, `"servers":[],"servers":`, 1),
			check: func(server *model.Server) {
				require.Equal(t, "com.example/weather", server.Name)
			},
		},
		{
			name: "nested uses last value",
			data: apiRawPage(strings.Replace(apiRawServer, `"description":"Weather"`, `"description":"Ignored","description":"Weather"`, 1)),
			check: func(server *model.Server) {
				require.Equal(t, "Weather", server.Description)
			},
		},
		{
			name: "escaped equivalent uses last value",
			data: apiRawPage(strings.Replace(apiRawServer, `"name":"com.example/weather"`, `"na\u006de":"ignored.example/weather","name":"com.example/weather"`, 1)),
			check: func(server *model.Server) {
				require.Equal(t, "com.example/weather", server.Name)
			},
		},
		{
			name: "unicode normalization form remains accepted",
			data: apiRawPage(strings.Replace(apiRawServer, "Weather", "Cafe\u0301", 1)),
			check: func(server *model.Server) {
				require.Equal(t, "Cafe\u0301", server.Description)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := mocks.NewMockHTTPClient(gomock.NewController(t))
			client.EXPECT().Get(gomock.Any(), "https://example.org/v0.1/servers?limit=100").Return([]byte(tc.data), nil)
			upstream, err := ingest.NewUpstream(client)
			require.NoError(t, err)
			snapshot, err := upstream.Fetch(t.Context(), "https://example.org")
			require.NoError(t, err)
			require.Len(t, snapshot.Data.Servers, 1)
			tc.check(&snapshot.Data.Servers[0])
		})
	}
}

func TestUpstreamRejectsInvalidPageWithoutPartialSnapshot(t *testing.T) {
	t.Parallel()
	valid := apiRawPage(apiRawServer)
	for _, tc := range []struct {
		name string
		data string
	}{
		{"malformed", valid[:len(valid)-1]},
		{"trailing JSON", valid + `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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

			client.EXPECT().Get(gomock.Any(), "https://example.org/v0.1/servers?limit=1").Return([]byte(apiRawPage(apiRawServer)), nil)
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
	client.EXPECT().Get(gomock.Any(), "https://example.org/v0.1/servers?limit=1").Return([]byte(apiRawPage(apiRawServer)), nil)
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
