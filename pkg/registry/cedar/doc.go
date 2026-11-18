// Package cedar is the reference Cedar authorizer for the registry's HTTP API.
//
// It decides whether a caller may use a route, and nothing else. Every consumer
// route (/registry/{registryName}/v0.1/...) is the action catalog.read on the
// view named in the path, so a policy can grant or deny whole views. Every
// admin route (/v1/...) and host route guarded with [Authorizer.Protect] is an
// instance-wide action such as sources.write or entries.publish. There is no
// per-entry or per-source resource and no row filtering: once a request is
// admitted, the handler returns the same page it would return without Cedar.
//
// # Pieces
//
//   - [Engine] holds an immutable, schema-validated policy set. [Load] reads one
//     from disk at startup; [Parse] takes bytes from an embedder. There is no
//     file watcher: policy edits need a restart.
//   - [Mapping] and [ValidateMappings] describe how a verified token's claims
//     become the principal's optional groups and scopes attributes.
//   - [Authorizer] turns an Engine into route decorators. [Authorizer.Decorate]
//     plugs into httpapi.ConsumerOptions.Decorate and httpapi.AdminOptions.Decorate.
//
// The host owns authentication. It verifies the token, then exposes the result
// through [Options].Identity. This package never parses tokens.
//
// # Default policy
//
// When no policy is supplied, [DefaultPolicy] permits every action on every
// resource. With admin routes enabled, that means any authenticated caller has
// full admin access. Supply a policy file before enabling the admin API.
//
// # Schema
//
// The policy must validate strictly against the embedded schema (namespace
// Registry). The principal is Registry::Principal with attributes
// authenticated, issuer, subject and the optional groups and scopes sets. The
// resources are Registry::View (for catalog.read) and Registry::Instance::"catalog"
// (for everything else). The request context is always empty.
//
// This package is experimental. Its API may change without notice.
package cedar
