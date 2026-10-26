#!/bin/sh
# The SQLite bundle's startup script (Dockerfile.single.sqlite), run by tini.
#
# As root, it sets up the peekaping user (PUID/PGID, the Docker socket's group and /app/data),
# then runs the migrations and peekaping as that user. Started with docker run --user, it
# skips that setup and runs both as the given user, who must be able to write /app/data.
set -e

# The database is always /app/data/peekaping.db
export DB_TYPE=sqlite
export DB_NAME=/app/data/peekaping.db

# peekaping serves the web app, the API and the websocket on port 8383, whatever SERVER_PORT
# the .env sets: before single mode, the API listened on SERVER_PORT behind Caddy on 8383.
export SERVER_PORT=8383
export WEB_DIR=/app/web
export CLIENT_URL="${CLIENT_URL:-http://localhost:8383}"
export MODE="${MODE:-prod}"
export TZ="${TZ:-UTC}"

cd /app/server

if [ "$(id -u)" = 0 ]; then
    # shellcheck source=startup.single.user.sh
    . /app/startup.user.sh
    set -- su-exec peekaping
else
    echo "Running as $(id -u):$(id -g) (docker run --user), so PUID, PGID and the Docker socket's group don't apply"
    if [ ! -w /app/data ]; then
        echo "ERROR: /app/data isn't writable by $(id -u):$(id -g). Give the volume to that user, or start the container without --user."
        exit 1
    fi
    set --
fi

echo "Running database migrations..."
if ! "$@" ./run-migrations.sh; then
    echo "ERROR: Migrations failed"
    exit 1
fi

exec "$@" /app/server/peekaping
