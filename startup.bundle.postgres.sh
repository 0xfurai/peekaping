#!/bin/sh
set -e

validate_env_vars() {
    local errors=0

    if [ -z "$DB_USER" ]; then
        echo "ERROR: DB_USER is required and must be set"
        errors=1
    fi

    if [ -z "$DB_PASS" ]; then
        echo "ERROR: DB_PASS is required and must be set"
        errors=1
    fi

    if [ -z "$ACCESS_TOKEN_SECRET_KEY" ]; then
        echo "ERROR: ACCESS_TOKEN_SECRET_KEY is required and must be set"
        errors=1
    fi

    if [ -z "$REFRESH_TOKEN_SECRET_KEY" ]; then
        echo "ERROR: REFRESH_TOKEN_SECRET_KEY is required and must be set"
        errors=1
    fi

    # Validate secret key strength
    if [ ${#ACCESS_TOKEN_SECRET_KEY} -lt 16 ]; then
        echo "ERROR: ACCESS_TOKEN_SECRET_KEY must be at least 16 characters long"
        errors=1
    fi

    if [ ${#REFRESH_TOKEN_SECRET_KEY} -lt 16 ]; then
        echo "ERROR: REFRESH_TOKEN_SECRET_KEY must be at least 16 characters long"
        errors=1
    fi

    if [ $errors -eq 1 ]; then
        echo "Environment validation failed. Please fix the above errors."
        exit 1
    fi

    echo "Environment validation passed."
}

validate_env_vars

# Security: Function to safely execute SQL
execute_sql_safely() {
    local sql="$1"
    local temp_file=$(mktemp)
    chmod 600 "$temp_file"
    echo "$sql" > "$temp_file"
    chown postgres:postgres "$temp_file"
    gosu postgres psql -p "$DB_PORT" -f "$temp_file" -q
    rm -f "$temp_file"
}

# Create env.js file for the web app
cat >/app/web/env.js <<EOF
/* generated each container start */
window.__CONFIG__ = {
  API_URL: ""
};
EOF
# Security: Set appropriate permissions for web assets
chmod 644 /app/web/env.js

# Set default environment variables if not provided
export DB_TYPE=${DB_TYPE:-postgres}
export DB_HOST=${DB_HOST:-localhost}
export DB_PORT=${DB_PORT:-5432}
export DB_NAME=${DB_NAME:-peekaping}
export DB_USER=${DB_USER}
export DB_PASS=${DB_PASS}

# Set server configuration environment variables
export SERVER_PORT=${SERVER_PORT:-8034}
# Security: Use HTTPS by default
export CLIENT_URL=${CLIENT_URL:-http://localhost:8383}
export ACCESS_TOKEN_SECRET_KEY=${ACCESS_TOKEN_SECRET_KEY}
export REFRESH_TOKEN_SECRET_KEY=${REFRESH_TOKEN_SECRET_KEY}
export ACCESS_TOKEN_EXPIRED_IN=${ACCESS_TOKEN_EXPIRED_IN:-15m}
export REFRESH_TOKEN_EXPIRED_IN=${REFRESH_TOKEN_EXPIRED_IN:-168h}
export MODE=${MODE:-prod}
export TZ=${TZ:-UTC}

# Create .env file for the server with secure permissions
cat > /app/.env << EOF
SERVER_PORT=$SERVER_PORT
CLIENT_URL=$CLIENT_URL
DB_TYPE=$DB_TYPE
DB_HOST=$DB_HOST
DB_PORT=$DB_PORT
DB_NAME=$DB_NAME
DB_USER=$DB_USER
DB_PASS=$DB_PASS
ACCESS_TOKEN_SECRET_KEY=$ACCESS_TOKEN_SECRET_KEY
REFRESH_TOKEN_SECRET_KEY=$REFRESH_TOKEN_SECRET_KEY
ACCESS_TOKEN_EXPIRED_IN=$ACCESS_TOKEN_EXPIRED_IN
REFRESH_TOKEN_EXPIRED_IN=$REFRESH_TOKEN_EXPIRED_IN
MODE=$MODE
TZ=$TZ
EOF

# Security: Set restrictive permissions on sensitive config file
chmod 600 /app/.env

# Create data directory if it doesn't exist
mkdir -p /var/lib/postgresql/data

# Create log directory and fix permissions
mkdir -p /var/log/supervisor
chown -R postgres:postgres /var/log/supervisor
chmod 755 /var/log/supervisor

# Fix ownership and permissions of PostgreSQL data directory
chown -R postgres:postgres /var/lib/postgresql/data
chmod 700 /var/lib/postgresql/data

# Initialize PostgreSQL if needed
if [ ! -f /var/lib/postgresql/data/.postgres_initialized ]; then
    echo "Initializing PostgreSQL..."

    # Clear data directory if it exists but is not initialized
    if [ -d /var/lib/postgresql/data ]; then
        rm -rf /var/lib/postgresql/data/*
    fi

    # Ensure ownership after clearing
    chown -R postgres:postgres /var/lib/postgresql/data

    # Initialize PostgreSQL cluster
    gosu postgres initdb -D /var/lib/postgresql/data

    # Start PostgreSQL temporarily with configurable port
    gosu postgres pg_ctl -D /var/lib/postgresql/data -o "-p $DB_PORT" -l /var/log/supervisor/postgres-init.log start

    # Wait for PostgreSQL to be ready with timeout
    echo "Waiting for PostgreSQL to be ready..."
    timeout=30
    while [ $timeout -gt 0 ]; do
        if gosu postgres pg_isready -p "$DB_PORT" -q; then
            break
        fi
        sleep 1
        timeout=$((timeout - 1))
    done

    if [ $timeout -eq 0 ]; then
        echo "Error: PostgreSQL failed to start within timeout"
        exit 1
    fi

    # Security: Create database and user using secure method
    echo "Creating database and user..."

    # Create user with secure password handling
    execute_sql_safely "CREATE USER \"$DB_USER\" WITH PASSWORD '$DB_PASS';"

    # Create database with proper ownership
    execute_sql_safely "CREATE DATABASE \"$DB_NAME\" OWNER \"$DB_USER\";"

    # Grant minimal required privileges instead of ALL
    execute_sql_safely "GRANT CONNECT ON DATABASE \"$DB_NAME\" TO \"$DB_USER\";"
    execute_sql_safely "GRANT USAGE ON SCHEMA public TO \"$DB_USER\";"
    execute_sql_safely "GRANT CREATE ON SCHEMA public TO \"$DB_USER\";"

    # Security: Connect to the specific database to grant table permissions
    PGDATABASE="$DB_NAME" execute_sql_safely "GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO \"$DB_USER\";"
    PGDATABASE="$DB_NAME" execute_sql_safely "GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO \"$DB_USER\";"
    PGDATABASE="$DB_NAME" execute_sql_safely "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO \"$DB_USER\";"
    PGDATABASE="$DB_NAME" execute_sql_safely "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO \"$DB_USER\";"

    # Stop PostgreSQL
    gosu postgres pg_ctl -D /var/lib/postgresql/data stop

    # Mark as initialized
    touch /var/lib/postgresql/data/.postgres_initialized
    chown postgres:postgres /var/lib/postgresql/data/.postgres_initialized
    echo "PostgreSQL initialization completed!"
fi

# Start PostgreSQL for migrations
echo "Starting PostgreSQL for migrations..."
gosu postgres pg_ctl -D /var/lib/postgresql/data -o "-p $DB_PORT" -l /var/log/supervisor/postgres-migration.log start

# Wait for PostgreSQL to be ready with timeout
echo "Waiting for PostgreSQL to be ready for migrations..."
timeout=30
while [ $timeout -gt 0 ]; do
    if gosu postgres pg_isready -p "$DB_PORT" -q; then
        break
    fi
    sleep 1
    timeout=$((timeout - 1))
done

if [ $timeout -eq 0 ]; then
    echo "Error: PostgreSQL failed to start for migrations within timeout"
    exit 1
fi

# Run database migrations
echo "Running database migrations..."
cd /app/server
if ./run-migrations.sh; then
    echo "Migrations completed successfully!"
else
    echo "ERROR: Migration failed!"
    exit 1
fi

# Stop PostgreSQL after migrations (supervisor will start it again)
echo "Stopping PostgreSQL after migrations..."
gosu postgres pg_ctl -D /var/lib/postgresql/data stop

# Start supervisor to manage PostgreSQL, server, and Caddy
echo "Starting supervisor to manage PostgreSQL, server, and Caddy..."

# Note: Environment variables are passed to supervisor processes via the environment= directive
# in the supervisor configuration, so they remain available to the server even if cleared here
exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
