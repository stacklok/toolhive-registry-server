package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConcurrentDefaultOpenAPIRendering(t *testing.T) {
	t.Parallel()
	results := make(chan *httptest.ResponseRecorder, 16)
	for range 16 {
		go func() {
			response := httptest.NewRecorder()
			openAPIHandler(response, httptest.NewRequest("GET", "/openapi.json", nil))
			results <- response
		}()
	}
	var previous string
	for range 16 {
		response := <-results
		require.Equal(t, 200, response.Code)
		require.Equal(t, "application/json", response.Header().Get("Content-Type"))
		if previous != "" {
			require.Equal(t, previous, response.Body.String())
		}
		previous = response.Body.String()
	}
}

func TestDefaultOpenAPIMatchesCatalogManifest(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../docs/thv-registry-api/swagger.json")
	require.NoError(t, err)
	registered, err := openAPISpec()
	require.NoError(t, err)
	var runtime, artifact map[string]any
	require.NoError(t, json.Unmarshal([]byte(registered), &runtime))
	require.NoError(t, json.Unmarshal(data, &artifact))
	require.Equal(t, artifact["paths"], runtime["paths"])
	require.Equal(t, artifact["components"], runtime["components"])
	var spec struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			Responses map[string]json.RawMessage   `json:"responses"`
			Security  []map[string]json.RawMessage `json:"security"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(data, &spec))
	require.Equal(t, "3.1.0", spec.OpenAPI)
	want := map[string]bool{"GET /.well-known/oauth-protected-resource": true}
	for _, route := range admissionManifest(true) {
		key := route.method + " " + route.template
		want[key] = true
		operation, ok := spec.Paths[route.template][strings.ToLower(route.method)]
		require.True(t, ok, key)
		require.NotEmpty(t, operation.Security, key)
		if route.retired {
			require.Contains(t, operation.Responses, "410", key)
			require.NotContains(t, operation.Responses, "200", key)
		}
	}
	for path, operations := range spec.Paths {
		for method := range operations {
			require.True(t, want[strings.ToUpper(method)+" "+path], "unselected/legacy route in default OpenAPI: %s %s", method, path)
		}
	}
	require.Empty(t, spec.Paths["/.well-known/oauth-protected-resource"]["get"].Security)
	require.NotContains(t, string(data), "internal_config")
	require.NotContains(t, string(data), "superAdmin")
	require.NotContains(t, string(data), "manageEntries")
}
