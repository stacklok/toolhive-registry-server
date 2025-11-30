// Package common provides shared HTTP utility functions for API handlers.
package common

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidateURLParam(t *testing.T) {
	t.Parallel()

	// Test with valid URLs through router
	routerTests := []struct {
		name       string
		paramName  string
		paramValue string
		wantValue  string
		wantErr    bool
		wantErrMsg string
	}{
		// Valid cases
		{
			name:       "valid plain string",
			paramName:  "serverName",
			paramValue: "test-server",
			wantValue:  "test-server",
			wantErr:    false,
		},
		{
			name:       "valid with dashes",
			paramName:  "serverName",
			paramValue: "test-server-123",
			wantValue:  "test-server-123",
			wantErr:    false,
		},
		{
			name:       "valid with underscores",
			paramName:  "serverName",
			paramValue: "test_server_123",
			wantValue:  "test_server_123",
			wantErr:    false,
		},
		{
			name:       "valid with dots",
			paramName:  "version",
			paramValue: "1.2.3",
			wantValue:  "1.2.3",
			wantErr:    false,
		},
		{
			name:       "valid with mixed special chars",
			paramName:  "serverName",
			paramValue: "test.server-v1_alpha",
			wantValue:  "test.server-v1_alpha",
			wantErr:    false,
		},

		// URL-encoded cases that should decode properly
		{
			name:       "url-encoded slash",
			paramName:  "serverName",
			paramValue: "test%2Fserver",
			wantValue:  "test/server",
			wantErr:    false,
		},
		{
			name:       "url-encoded at symbol",
			paramName:  "version",
			paramValue: "test%40v1",
			wantValue:  "test@v1",
			wantErr:    false,
		},
		{
			name:       "url-encoded colon",
			paramName:  "serverName",
			paramValue: "test%3Aserver",
			wantValue:  "test:server",
			wantErr:    false,
		},
		{
			name:       "url-encoded equals",
			paramName:  "serverName",
			paramValue: "test%3Dserver",
			wantValue:  "test=server",
			wantErr:    false,
		},
		{
			name:       "url-encoded ampersand",
			paramName:  "serverName",
			paramValue: "test%26server",
			wantValue:  "test&server",
			wantErr:    false,
		},
		{
			name:       "url-encoded plus",
			paramName:  "serverName",
			paramValue: "test%2Bserver",
			wantValue:  "test+server",
			wantErr:    false,
		},
		// Note: Chi router already partially decodes URLs
		// %2525 becomes %25 which we then decode to %
		{
			name:       "double-encoded percent",
			paramName:  "serverName",
			paramValue: "test%2525server",
			wantValue:  "test%server", // Chi decodes %25 to %, then we decode %25 to %
			wantErr:    false,
		},
		{
			name:       "multiple url-encoded chars",
			paramName:  "serverName",
			paramValue: "test%2Fserver%40v1%2B2",
			wantValue:  "test/server@v1+2",
			wantErr:    false,
		},

		// Empty and whitespace cases
		{
			name:       "empty string",
			paramName:  "serverName",
			paramValue: "",
			wantErr:    true,
			wantErrMsg: "serverName cannot be empty",
		},
		{
			name:       "url-encoded space only",
			paramName:  "serverName",
			paramValue: "%20",
			wantErr:    true,
			wantErrMsg: "serverName cannot be empty",
		},
		{
			name:       "multiple url-encoded spaces",
			paramName:  "serverName",
			paramValue: "%20%20%20",
			wantErr:    true,
			wantErrMsg: "serverName cannot be empty",
		},
		{
			name:       "url-encoded tab only",
			paramName:  "serverName",
			paramValue: "%09",
			wantErr:    true,
			wantErrMsg: "serverName cannot be empty",
		},
		{
			name:       "url-encoded newline only",
			paramName:  "serverName",
			paramValue: "%0A",
			wantErr:    true,
			wantErrMsg: "serverName cannot be empty",
		},
		{
			name:       "url-encoded carriage return only",
			paramName:  "serverName",
			paramValue: "%0D",
			wantErr:    true,
			wantErrMsg: "serverName cannot be empty",
		},

		// Whitespace in middle cases
		{
			name:       "space in middle",
			paramName:  "serverName",
			paramValue: "test%20server",
			wantErr:    true,
			wantErrMsg: "serverName cannot contain whitespace",
		},
		{
			name:       "tab in middle",
			paramName:  "serverName",
			paramValue: "test%09server",
			wantErr:    true,
			wantErrMsg: "serverName cannot contain whitespace",
		},
		{
			name:       "newline in middle",
			paramName:  "serverName",
			paramValue: "test%0Aserver",
			wantErr:    true,
			wantErrMsg: "serverName cannot contain whitespace",
		},
		{
			name:       "carriage return in middle",
			paramName:  "serverName",
			paramValue: "test%0Dserver",
			wantErr:    true,
			wantErrMsg: "serverName cannot contain whitespace",
		},
		{
			name:       "space at start",
			paramName:  "serverName",
			paramValue: "%20test",
			wantErr:    true,
			wantErrMsg: "serverName cannot contain whitespace",
		},
		{
			name:       "space at end",
			paramName:  "serverName",
			paramValue: "test%20",
			wantErr:    true,
			wantErrMsg: "serverName cannot contain whitespace",
		},
		{
			name:       "multiple spaces",
			paramName:  "serverName",
			paramValue: "test%20%20server",
			wantErr:    true,
			wantErrMsg: "serverName cannot contain whitespace",
		},
		{
			name:       "mixed whitespace",
			paramName:  "serverName",
			paramValue: "test%20%09%0A%0Dserver",
			wantErr:    true,
			wantErrMsg: "serverName cannot contain whitespace",
		},
	}

	for _, tt := range routerTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a test router with chi
			router := chi.NewRouter()
			router.Get("/{"+tt.paramName+"}", func(_ http.ResponseWriter, r *http.Request) {
				value, err := GetAndValidateURLParam(r, tt.paramName)

				if tt.wantErr {
					require.Error(t, err)
					assert.Equal(t, tt.wantErrMsg, err.Error())
				} else {
					require.NoError(t, err)
					assert.Equal(t, tt.wantValue, value)
				}
			})

			// Create test request
			req, err := http.NewRequest("GET", "/"+tt.paramValue, nil)
			require.NoError(t, err)

			// Execute request
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
		})
	}

	// Test invalid URL encoding directly (chi router won't parse these)
	directTests := []struct {
		name       string
		paramName  string
		paramValue string
		wantErrMsg string
	}{
		{
			name:       "invalid url encoding - incomplete",
			paramName:  "serverName",
			paramValue: "test%2",
			wantErrMsg: "invalid URL encoding in serverName",
		},
		{
			name:       "invalid url encoding - invalid hex",
			paramName:  "serverName",
			paramValue: "test%ZZ",
			wantErrMsg: "invalid URL encoding in serverName",
		},
		{
			name:       "invalid url encoding - incomplete percent",
			paramName:  "serverName",
			paramValue: "test%",
			wantErrMsg: "invalid URL encoding in serverName",
		},
	}

	for _, tt := range directTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a mock request with chi context
			req := httptest.NewRequest("GET", "/test", nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add(tt.paramName, tt.paramValue)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			// Call the function directly
			_, err := GetAndValidateURLParam(req, tt.paramName)
			require.Error(t, err)
			assert.Equal(t, tt.wantErrMsg, err.Error())
		})
	}
}
