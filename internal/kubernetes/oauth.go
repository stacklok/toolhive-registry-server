// SPDX-FileCopyrightText: Copyright 2025 Stacklok, Inc.
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"fmt"
	"net/url"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// DiscoverOAuth2Config carries the resolved OAuth2 client_credentials
// parameters used to obtain Bearer tokens for MCP tools/list calls.
//
// The struct is intentionally IdP-agnostic — the caller decides how to
// populate ClientSecret (from a file, env, or a secret manager). This
// package never reads secrets from disk directly.
type DiscoverOAuth2Config struct {
	TokenURL       string
	ClientID       string
	ClientSecret   string
	Scopes         []string
	Audience       string
	EndpointParams map[string]string
}

// NewTokenSource returns a cached oauth2.TokenSource that lazily fetches
// tokens via the client_credentials grant and refreshes them on expiry.
//
// The returned TokenSource wraps oauth2.ReuseTokenSource so callers can invoke
// Token() on every discovery attempt without triggering a token request until
// the previous token nears expiry.
//
// Returns (nil, nil) when cfg is nil, which callers interpret as "anonymous
// discovery" — no Authorization header is attached.
func NewTokenSource(ctx context.Context, cfg *DiscoverOAuth2Config) (oauth2.TokenSource, error) {
	if cfg == nil {
		return nil, nil
	}
	if cfg.TokenURL == "" {
		return nil, fmt.Errorf("oauth2: tokenURL is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("oauth2: clientID is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("oauth2: clientSecret is required")
	}

	params := url.Values{}
	if cfg.Audience != "" {
		params.Set("audience", cfg.Audience)
	}
	for k, v := range cfg.EndpointParams {
		params.Set(k, v)
	}

	cc := &clientcredentials.Config{
		ClientID:       cfg.ClientID,
		ClientSecret:   cfg.ClientSecret,
		TokenURL:       cfg.TokenURL,
		Scopes:         cfg.Scopes,
		EndpointParams: params,
		AuthStyle:      oauth2.AuthStyleInParams,
	}
	return cc.TokenSource(ctx), nil
}
