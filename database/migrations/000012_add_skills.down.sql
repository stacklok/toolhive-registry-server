-- Rollback migration: Remove skill support

-- =============================================================================
-- Drop skill-related tables and types in reverse order of creation
-- =============================================================================
DROP TABLE IF EXISTS skill_git_package;
DROP TABLE IF EXISTS skill_oci_package;
DROP TABLE IF EXISTS skill;
DROP TYPE IF EXISTS skill_status;
