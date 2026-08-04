package validators_test

import (
	"testing"

	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/validators"
)

// TestArgumentsRoundTrip is the regression test for #851: every field on
// model.Argument must survive a serialize/deserialize cycle, not just Name.
func TestArgumentsRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []model.Argument
	}{
		{
			name: "named argument retains value",
			arguments: []model.Argument{
				{
					Type: model.ArgumentTypeNamed,
					Name: "--url",
					InputWithVariables: model.InputWithVariables{
						Input: model.Input{Value: "https://example.com/mcp"},
					},
				},
			},
		},
		{
			name: "positional argument survives without a name",
			arguments: []model.Argument{
				{
					Type:      model.ArgumentTypePositional,
					ValueHint: "config_file",
					InputWithVariables: model.InputWithVariables{
						Input: model.Input{
							Value:  "/etc/args-server/config.yaml",
							Format: model.FormatFilePath,
						},
					},
				},
			},
		},
		{
			name: "all scalar metadata fields retained",
			arguments: []model.Argument{
				{
					Type:       model.ArgumentTypeNamed,
					Name:       "--timeout",
					ValueHint:  "seconds",
					IsRepeated: true,
					InputWithVariables: model.InputWithVariables{
						Input: model.Input{
							Description: "Request timeout",
							IsRequired:  true,
							Format:      model.FormatNumber,
							Value:       "180",
							IsSecret:    true,
							Default:     "60",
							Placeholder: "e.g. 60",
							Choices:     []string{"30", "60", "180"},
						},
					},
				},
			},
		},
		{
			name: "variables map retained",
			arguments: []model.Argument{
				{
					Type: model.ArgumentTypeNamed,
					Name: "--mount",
					InputWithVariables: model.InputWithVariables{
						Input: model.Input{Value: "{src}:{dst}"},
						Variables: map[string]model.Input{
							"src": {Description: "host path", IsRequired: true},
							"dst": {Description: "container path", Default: "/data"},
						},
					},
				},
			},
		},
		{
			name: "multiple arguments retain order and values",
			arguments: []model.Argument{
				{
					Type: model.ArgumentTypeNamed,
					Name: "--url",
					InputWithVariables: model.InputWithVariables{
						Input: model.Input{Value: "https://example.com/mcp"},
					},
				},
				{
					Type: model.ArgumentTypeNamed,
					Name: "--timeout",
					InputWithVariables: model.InputWithVariables{
						Input: model.Input{Value: "180", Default: "60"},
					},
				},
				{
					Type:      model.ArgumentTypePositional,
					ValueHint: "config_file",
					InputWithVariables: model.InputWithVariables{
						Input: model.Input{Value: "/etc/args-server/config.yaml"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := validators.SerializeArguments(tt.arguments)
			require.NoError(t, err)

			got := validators.DeserializeArguments(data)
			assert.Equal(t, tt.arguments, got)
		})
	}
}

func TestSerializeArgumentsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []model.Argument
	}{
		{name: "nil arguments", arguments: nil},
		{name: "empty arguments", arguments: []model.Argument{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := validators.SerializeArguments(tt.arguments)
			require.NoError(t, err)
			assert.JSONEq(t, "[]", string(data),
				"empty input must serialize to an empty JSON array, not null")

			assert.Empty(t, validators.DeserializeArguments(data))
		})
	}
}

func TestDeserializeArgumentsMalformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
	}{
		{name: "nil bytes", data: nil},
		{name: "empty bytes", data: []byte{}},
		{name: "not json", data: []byte("not json")},
		{name: "json object instead of array", data: []byte(`{"name":"--url"}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Malformed rows must degrade to an empty slice rather than panic,
			// matching how toKeyValueInputs handles unparseable env_vars.
			assert.Empty(t, validators.DeserializeArguments(tt.data))
		})
	}
}
