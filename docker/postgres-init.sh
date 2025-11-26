#!/bin/sh
set -e

# This script runs automatically when the PostgreSQL container is initialized
# It creates two separate database users for the two-user security model:
#
# 1. db_app - Application user with limited privileges (SELECT, INSERT, UPDATE, DELETE)
#    Used by the server for normal operations
#
# 2. db_migrator - Migration user with elevated privileges (CREATE, ALTER, DROP)
#    Used only for running database schema migrations
#
# Password management:
# - Passwords are provided via a pgpass file mounted into the container
# - The pgpass file format is: hostname:port:database:username:password
# - Example pgpass file contents:
#     postgres:5432:registry:db_app:app_password
#     postgres:5432:registry:db_migrator:migration_password
#
# See: https://www.postgresql.org/docs/current/libpq-pgpass.html

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    -- Create the toolhive_registry_server role if it doesn't exist
    -- This role is used by migrations to grant privileges
    DO \$\$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'toolhive_registry_server') THEN
            CREATE ROLE toolhive_registry_server;
        END IF;
    END
    \$\$;

    -- Create application user (limited privileges for normal operations)
    CREATE USER db_app WITH PASSWORD 'app_password';
    GRANT CONNECT ON DATABASE $POSTGRES_DB TO db_app;
    GRANT USAGE ON SCHEMA public TO db_app;
    -- Note: Table-level privileges are granted by migrations via the toolhive_registry_server role
    GRANT toolhive_registry_server TO db_app;

    -- Create migration user (elevated privileges for schema changes)
    CREATE USER db_migrator WITH PASSWORD 'migration_password';
    GRANT CONNECT ON DATABASE $POSTGRES_DB TO db_migrator;
    GRANT ALL PRIVILEGES ON SCHEMA public TO db_migrator;
    GRANT CREATE ON SCHEMA public TO db_migrator;
    GRANT ALL PRIVILEGES ON DATABASE $POSTGRES_DB TO db_migrator;
    GRANT toolhive_registry_server TO db_migrator;
EOSQL

echo "Database users created successfully:"
echo "  - db_app: Application user (SELECT, INSERT, UPDATE, DELETE)"
echo "  - db_migrator: Migration user (CREATE, ALTER, DROP, plus all app privileges)"
echo ""
echo "Password management: Use a pgpass file to provide credentials for both users."
echo "See: https://www.postgresql.org/docs/current/libpq-pgpass.html"
