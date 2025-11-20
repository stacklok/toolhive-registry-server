#!/bin/sh
set -e

# This script runs automatically when the PostgreSQL container is initialized
# It creates separate database users for migrations and normal operations

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    -- Create migration user with elevated privileges for schema changes
    CREATE USER db_migrator WITH PASSWORD 'migration_password';
    GRANT ALL PRIVILEGES ON DATABASE $POSTGRES_DB TO db_migrator;
    GRANT ALL PRIVILEGES ON SCHEMA public TO db_migrator;

    -- Create application user with limited privileges for normal operations
    CREATE USER db_app WITH PASSWORD 'app_password';
    GRANT CONNECT ON DATABASE $POSTGRES_DB TO db_app;
    GRANT USAGE ON SCHEMA public TO db_app;

    -- Note: Table-level permissions will be granted automatically after migrations
    -- via ALTER DEFAULT PRIVILEGES (done in migration files)
EOSQL

echo "Database users created successfully:"
echo "  - db_migrator: Migration user with elevated privileges"
echo "  - db_app: Application user with limited privileges"
