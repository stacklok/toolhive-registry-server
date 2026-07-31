// SPDX-FileCopyrightText: Copyright 2025 Stacklok, Inc.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateKubernetesConfig_DiscoverTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     *KubernetesConfig
		wantErr bool
	}{
		{"empty", &KubernetesConfig{}, false},
		{"valid duration", &KubernetesConfig{DiscoverTimeout: "10s"}, false},
		{"invalid duration", &KubernetesConfig{DiscoverTimeout: "not-a-duration"}, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateKubernetesConfig(tt.cfg, "src", false)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateDiscoverOAuth2(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "secret")
	require.NoError(t, os.WriteFile(secretPath, []byte("s3cret"), 0o600))

	base := func() *DiscoverOAuth2Yaml {
		return &DiscoverOAuth2Yaml{
			TokenURL:         "https://idp.example/token",
			ClientID:         "cid",
			ClientSecretFile: secretPath,
		}
	}

	t.Run("valid HTTPS config", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, validateDiscoverOAuth2(base(), "src", false))
	})

	t.Run("missing tokenUrl", func(t *testing.T) {
		t.Parallel()
		c := base()
		c.TokenURL = ""
		assert.Error(t, validateDiscoverOAuth2(c, "src", false))
	})

	t.Run("missing clientId", func(t *testing.T) {
		t.Parallel()
		c := base()
		c.ClientID = ""
		assert.Error(t, validateDiscoverOAuth2(c, "src", false))
	})

	t.Run("missing clientSecretFile", func(t *testing.T) {
		t.Parallel()
		c := base()
		c.ClientSecretFile = ""
		assert.Error(t, validateDiscoverOAuth2(c, "src", false))
	})

	t.Run("relative clientSecretFile rejected", func(t *testing.T) {
		t.Parallel()
		c := base()
		c.ClientSecretFile = "relative/path"
		assert.Error(t, validateDiscoverOAuth2(c, "src", false))
	})

	t.Run("invalid tokenUrl format rejected", func(t *testing.T) {
		t.Parallel()
		c := base()
		c.TokenURL = ":://bad"
		assert.Error(t, validateDiscoverOAuth2(c, "src", false))
	})

	t.Run("HTTP tokenUrl rejected without insecure flag", func(t *testing.T) {
		t.Parallel()
		c := base()
		c.TokenURL = "http://idp.example/token"
		assert.Error(t, validateDiscoverOAuth2(c, "src", false))
	})

	t.Run("HTTP tokenUrl allowed with insecure flag", func(t *testing.T) {
		t.Parallel()
		c := base()
		c.TokenURL = "http://idp.example/token"
		assert.NoError(t, validateDiscoverOAuth2(c, "src", true))
	})

	t.Run("HTTP tokenUrl on loopback host is allowed", func(t *testing.T) {
		t.Parallel()
		c := base()
		c.TokenURL = "http://127.0.0.1:8080/token"
		assert.NoError(t, validateDiscoverOAuth2(c, "src", false))
	})
}

func TestDiscoverOAuth2Yaml_GetClientSecret(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "s")
	require.NoError(t, os.WriteFile(path, []byte("  topsecret  \n"), 0o600))

	d := &DiscoverOAuth2Yaml{ClientSecretFile: path}
	got, err := d.GetClientSecret()
	require.NoError(t, err)
	assert.Equal(t, "topsecret", got)
}

func TestDiscoverOAuth2Yaml_LogValueRedactsSecret(t *testing.T) {
	t.Parallel()
	d := &DiscoverOAuth2Yaml{
		TokenURL:         "https://idp/token",
		ClientID:         "cid",
		ClientSecretFile: "/etc/secret",
		Audience:         "aud",
	}
	// LogValue must not expose the ClientSecretFile path directly and must
	// carry only a presence indicator for the secret material.
	attrs := d.LogValue().Group()
	found := false
	for _, a := range attrs {
		if a.Key == "hasClientSecretFile" {
			found = true
			assert.True(t, a.Value.Bool())
		}
		if a.Key == "clientSecretFile" {
			t.Fatalf("LogValue must not emit clientSecretFile directly")
		}
	}
	assert.True(t, found, "hasClientSecretFile indicator must be present")
}
