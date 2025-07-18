#!/bin/sh
set -e




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
