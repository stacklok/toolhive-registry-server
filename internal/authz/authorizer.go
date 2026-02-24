// Package authz provides Cedar-based authorization for the registry API server.
package authz

import "context"

//go:generate mockgen -destination=mocks/mock_authorizer.go -package=mocks -source=authorizer.go Authorizer

// Authorizer evaluates authorization decisions using Cedar policies.
type Authorizer interface {
	// Authorize checks if the principal with the given granted actions
	// can perform the specified action on the resource.
	Authorize(ctx context.Context, req Request) (Decision, error)
}

// Request represents an authorization request.
type Request struct {
	// GrantedActions are the actions granted to the user based on scope mapping.
	GrantedActions []string

	// Action is the required Cedar action name (read, write, admin).
	Action string

	// ResourceType is the Cedar resource entity type (e.g., "Subregistry").
	ResourceType string

	// ResourceID is the resource identifier (e.g., registry name).
	ResourceID string
}

// Decision represents the result of an authorization check.
type Decision struct {
	// Allowed indicates whether the request is permitted.
	Allowed bool

	// Reasons provides policy IDs that contributed to the decision.
	Reasons []string
}
