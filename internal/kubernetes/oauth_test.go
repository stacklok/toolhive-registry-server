// SPDX-FileCopyrightText: Copyright 2025 Stacklok, Inc.
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTokenSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("nil config returns nil source and nil error", func(t *testing.T) {
		t.Parallel()
		ts, err := NewTokenSource(ctx, nil)
		assert.NoError(t, err)
		assert.Nil(t, ts)
	})

	t.Run("missing TokenURL is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := NewTokenSource(ctx, &DiscoverOAuth2Config{
			ClientID:     "cid",
			ClientSecret: "sec",
		})
		assert.Error(t, err)
	})

	t.Run("missing ClientID is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := NewTokenSource(ctx, &DiscoverOAuth2Config{
			TokenURL:     "https://idp.example/token",
			ClientSecret: "sec",
		})
		assert.Error(t, err)
	})

	t.Run("missing ClientSecret is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := NewTokenSource(ctx, &DiscoverOAuth2Config{
			TokenURL: "https://idp.example/token",
			ClientID: "cid",
		})
		assert.Error(t, err)
	})

	t.Run("valid config returns non-nil source", func(t *testing.T) {
		t.Parallel()
		ts, err := NewTokenSource(ctx, &DiscoverOAuth2Config{
			TokenURL:     "https://idp.example/token",
			ClientID:     "cid",
			ClientSecret: "sec",
			Scopes:       []string{"mcp:tools"},
			Audience:     "registry",
			EndpointParams: map[string]string{
				"resource": "https://mcp.example",
			},
		})
		require.NoError(t, err)
		assert.NotNil(t, ts)
	})
}
