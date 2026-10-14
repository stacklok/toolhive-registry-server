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

func TestValidateRawJSONStrict(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"literal escaped backslash before surrogate", []byte(`{"description":"\\uD800"}`), true},
		{"surrogate pair", []byte(`{"description":"\uD800\uDC00"}`), true},
		{"lone high surrogate", []byte(`{"description":"\uD800"}`), false},
		{"lone low surrogate", []byte(`{"description":"\uDC00"}`), false},
		{"invalid UTF-8", []byte("{\"description\":\"bad\xff\"}"), false},
		{"duplicate decoded key", []byte(`{"na\u006de":1,"name":2}`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := formats.ValidateRawJSON(tc.data)
			require.Equal(t, tc.valid, err == nil, "error = %v", err)
		})
	}
}

func TestDecodeUpstreamOriginalJSON(t *testing.T) {
	t.Parallel()
	valid := rawRegistry(rawServer)
	for _, tc := range []struct {
		name string
		data string
	}{
		{"top-level duplicate", strings.Replace(valid, `"version":"1.0.0"`, `"version":"old","version":"1.0.0"`, 1)},
		{"nested duplicate", rawRegistry(strings.Replace(rawServer, `"name":`, `"name":"ignored","name":`, 1))},
		{"escaped duplicate decoded key", rawRegistry(strings.Replace(rawServer, `"name":`, `"na\u006de":"ignored","name":`, 1))},
		{"metadata duplicate", rawRegistry(strings.Replace(rawServer, `"packages":[]`, `"_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"com.example/custom":{"nested":[{"n":1,"n":2}]}}}`, 1))},
		{"malformed", valid[:len(valid)-1]},
		{"trailing JSON", valid + `{}`},
		{"invalid UTF-8", strings.Replace(valid, "Weather", "Wea\xffther", 1)},
		{"lone high surrogate escape", strings.Replace(valid, "Weather", `\uD800`, 1)},
		{"lone low surrogate escape", strings.Replace(valid, "Weather", `\uDC00`, 1)},
		{"raw null array hidden by typed projection", rawRegistry(strings.Replace(rawServer, `"packages":[]`, `"packages":null`, 1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := []byte(tc.data)
			original := bytes.Clone(data)
			got, err := formats.DecodeUpstream(data)
			require.Error(t, err)
			require.Nil(t, got)
			require.Equal(t, original, data)
		})
	}
}

func TestDecodeUpstreamMetadataPrecision(t *testing.T) {
	t.Parallel()
	server := strings.Replace(rawServer, `"packages":[]`, `"unknown":"discarded","_meta":{"io.modelcontextprotocol.registry/publisher-provided":{"com.example/custom":{"integer":9007199254740993,"decimal":0.123456789012345678901234567890,"nested":[{"number":1.2300}]}}}`, 1)
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
	require.NotContains(t, string(wire), `"unknown"`)
}

func TestDecodeUpstreamCoreExtensionLimitations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		metadata string
	}{
		{"official scalar namespace rejected by core policy", `{"com.example/custom":42}`},
		{"core float64 parsing rejects overflow", `{"com.example/custom":{"number":1e400}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := strings.Replace(rawServer, `"packages":[]`, `"_meta":{"io.modelcontextprotocol.registry/publisher-provided":`+tc.metadata+`}`, 1)
			snapshot, err := formats.DecodeUpstream([]byte(rawRegistry(server)))
			require.Error(t, err)
			require.Nil(t, snapshot)
		})
	}
}
