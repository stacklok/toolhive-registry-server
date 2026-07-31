// SPDX-FileCopyrightText: Copyright 2025 Stacklok, Inc.
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"golang.org/x/oauth2"
)

const (
	discoverClientName = "toolhive-registry-discovery"
)

// discoverTools connects to the MCP proxy at registryURL and returns its
// tools/list response. Returns nil on any error — callers treat nil as
// "leave ToolDefinitions empty, retry on next reconcile".
//
// When ts is non-nil, a Bearer token is fetched via oauth2.TokenSource and
// injected as an Authorization header on the transport. Token fetch failures
// yield a nil result (skip this reconcile) rather than crashing the loop.
func discoverTools(
	ctx context.Context,
	registryURL, serverName, namespace string,
	timeout time.Duration,
	ts oauth2.TokenSource,
) []mcp.Tool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	slog.Debug("lazy discovery: starting",
		"url", registryURL, "server", serverName,
		"namespace", namespace, "timeout", timeout, "oauth2", ts != nil)

	headers, err := buildAuthHeaders(ts)
	if err != nil {
		slog.Warn("lazy discovery: failed to obtain OAuth2 token, skipping",
			"url", registryURL, "server", serverName,
			"namespace", namespace, "error", err)
		return nil
	}

	c, err := connectMCPClient(ctx, registryURL, headers)
	if err != nil {
		slog.Warn("lazy discovery: failed to connect, skipping",
			"url", registryURL, "server", serverName,
			"namespace", namespace, "error", err)
		return nil
	}
	defer func() { _ = c.Close() }()

	resp, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		slog.Warn("lazy discovery: tools/list failed, skipping",
			"url", registryURL, "server", serverName,
			"namespace", namespace, "error", err)
		return nil
	}
	if len(resp.Tools) == 0 {
		slog.Warn("lazy discovery: tools/list returned empty list; "+
			"if Cedar is configured verify that mcp_tool_discovery role is granted",
			"url", registryURL, "server", serverName, "namespace", namespace)
		return nil
	}

	slog.Info("lazy discovery: discovered tools",
		"server", serverName, "namespace", namespace, "count", len(resp.Tools))
	return resp.Tools
}

// buildAuthHeaders returns the HTTP headers to attach to the MCP client
// transport. Returns nil headers (and nil error) when ts is nil — anonymous
// discovery is a supported mode.
func buildAuthHeaders(ts oauth2.TokenSource) (map[string]string, error) {
	if ts == nil {
		return nil, nil
	}
	tok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("fetch oauth2 token: %w", err)
	}
	return map[string]string{
		"Authorization": tok.Type() + " " + tok.AccessToken,
	}, nil
}

// connectMCPClient is a var so unit tests can replace it with a mock without
// needing an interface. Tries streamable-http first, falls back to SSE.
var connectMCPClient = func(ctx context.Context, url string, headers map[string]string) (*mcpclient.Client, error) {
	var streamOpts []transport.StreamableHTTPCOption
	if len(headers) > 0 {
		streamOpts = append(streamOpts, transport.WithHTTPHeaders(headers))
	}
	if c, err := mcpclient.NewStreamableHttpClient(url, streamOpts...); err == nil {
		startErr := initMCPClient(ctx, c)
		if startErr == nil {
			slog.Debug("lazy discovery: connected via streamable-http", "url", url)
			return c, nil
		}
		slog.Debug("lazy discovery: streamable-http init failed, falling back to SSE",
			"url", url, "error", startErr)
		_ = c.Close()
	}
	var sseOpts []transport.ClientOption
	if len(headers) > 0 {
		sseOpts = append(sseOpts, mcpclient.WithHeaders(headers))
	}
	c, err := mcpclient.NewSSEMCPClient(url, sseOpts...)
	if err != nil {
		return nil, fmt.Errorf("create MCP client (tried streamable-http and SSE): %w", err)
	}
	if err := initMCPClient(ctx, c); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("initialize MCP client: %w", err)
	}
	slog.Debug("lazy discovery: connected via SSE", "url", url)
	return c, nil
}

func initMCPClient(ctx context.Context, c *mcpclient.Client) error {
	if err := c.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	req := mcp.InitializeRequest{}
	req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcp.Implementation{Name: discoverClientName, Version: "1.0"}
	if _, err := c.Initialize(ctx, req); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	return nil
}
