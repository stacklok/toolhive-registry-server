// Package kubernetes adapts the library's leased operator runner
// (pkg/registry/operator) to the server: it builds the API client, preflights
// a Kubernetes source's namespaces, and records each exported resource's
// toolhive.stacklok.dev/authz-claims annotation for claims mode.
package kubernetes
