// SPDX-FileCopyrightText: Copyright 2025 Stacklok, Inc.
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// stubTokenSource is a minimal oauth2.TokenSource used to exercise the
// header-injection path without hitting a real IdP.
type stubTokenSource struct {
	tok *oauth2.Token
	err error
}

func (s stubTokenSource) Token() (*oauth2.Token, error) { return s.tok, s.err }

//nolint:paralleltest,tparallel // subtests write connectMCPClient package-level var and must run sequentially
func TestDiscoverTools(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		setupServer   func(t *testing.T) *httptest.Server
		stubConnect   func(t *testing.T)
		tokenSource   oauth2.TokenSource
		expectedCount int
		expectedNames []string
		expectNil     bool
		timeout       time.Duration
	}{
		{
			name: "success with two tools",
			setupServer: func(t *testing.T) *httptest.Server {
				t.Helper()
				s := mcpserver.NewMCPServer("test-server", "1.0")
				s.AddTool(mcp.NewTool("tool_a", mcp.WithDescription("first tool")),
					func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return mcp.NewToolResultText("ok"), nil
					})
				s.AddTool(mcp.NewTool("tool_b", mcp.WithDescription("second tool")),
					func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return mcp.NewToolResultText("ok"), nil
					})
				handler := mcpserver.NewStreamableHTTPServer(s)
				return httptest.NewServer(handler)
			},
			expectedCount: 2,
			expectedNames: []string{"tool_a", "tool_b"},
		},
		{
			name: "connect failure returns nil",
			stubConnect: func(t *testing.T) {
				t.Helper()
				connectMCPClient = func(_ context.Context, _ string, _ map[string]string) (*mcpclient.Client, error) {
					return nil, fmt.Errorf("connection refused")
				}
			},
			expectNil: true,
		},
		{
			name: "empty tools list returns nil",
			setupServer: func(t *testing.T) *httptest.Server {
				t.Helper()
				s := mcpserver.NewMCPServer("empty-server", "1.0")
				handler := mcpserver.NewStreamableHTTPServer(s)
				return httptest.NewServer(handler)
			},
			expectNil: true,
		},
		{
			name: "context cancelled before connect completes returns nil",
			stubConnect: func(t *testing.T) {
				t.Helper()
				connectMCPClient = func(ctx context.Context, _ string, _ map[string]string) (*mcpclient.Client, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}
			},
			expectNil: true,
			timeout:   100 * time.Millisecond,
		},
		{
			name: "token source error skips discovery (nil result)",
			stubConnect: func(t *testing.T) {
				t.Helper()
				connectMCPClient = func(_ context.Context, _ string, _ map[string]string) (*mcpclient.Client, error) {
					t.Fatalf("connectMCPClient must not be called when token fetch fails")
					return nil, nil
				}
			},
			tokenSource: stubTokenSource{err: fmt.Errorf("idp down")},
			expectNil:   true,
		},
		{
			name: "token source success injects Authorization header",
			stubConnect: func(t *testing.T) {
				t.Helper()
				connectMCPClient = func(_ context.Context, _ string, headers map[string]string) (*mcpclient.Client, error) {
					require.NotNil(t, headers)
					assert.Equal(t, "Bearer abc123", headers["Authorization"])
					return nil, fmt.Errorf("stop-after-header-check")
				}
			},
			tokenSource: stubTokenSource{tok: &oauth2.Token{AccessToken: "abc123", TokenType: "Bearer"}},
			expectNil:   true,
		},
	}

	for _, tt := range tests {
		tt := tt
		//nolint:paralleltest // subtests share connectMCPClient package-level var; parallel writes would race
		t.Run(tt.name, func(t *testing.T) {
			original := connectMCPClient
			t.Cleanup(func() { connectMCPClient = original })

			var serverURL string

			if tt.setupServer != nil {
				ts := tt.setupServer(t)
				defer ts.Close()
				serverURL = ts.URL
			}

			if tt.stubConnect != nil {
				tt.stubConnect(t)
				if serverURL == "" {
					serverURL = "http://unused"
				}
			}

			ctx := context.Background()
			if tt.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.timeout)
				defer cancel()
			}

			result := discoverTools(ctx, serverURL, "test-server", "default", defaultDiscoverTimeout, tt.tokenSource)

			if tt.expectNil {
				assert.Nil(t, result)
				return
			}

			require.NotNil(t, result)
			assert.Len(t, result, tt.expectedCount)

			if tt.expectedNames != nil {
				names := make([]string, len(result))
				for i, tool := range result {
					names[i] = tool.Name
				}
				assert.ElementsMatch(t, tt.expectedNames, names)
			}
		})
	}
}

func TestBuildAuthHeaders(t *testing.T) {
	t.Parallel()
	t.Run("nil TokenSource returns nil headers, nil error", func(t *testing.T) {
		t.Parallel()
		h, err := buildAuthHeaders(nil)
		assert.NoError(t, err)
		assert.Nil(t, h)
	})

	t.Run("error propagates", func(t *testing.T) {
		t.Parallel()
		_, err := buildAuthHeaders(stubTokenSource{err: fmt.Errorf("boom")})
		assert.Error(t, err)
	})

	t.Run("Bearer header formatted correctly", func(t *testing.T) {
		t.Parallel()
		h, err := buildAuthHeaders(stubTokenSource{
			tok: &oauth2.Token{AccessToken: "xyz", TokenType: "Bearer"},
		})
		require.NoError(t, err)
		assert.Equal(t, "Bearer xyz", h["Authorization"])
	})

	t.Run("default TokenType is Bearer when unset", func(t *testing.T) {
		t.Parallel()
		h, err := buildAuthHeaders(stubTokenSource{
			tok: &oauth2.Token{AccessToken: "xyz"},
		})
		require.NoError(t, err)
		// oauth2.Token.Type() returns "Bearer" when TokenType is empty
		assert.Equal(t, "Bearer xyz", h["Authorization"])
	})
}
