package auth

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentityFromClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		claims   jwt.MapClaims
		wantSub  string
		wantUser string
	}{
		{
			name:     "nil claims returns empty",
			claims:   nil,
			wantSub:  "",
			wantUser: "",
		},
		{
			name:     "empty claims returns empty",
			claims:   jwt.MapClaims{},
			wantSub:  "",
			wantUser: "",
		},
		{
			name:     "sub only",
			claims:   jwt.MapClaims{"sub": "user-1"},
			wantSub:  "user-1",
			wantUser: "",
		},
		{
			name:     "name preferred over preferred_username and email",
			claims:   jwt.MapClaims{"sub": "u", "name": "Alice", "preferred_username": "alice", "email": "alice@example.com"},
			wantSub:  "u",
			wantUser: "Alice",
		},
		{
			name:     "preferred_username preferred over email",
			claims:   jwt.MapClaims{"sub": "u", "preferred_username": "alice", "email": "alice@example.com"},
			wantSub:  "u",
			wantUser: "alice",
		},
		{
			name:     "email is last fallback",
			claims:   jwt.MapClaims{"sub": "u", "email": "alice@example.com"},
			wantSub:  "u",
			wantUser: "alice@example.com",
		},
		{
			name:     "missing sub is empty",
			claims:   jwt.MapClaims{"aud": "api", "name": "Alice"},
			wantSub:  "",
			wantUser: "Alice",
		},
		{
			name:     "non-string claims are ignored",
			claims:   jwt.MapClaims{"sub": 123, "name": []string{"a"}},
			wantSub:  "",
			wantUser: "",
		},
		{
			name:     "empty string display claims fall through",
			claims:   jwt.MapClaims{"sub": "u", "name": "", "preferred_username": "", "email": "alice@example.com"},
			wantSub:  "u",
			wantUser: "alice@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sub, user := IdentityFromClaims(tt.claims)
			assert.Equal(t, tt.wantSub, sub)
			assert.Equal(t, tt.wantUser, user)
		})
	}
}

func TestIdentityFromContext_FromClaims(t *testing.T) {
	t.Parallel()

	ctx := ContextWithClaims(context.Background(), jwt.MapClaims{
		"sub":                "user-1",
		"preferred_username": "alice",
	})
	sub, user := IdentityFromContext(ctx)
	assert.Equal(t, "user-1", sub)
	assert.Equal(t, "alice", user)
}

func TestIdentityFromContext_NoClaims(t *testing.T) {
	t.Parallel()

	sub, user := IdentityFromContext(context.Background())
	assert.Empty(t, sub)
	assert.Empty(t, user)
}

func TestIdentityHolder_PopulatedFromInnerContext(t *testing.T) {
	t.Parallel()

	// Outer scope installs a holder.
	outer := WithIdentityHolder(context.Background())

	// Inner scope (e.g., auth middleware after r.WithContext) extends it
	// with claims and writes to the holder. Crucially the inner ctx is a
	// child of outer — outer never sees the claims attached at the inner
	// scope, but it does see the holder mutation because the pointer is
	// shared.
	inner := ContextWithClaims(outer, jwt.MapClaims{"sub": "user-1", "name": "Alice"})
	SetIdentity(inner, "user-1", "Alice")

	sub, user := IdentityFromContext(outer)
	assert.Equal(t, "user-1", sub)
	assert.Equal(t, "Alice", user)
}

func TestIdentityHolder_PrefersHolderOverClaims(t *testing.T) {
	t.Parallel()

	// Holder set with one identity, but ctx also carries different claims
	// (would not happen in practice — used here to verify resolution order).
	ctx := WithIdentityHolder(context.Background())
	ctx = ContextWithClaims(ctx, jwt.MapClaims{"sub": "from-claims"})
	SetIdentity(ctx, "from-holder", "")

	sub, _ := IdentityFromContext(ctx)
	assert.Equal(t, "from-holder", sub)
}

func TestIdentityHolder_UnsetFallsBackToClaims(t *testing.T) {
	t.Parallel()

	// Holder installed but never populated → falls through to claims.
	ctx := WithIdentityHolder(context.Background())
	ctx = ContextWithClaims(ctx, jwt.MapClaims{"sub": "from-claims"})

	sub, _ := IdentityFromContext(ctx)
	assert.Equal(t, "from-claims", sub)
}

func TestSetIdentity_NoHolderIsNoOp(t *testing.T) {
	t.Parallel()

	// Should not panic when no holder is installed.
	require.NotPanics(t, func() {
		SetIdentity(context.Background(), "user-1", "Alice")
	})
}

// TestIdentityHolder_ConcurrentAccess hammers the holder from multiple
// goroutines so the race detector exercises the mutex. The values read
// don't matter — we only care that no race is reported.
func TestIdentityHolder_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	ctx := WithIdentityHolder(context.Background())

	const iters = 1000
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			SetIdentity(ctx, "u-"+strconv.Itoa(i), "Alice")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			SetIdentity(ctx, "v-"+strconv.Itoa(i), "Bob")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_, _ = IdentityFromContext(ctx)
		}
	}()
	wg.Wait()

	// Final state must be coherent: whichever writer landed last, both
	// strings reflect that single store call (no torn read).
	sub, user := IdentityFromContext(ctx)
	switch user {
	case "Alice":
		assert.Truef(t, len(sub) > 0 && sub[0] == 'u', "torn read: sub=%q user=%q", sub, user)
	case "Bob":
		assert.Truef(t, len(sub) > 0 && sub[0] == 'v', "torn read: sub=%q user=%q", sub, user)
	default:
		t.Fatalf("unexpected user value: %q", user)
	}
}

func TestClaimString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		claims   map[string]any
		key      string
		expected string
	}{
		{name: "existing string claim", claims: map[string]any{"name": "Alice"}, key: "name", expected: "Alice"},
		{name: "missing claim", claims: map[string]any{"name": "Alice"}, key: "email", expected: ""},
		{name: "non-string claim", claims: map[string]any{"iat": 12345}, key: "iat", expected: ""},
		{name: "nil claims map", claims: nil, key: "name", expected: ""},
		{name: "empty string claim", claims: map[string]any{"name": ""}, key: "name", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, claimString(tt.claims, tt.key))
		})
	}
}
