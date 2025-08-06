#!/bin/sh

# Function to wait for database
wait_for_database() {
    echo "Waiting for database to be ready..."
    case "$DB_TYPE" in
        "postgres"|"postgresql")
            while ! nc -z $DB_HOST $DB_PORT; do
                echo "Waiting for PostgreSQL at $DB_HOST:$DB_PORT..."
                sleep 2
            done
            ;;
        "mysql")
            while ! nc -z $DB_HOST $DB_PORT; do
                echo "Waiting for MySQL at $DB_HOST:$DB_PORT..."
                sleep 2
            done
            ;;
        "sqlite")
            echo "SQLite database - no connection check needed"
            ;;
        *)
            echo "Unknown database type: $DB_TYPE"
            exit 1
            ;;
    esac
    echo "Database is ready!"
}

# Function to check if database needs initialization
check_and_init_database() {
    echo "Checking if database needs initialization..."

    # Try to run status command to see if migration tables exist
    if ./bun db status >/dev/null 2>&1; then
        echo "Migration tables exist, proceeding with migrations..."
        return 0
    else
        echo "Migration tables not found, initializing database..."
        if ./bun db init; then
            echo "Database initialized successfully!"
            return 0
        else
            echo "Failed to initialize database!"
            return 1
        fi
    fi
}

# Skip migrations for MongoDB
if [ "$DB_TYPE" = "mongo" ] || [ "$DB_TYPE" = "mongodb" ]; then
    echo "Skipping migrations for MongoDB - not needed"
    exit 0
fi

# Wait for database and run migrations
wait_for_database

# Check if database needs initialization
if ! check_and_init_database; then
    echo "Database initialization failed!"
    exit 1
fi

echo "Running database migrations..."
if ./bun db migrate; then
    echo "Migrations completed successfully!"
else
    echo "Migration failed!"
    exit 1
fi
