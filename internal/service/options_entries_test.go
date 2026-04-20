package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithEntryType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		entryType string
		wantErr   bool
		wantValue string
	}{
		{name: "server is valid", entryType: EntryTypeServer, wantErr: false, wantValue: EntryTypeServer},
		{name: "skill is valid", entryType: EntryTypeSkill, wantErr: false, wantValue: EntryTypeSkill},
		{name: "empty rejected", entryType: "", wantErr: true},
		{name: "unknown rejected", entryType: "widget", wantErr: true},
		{name: "wrong case rejected", entryType: "Server", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := &UpdateEntryClaimsOptions{}
			err := WithEntryType(tt.entryType)(opts)

			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidEntryType)
				assert.Empty(t, opts.EntryType, "EntryType must remain unset on error")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantValue, opts.EntryType)
		})
	}
}

func TestWithEntryType_WrongOptionType(t *testing.T) {
	t.Parallel()

	// An option type that doesn't implement entryTypeOption.
	type incompatibleOpts struct{}
	err := WithEntryType(EntryTypeServer)(&incompatibleOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid option type")
}

func TestUpdateEntryClaimsOptions_Setters(t *testing.T) {
	t.Parallel()

	t.Run("setName stores the name", func(t *testing.T) {
		t.Parallel()
		opts := &UpdateEntryClaimsOptions{}
		err := WithName("com.example/widget")(opts)
		require.NoError(t, err)
		assert.Equal(t, "com.example/widget", opts.Name)
	})

	t.Run("setClaims stores the claims map", func(t *testing.T) {
		t.Parallel()
		opts := &UpdateEntryClaimsOptions{}
		claims := map[string]any{"org": "acme", "team": "platform"}
		err := WithClaims(claims)(opts)
		require.NoError(t, err)
		assert.Equal(t, claims, opts.Claims)
	})

	t.Run("setJWTClaims stores the JWT claims map", func(t *testing.T) {
		t.Parallel()
		opts := &UpdateEntryClaimsOptions{}
		jwtClaims := map[string]any{"sub": "user-1", "org": "acme"}
		err := WithJWTClaims(jwtClaims)(opts)
		require.NoError(t, err)
		assert.Equal(t, jwtClaims, opts.JWTClaims)
	})

	t.Run("multiple setters compose", func(t *testing.T) {
		t.Parallel()
		opts := &UpdateEntryClaimsOptions{}
		require.NoError(t, WithEntryType(EntryTypeSkill)(opts))
		require.NoError(t, WithName("skill-1")(opts))
		require.NoError(t, WithClaims(map[string]any{"org": "acme"})(opts))
		require.NoError(t, WithJWTClaims(map[string]any{"sub": "u1"})(opts))

		assert.Equal(t, EntryTypeSkill, opts.EntryType)
		assert.Equal(t, "skill-1", opts.Name)
		assert.Equal(t, map[string]any{"org": "acme"}, opts.Claims)
		assert.Equal(t, map[string]any{"sub": "u1"}, opts.JWTClaims)
	})
}
