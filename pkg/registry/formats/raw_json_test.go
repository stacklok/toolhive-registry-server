package formats_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/formats"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
)

const rawServer = `{"name":"com.example/weather","description":"Weather","version":"1.0","packages":[]}`

func rawRegistry(server string) string {
	return `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[` + server + `]}}`
}

func TestDecodeUpstreamLegacyDuplicateNames(t *testing.T) {
	t.Parallel()
	valid := rawRegistry(rawServer)
	for _, tc := range []struct {
		name  string
		data  string
		check func(*model.Snapshot)
	}{
		{
			name: "top-level uses last value",
			data: strings.Replace(valid, `"version":"1.0.0"`, `"version":"legacy","version":"1.0.0"`, 1),
			check: func(snapshot *model.Snapshot) {
				require.Equal(t, "1.0.0", snapshot.Version)
			},
		},
		{
			name: "nested uses last value",
			data: rawRegistry(strings.Replace(rawServer, `"description":"Weather"`, `"description":"Ignored","description":"Weather"`, 1)),
			check: func(snapshot *model.Snapshot) {
				require.Equal(t, "Weather", snapshot.Data.Servers[0].Description)
			},
		},
		{
			name: "escaped equivalent uses last value",
			data: rawRegistry(strings.Replace(rawServer, `"name":"com.example/weather"`, `"na\u006de":"ignored.example/weather","name":"com.example/weather"`, 1)),
			check: func(snapshot *model.Snapshot) {
				require.Equal(t, "com.example/weather", snapshot.Data.Servers[0].Name)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snapshot, err := formats.DecodeUpstream([]byte(tc.data))
			require.NoError(t, err)
			tc.check(snapshot)
		})
	}
}

func TestDecodeUpstreamLegacyUnicodeForms(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		input       string
		description string
	}{
		{"composed", "Café", "Café"},
		{"decomposed", "Cafe\u0301", "Cafe\u0301"},
		{"invalid UTF-8", "Weather\xff", "Weather�"},
		{"lone surrogate", `Weather\uD800`, "Weather�"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := strings.Replace(rawServer, "Weather", tc.input, 1)
			snapshot, err := formats.DecodeUpstream([]byte(rawRegistry(server)))
			require.NoError(t, err)
			require.Equal(t, tc.description, snapshot.Data.Servers[0].Description)
		})
	}
}

func TestDecodeUpstreamRejectsMalformedOrTrailingJSON(t *testing.T) {
	t.Parallel()
	valid := rawRegistry(rawServer)
	for _, tc := range []struct {
		name string
		data string
	}{
		{"malformed", valid[:len(valid)-1]},
		{"trailing JSON", valid + `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := []byte(tc.data)
			original := bytes.Clone(data)
			snapshot, err := formats.DecodeUpstream(data)
			require.Error(t, err)
			require.Nil(t, snapshot)
			require.Equal(t, original, data)
		})
	}
}

func TestDecodeUpstreamMetadataPrecision(t *testing.T) {
	t.Parallel()
	server := strings.Replace(rawServer, `"packages":[]`, `"_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"com.example/custom":{"integer":9007199254740993,"decimal":0.123456789012345678901234567890,"nested":[{"number":1.2300}]}}}`, 1)
	data := []byte(rawRegistry(server))
	original := bytes.Clone(data)
	snapshot, err := formats.DecodeUpstream(data)
	require.NoError(t, err)
	require.Equal(t, original, data)
	metadata := snapshot.Data.Servers[0].Meta.PublisherProvided["com.example/custom"].(map[string]any)
	require.Equal(t, json.Number("9007199254740993"), metadata["integer"])
	require.Equal(t, json.Number("0.123456789012345678901234567890"), metadata["decimal"])
	require.Equal(t, json.Number("1.2300"), metadata["nested"].([]any)[0].(map[string]any)["number"])
	wire, err := json.Marshal(formats.ServerPage([]*model.Server{&snapshot.Data.Servers[0]}, ""))
	require.NoError(t, err)
	require.Contains(t, string(wire), `"integer":9007199254740993`)
	require.Contains(t, string(wire), `"decimal":0.123456789012345678901234567890`)
	require.Contains(t, string(wire), `"number":1.2300`)
}
