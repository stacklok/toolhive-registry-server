// Explicit lifecycle-test fixture, not a deployment policy.
permit(principal is Registry::Principal, action in [Registry::Action::"catalog.read", Registry::Action::"metadata.read"], resource == Registry::Instance::"catalog");
