package authz

// defaultPolicies contains the built-in Cedar authorization policies.
// These check principal.grantedActions (populated from scope mapping in Go code)
// rather than checking scope names directly. This decouples scope naming from
// policy evaluation, allowing custom scope-to-action mappings to work without
// requiring custom Cedar policies.
const defaultPolicies = `
permit(
  principal,
  action == ToolHive::Registry::Action::"read",
  resource
) when {
  principal.grantedActions.contains("read")
};

permit(
  principal,
  action == ToolHive::Registry::Action::"write",
  resource
) when {
  principal.grantedActions.contains("write")
};

permit(
  principal,
  action == ToolHive::Registry::Action::"admin",
  resource
) when {
  principal.grantedActions.contains("admin")
};
`
