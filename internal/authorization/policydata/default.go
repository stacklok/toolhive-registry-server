// Package policydata holds the standalone default policy bytes without evaluator
// or configuration dependencies, so offline review hashes the same policy as serving.
package policydata

// Default is evaluated by Cedar only after independent authentication and route
// enablement gates. A configured custom file replaces these bytes entirely.
const Default = "permit(principal, action, resource);\n"
