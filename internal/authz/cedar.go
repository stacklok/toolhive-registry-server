package authz

import (
	"context"
	"fmt"
	"log/slog"

	cedar "github.com/cedar-policy/cedar-go"
)

const cedarNamespace = "ToolHive::Registry"

type cedarAuthorizer struct {
	policySet *cedar.PolicySet
}

// NewCedarAuthorizer creates a new Cedar-based authorizer.
// If policyBytes is nil, built-in default policies are used.
func NewCedarAuthorizer(policyBytes []byte) (*cedarAuthorizer, error) {
	if policyBytes == nil {
		policyBytes = []byte(defaultPolicies)
	}

	ps, err := cedar.NewPolicySetFromBytes("policies.cedar", policyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Cedar policies: %w", err)
	}

	return &cedarAuthorizer{policySet: ps}, nil
}

// Authorize checks if the principal with the given granted actions can perform
// the specified action on the resource using Cedar policy evaluation.
func (a *cedarAuthorizer) Authorize(ctx context.Context, req Request) (Decision, error) {
	_ = ctx // reserved for future use (e.g., tracing)

	// Build principal entity with grantedActions attribute
	principalUID := cedar.NewEntityUID(cedar.EntityType(cedarNamespace+"::User"), cedar.String("authenticated"))

	actionValues := make([]cedar.Value, len(req.GrantedActions))
	for i, action := range req.GrantedActions {
		actionValues[i] = cedar.String(action)
	}

	entities := cedar.EntityMap{
		principalUID: cedar.Entity{
			UID: principalUID,
			Attributes: cedar.NewRecord(cedar.RecordMap{
				"grantedActions": cedar.NewSet(actionValues...),
			}),
		},
	}

	// Build action entity UID
	actionUID := cedar.NewEntityUID(cedar.EntityType(cedarNamespace+"::Action"), cedar.String(req.Action))

	// Build resource entity UID
	resourceType := req.ResourceType
	if resourceType == "" {
		resourceType = "Subregistry"
	}
	resourceID := req.ResourceID
	if resourceID == "" {
		resourceID = "global"
	}
	resourceUID := cedar.NewEntityUID(cedar.EntityType(cedarNamespace+"::"+resourceType), cedar.String(resourceID))

	// Build Cedar request
	cedarReq := cedar.Request{
		Principal: principalUID,
		Action:    actionUID,
		Resource:  resourceUID,
		Context:   cedar.NewRecord(cedar.RecordMap{}),
	}

	// Evaluate using the top-level Authorize function (preferred over deprecated IsAuthorized)
	decision, diagnostic := cedar.Authorize(a.policySet, entities, cedarReq)

	slog.Debug("Authorization decision",
		"action", req.Action,
		"decision", decision,
		"grantedActions", req.GrantedActions,
		"resource", req.ResourceID,
	)

	// Collect reasons from diagnostic
	var reasons []string
	for _, r := range diagnostic.Reasons {
		reasons = append(reasons, string(r.PolicyID))
	}

	return Decision{
		Allowed: decision == cedar.Allow,
		Reasons: reasons,
	}, nil
}
