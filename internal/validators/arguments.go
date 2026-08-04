package validators

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// SerializeArguments serializes a slice of model.Argument to JSON bytes for storage
// in a JSONB column. The full object is preserved — value, type, default, valueHint,
// variables and the rest — rather than just the flag name.
//
// Empty and nil input both serialize to an empty JSON array so the column never
// holds a JSON `null`.
func SerializeArguments(arguments []model.Argument) ([]byte, error) {
	if len(arguments) == 0 {
		return []byte("[]"), nil
	}

	bytes, err := json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize arguments: %w", err)
	}

	return bytes, nil
}

// DeserializeArguments reconstructs a slice of model.Argument from JSONB bytes.
//
// Malformed or absent data degrades to an empty slice rather than an error: a
// single malformed row must not fail a whole listing. This mirrors how
// environment variables and transport headers are read back.
//
// A flat array of strings is also accepted. Binaries older than migration 000023
// stored arguments as bare flag names, and one writing to the migrated JSONB column
// lands a value like ["--url","--timeout",""]. Recovering the names is strictly
// better than discarding the row, though the upgrade is documented as
// stop-the-world precisely so this path should not be reached.
func DeserializeArguments(data []byte) []model.Argument {
	if len(data) == 0 {
		return []model.Argument{}
	}

	var result []model.Argument
	if err := json.Unmarshal(data, &result); err == nil {
		return result
	}

	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return []model.Argument{}
	}

	result = make([]model.Argument, 0, len(names))
	for _, name := range names {
		// Positional arguments were stored as empty strings and carry no
		// recoverable information.
		if name == "" {
			continue
		}
		result = append(result, model.Argument{Name: name})
	}

	return result
}
