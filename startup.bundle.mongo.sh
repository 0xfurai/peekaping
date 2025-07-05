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

# Set default environment variables if not provided
export DB_TYPE=${DB_TYPE:-mongodb}
export DB_HOST=${DB_HOST:-localhost}
export DB_PORT=${DB_PORT:-27017}
export DB_NAME=${DB_NAME:-peekaping}
export DB_USER=${DB_USER:-peekaping}
export DB_PASS=${DB_PASS:-peekaping}

# Set server configuration environment variables
export PORT=${PORT:-8034}
export CLIENT_URL=${CLIENT_URL:-http://localhost:8383}
export ACCESS_TOKEN_SECRET_KEY=${ACCESS_TOKEN_SECRET_KEY:-your-access-token-secret-key-change-this-in-production}
export REFRESH_TOKEN_SECRET_KEY=${REFRESH_TOKEN_SECRET_KEY:-your-refresh-token-secret-key-change-this-in-production}
export ACCESS_TOKEN_EXPIRED_IN=${ACCESS_TOKEN_EXPIRED_IN:-15m}
export REFRESH_TOKEN_EXPIRED_IN=${REFRESH_TOKEN_EXPIRED_IN:-168h}
export MODE=${MODE:-prod}
export TZ=${TZ:-UTC}

# Create .env file for the server
cat > /app/.env << EOF
PORT=$PORT
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

# Create data directory if it doesn't exist
mkdir -p /data/db

# Create log directory and fix permissions
mkdir -p /var/log/supervisor
chmod 755 /var/log/supervisor

# Initialize MongoDB if needed
if [ ! -f /data/db/.mongodb_initialized ]; then
    echo "Initializing MongoDB..."

    # Start MongoDB without auth temporarily
    mongod --dbpath /data/db --fork --logpath /var/log/supervisor/mongodb-init.log

    # Wait for MongoDB to be ready
    echo "Waiting for MongoDB to be ready..."
    sleep 5

    # Create admin user in admin database using mongosh (MongoDB Shell)
    mongosh admin --eval "
        db.createUser({
            user: 'admin',
            pwd: '$DB_PASS',
            roles: ['root']
        });
    "

    # Create application user in admin database (not target database)
    mongosh admin --eval "
        db.createUser({
            user: '$DB_USER',
            pwd: '$DB_PASS',
            roles: [
                { role: 'readWrite', db: '$DB_NAME' },
                { role: 'dbAdmin', db: '$DB_NAME' }
            ]
        });
    " --authenticationDatabase admin -u admin -p "$DB_PASS"

    # Stop MongoDB
    mongod --dbpath /data/db --shutdown

    # Mark as initialized
    touch /data/db/.mongodb_initialized
    echo "MongoDB initialization completed!"
fi

# Give MongoDB a moment to fully start up
echo "Waiting for MongoDB to be fully ready..."
sleep 3

# Run database migrations
echo "Running database migrations..."
cd /app/server
if ./run-migrations.sh; then
    echo "Migrations completed successfully!"
else
    echo "Migration failed!"
    exit 1
fi

# Start supervisor to manage MongoDB, server, and Caddy
echo "Starting supervisor to manage MongoDB, server, and Caddy..."
exec /usr/bin/supervisord -c /etc/supervisor/conf.d/supervisord.conf
