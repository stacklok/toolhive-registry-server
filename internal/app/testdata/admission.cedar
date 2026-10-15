// Explicit lifecycle-test fixture, not a deployment policy.
permit(principal is Registry::Principal, action == Registry::Action::"catalog.read", resource is Registry::View);
permit(principal is Registry::Principal, action == Registry::Action::"metadata.read", resource == Registry::Instance::"catalog");
