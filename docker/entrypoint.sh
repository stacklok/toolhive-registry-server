#!/bin/sh
set -e

# Function to run migrations
run_migrations() {
    echo "Running database migrations..."
    /main migrate up --config "$1" --yes
    echo "Migrations completed successfully"
}

# Check if the first argument is "serve"
if [ "$1" = "serve" ]; then
    # Save all original arguments for later
    ORIGINAL_ARGS="$@"

    # Extract config path from arguments
    CONFIG_PATH=""
    shift  # Remove "serve" from arguments

    # Parse arguments to find --config
    while [ $# -gt 0 ]; do
        case "$1" in
            --config)
                CONFIG_PATH="$2"
                shift 2
                ;;
            *)
                shift
                ;;
        esac
    done

    # Run migrations if config path is found
    if [ -n "$CONFIG_PATH" ]; then
        run_migrations "$CONFIG_PATH"
    else
        echo "Warning: No config file specified, skipping migrations"
    fi

    # Start the server with all original arguments
    exec /main $ORIGINAL_ARGS
else
    # For other commands (like migrate, version), just pass through
    exec /main "$@"
fi
