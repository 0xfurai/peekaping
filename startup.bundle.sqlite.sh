#!/bin/sh
set -e

# Create env.js file for the web app
API_URL=${API_URL:-}
cat >/app/web/env.js <<EOF
/* generated each container start */
window.__CONFIG__ = {
  API_URL: "$API_URL"
};
EOF

# Set environment variables for SQLite
export DB_TYPE=sqlite
export DB_NAME=/app/data/peekaping.db

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
