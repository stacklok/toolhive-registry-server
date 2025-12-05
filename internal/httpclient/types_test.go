package httpclient_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
)

func TestHTTPError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		statusCode    int
		url           string
		message       string
		expectedError string
		errorContains []string
	}{
		{
			name:          "create HTTPError with all fields",
			statusCode:    404,
			url:           "http://example.com",
			message:       "Not Found",
			expectedError: "HTTP 404 for URL http://example.com: Not Found",
			errorContains: []string{"HTTP 404", "http://example.com", "Not Found"},
		},
		{
			name:          "format error message correctly for 500",
			statusCode:    500,
			url:           "http://api.example.com/v1/data",
			message:       "Internal Server Error",
			expectedError: "HTTP 500 for URL http://api.example.com/v1/data: Internal Server Error",
		},
		{
			name:          "handle empty message",
			statusCode:    404,
			url:           "http://example.com",
			message:       "",
			expectedError: "HTTP 404 for URL http://example.com: ",
		},
		{
			name:          "handle long URLs",
			statusCode:    404,
			url:           "http://example.com/very/long/path/with/many/segments/that/goes/on/and/on",
			message:       "Not Found",
			errorContains: []string{"http://example.com/very/long/path/with/many/segments/that/goes/on/and/on"},
		},
		{
			name:          "handle 200 OK status code",
			statusCode:    200,
			url:           "http://test.com",
			message:       "OK",
			errorContains: []string{"OK"},
		},
		{
			name:          "handle 201 Created status code",
			statusCode:    201,
			url:           "http://test.com",
			message:       "Created",
			errorContains: []string{"Created"},
		},
		{
			name:          "handle 400 Bad Request status code",
			statusCode:    400,
			url:           "http://test.com",
			message:       "Bad Request",
			errorContains: []string{"Bad Request"},
		},
		{
			name:          "handle 401 Unauthorized status code",
			statusCode:    401,
			url:           "http://test.com",
			message:       "Unauthorized",
			errorContains: []string{"Unauthorized"},
		},
		{
			name:          "handle 403 Forbidden status code",
			statusCode:    403,
			url:           "http://test.com",
			message:       "Forbidden",
			errorContains: []string{"Forbidden"},
		},
		{
			name:          "handle 502 Bad Gateway status code",
			statusCode:    502,
			url:           "http://test.com",
			message:       "Bad Gateway",
			errorContains: []string{"Bad Gateway"},
		},
		{
			name:          "handle 503 Service Unavailable status code",
			statusCode:    503,
			url:           "http://test.com",
			message:       "Service Unavailable",
			errorContains: []string{"Service Unavailable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := httpclient.NewHTTPError(tt.statusCode, tt.url, tt.message)

			require.Error(t, err)

			if tt.expectedError != "" {
				assert.Equal(t, tt.expectedError, err.Error())
			}

			for _, contains := range tt.errorContains {
				assert.Contains(t, err.Error(), contains)
			}
		})
	}
}

func TestHTTPError_ErrorInterface(t *testing.T) {
	t.Parallel()

	t.Run("HTTPError implements error interface", func(t *testing.T) {
		t.Parallel()

		err := httpclient.NewHTTPError(404, "http://example.com", "Not Found")

		// Verify it implements the error interface
		var errInterface = err
		require.NotNil(t, errInterface)
		assert.NotEmpty(t, errInterface.Error())
	})

	t.Run("HTTPError Error() returns consistent result", func(t *testing.T) {
		t.Parallel()

		err := httpclient.NewHTTPError(500, "http://api.example.com", "Server Error")

		// Call Error() multiple times to ensure consistency
		firstCall := err.Error()
		secondCall := err.Error()

		assert.Equal(t, firstCall, secondCall, "Error() should return consistent results")
	})
}

func TestHTTPError_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		url        string
		message    string
	}{
		{
			name:       "zero status code",
			statusCode: 0,
			url:        "http://example.com",
			message:    "Zero Status",
		},
		{
			name:       "negative status code",
			statusCode: -1,
			url:        "http://example.com",
			message:    "Negative Status",
		},
		{
			name:       "empty URL",
			statusCode: 404,
			url:        "",
			message:    "Not Found",
		},
		{
			name:       "URL with special characters",
			statusCode: 404,
			url:        "http://example.com/path?query=value&foo=bar#anchor",
			message:    "Not Found",
		},
		{
			name:       "message with special characters",
			statusCode: 500,
			url:        "http://example.com",
			message:    "Error: something went wrong! <script>alert('xss')</script>",
		},
		{
			name:       "very large status code",
			statusCode: 999,
			url:        "http://example.com",
			message:    "Unknown Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := httpclient.NewHTTPError(tt.statusCode, tt.url, tt.message)

			require.Error(t, err)
			assert.NotEmpty(t, err.Error())
		})
	}
}
