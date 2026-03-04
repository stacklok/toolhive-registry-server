package authz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCedarAuthorizer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		policyBytes []byte
		wantErr     string
	}{
		{
			name:        "nil bytes uses default policies",
			policyBytes: nil,
		},
		{
			name:        "empty bytes creates authorizer with no policies",
			policyBytes: []byte(""),
		},
		{
			name:        "invalid policy bytes returns error",
			policyBytes: []byte("this is not a valid cedar policy!!!"),
			wantErr:     "failed to parse Cedar policies",
		},
		{
			name: "valid custom policy bytes succeeds",
			policyBytes: []byte(`permit(
				principal,
				action == ToolHive::Registry::Action::"read",
				resource
			);`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			authorizer, err := NewCedarAuthorizer(tt.policyBytes)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, authorizer)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, authorizer)
			assert.NotNil(t, authorizer.policySet)
		})
	}
}

func TestCedarAuthorizer_Authorize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		request     Request
		wantAllowed bool
	}{
		{
			name: "grantedActions=[read] action=read is allowed",
			request: Request{
				GrantedActions: []string{"read"},
				Action:         "read",
			},
			wantAllowed: true,
		},
		{
			name: "grantedActions=[read] action=write is denied",
			request: Request{
				GrantedActions: []string{"read"},
				Action:         "write",
			},
			wantAllowed: false,
		},
		{
			name: "grantedActions=[read] action=admin is denied",
			request: Request{
				GrantedActions: []string{"read"},
				Action:         "admin",
			},
			wantAllowed: false,
		},
		{
			name: "grantedActions=[read,write] action=read is allowed",
			request: Request{
				GrantedActions: []string{"read", "write"},
				Action:         "read",
			},
			wantAllowed: true,
		},
		{
			name: "grantedActions=[read,write] action=write is allowed",
			request: Request{
				GrantedActions: []string{"read", "write"},
				Action:         "write",
			},
			wantAllowed: true,
		},
		{
			name: "grantedActions=[read,write] action=admin is denied",
			request: Request{
				GrantedActions: []string{"read", "write"},
				Action:         "admin",
			},
			wantAllowed: false,
		},
		{
			name: "grantedActions=[read,write,admin] action=admin is allowed",
			request: Request{
				GrantedActions: []string{"read", "write", "admin"},
				Action:         "admin",
			},
			wantAllowed: true,
		},
		{
			name: "grantedActions=[read,write,admin] action=read is allowed",
			request: Request{
				GrantedActions: []string{"read", "write", "admin"},
				Action:         "read",
			},
			wantAllowed: true,
		},
		{
			name: "grantedActions=[read,write,admin] action=write is allowed",
			request: Request{
				GrantedActions: []string{"read", "write", "admin"},
				Action:         "write",
			},
			wantAllowed: true,
		},
		{
			name: "empty grantedActions with action=read is denied",
			request: Request{
				GrantedActions: []string{},
				Action:         "read",
			},
			wantAllowed: false,
		},
		{
			name: "empty grantedActions with action=write is denied",
			request: Request{
				GrantedActions: []string{},
				Action:         "write",
			},
			wantAllowed: false,
		},
		{
			name: "empty grantedActions with action=admin is denied",
			request: Request{
				GrantedActions: []string{},
				Action:         "admin",
			},
			wantAllowed: false,
		},
		{
			name: "grantedActions=[write] action=write is allowed",
			request: Request{
				GrantedActions: []string{"write"},
				Action:         "write",
			},
			wantAllowed: true,
		},
		{
			name: "grantedActions=[write] action=read is denied (Cedar level)",
			request: Request{
				GrantedActions: []string{"write"},
				Action:         "read",
			},
			wantAllowed: false,
		},
		{
			name: "grantedActions=[admin] action=admin is allowed",
			request: Request{
				GrantedActions: []string{"admin"},
				Action:         "admin",
			},
			wantAllowed: true,
		},
		{
			name: "grantedActions=[admin] action=read is denied (Cedar level)",
			request: Request{
				GrantedActions: []string{"admin"},
				Action:         "read",
			},
			wantAllowed: false,
		},
		{
			name: "unknown action is denied",
			request: Request{
				GrantedActions: []string{"read", "write", "admin"},
				Action:         "unknown",
			},
			wantAllowed: false,
		},
	}

	// Create the authorizer once with default policies for all subtests
	authorizer, err := NewCedarAuthorizer(nil)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decision, err := authorizer.Authorize(context.Background(), tt.request)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAllowed, decision.Allowed)

			if tt.wantAllowed {
				assert.NotEmpty(t, decision.Reasons, "allowed decisions should have policy reasons")
			}
		})
	}
}

func TestCedarAuthorizer_Authorize_ResourceDefaults(t *testing.T) {
	t.Parallel()

	authorizer, err := NewCedarAuthorizer(nil)
	require.NoError(t, err)

	tests := []struct {
		name         string
		resourceType string
		resourceID   string
	}{
		{
			name:         "empty resource type and ID uses defaults",
			resourceType: "",
			resourceID:   "",
		},
		{
			name:         "explicit resource type and ID",
			resourceType: "Subregistry",
			resourceID:   "my-registry",
		},
		{
			name:         "only resource type specified",
			resourceType: "Server",
			resourceID:   "",
		},
		{
			name:         "only resource ID specified",
			resourceType: "",
			resourceID:   "my-resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// With read granted, a read action should succeed regardless of resource details
			decision, err := authorizer.Authorize(context.Background(), Request{
				GrantedActions: []string{"read"},
				Action:         "read",
				ResourceType:   tt.resourceType,
				ResourceID:     tt.resourceID,
			})
			require.NoError(t, err)
			assert.True(t, decision.Allowed, "read should be allowed with read granted regardless of resource")
		})
	}
}

func TestCedarAuthorizer_Authorize_CustomPolicy(t *testing.T) {
	t.Parallel()

	// Custom policy that only allows read, never write or admin
	customPolicy := []byte(`permit(
		principal,
		action == ToolHive::Registry::Action::"read",
		resource
	) when {
		principal.grantedActions.contains("read")
	};`)

	authorizer, err := NewCedarAuthorizer(customPolicy)
	require.NoError(t, err)

	tests := []struct {
		name        string
		request     Request
		wantAllowed bool
	}{
		{
			name: "custom policy allows read with read grant",
			request: Request{
				GrantedActions: []string{"read"},
				Action:         "read",
			},
			wantAllowed: true,
		},
		{
			name: "custom policy denies write even with write grant (no write policy)",
			request: Request{
				GrantedActions: []string{"write"},
				Action:         "write",
			},
			wantAllowed: false,
		},
		{
			name: "custom policy denies admin even with admin grant (no admin policy)",
			request: Request{
				GrantedActions: []string{"admin"},
				Action:         "admin",
			},
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decision, err := authorizer.Authorize(context.Background(), tt.request)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAllowed, decision.Allowed)
		})
	}
}
