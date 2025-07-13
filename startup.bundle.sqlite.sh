#!/bin/sh
set -e

validate_env_vars() {
    local errors=0

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

# Create env.js file for the web app
cat >/app/web/env.js <<EOF
/* generated each container start */
window.__CONFIG__ = {
  API_URL: ""
};
EOF
# Security: Set appropriate permissions for web assets
chmod 644 /app/web/env.js

# Set environment variables for SQLite
export DB_TYPE=sqlite
export DB_NAME=/app/data/peekaping.db

# Set server configuration environment variables
export SERVER_PORT=${SERVER_PORT:-8034}
export CLIENT_URL=${CLIENT_URL:-http://localhost:8383}
export ACCESS_TOKEN_SECRET_KEY=${ACCESS_TOKEN_SECRET_KEY}
export REFRESH_TOKEN_SECRET_KEY=${REFRESH_TOKEN_SECRET_KEY}
export ACCESS_TOKEN_EXPIRED_IN=${ACCESS_TOKEN_EXPIRED_IN:-15m}
export REFRESH_TOKEN_EXPIRED_IN=${REFRESH_TOKEN_EXPIRED_IN:-168h}
export MODE=${MODE:-prod}
export TZ=${TZ:-UTC}

# Create data directory if it doesn't exist
mkdir -p /app/data

# Run database migrations
echo "Running database migrations..."
cd /app/server
if ./run-migrations.sh; then
    echo "Migrations completed successfully!"
else
    echo "Migration failed!"
    exit 1
fi

# Start supervisor to manage both server and Caddy
echo "Starting supervisor to manage server and Caddy..."
exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
