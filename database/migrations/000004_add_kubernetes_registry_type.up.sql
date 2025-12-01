-- Add KUBERNETES value to registry_type enum
-- Kubernetes registries discover MCP servers from running Kubernetes resources

ALTER TYPE registry_type ADD VALUE 'KUBERNETES';
