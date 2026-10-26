#!/bin/sh
# The single image's startup script (Dockerfile.single), run by tini.
#
# As root, it sets up the peekaping user (PUID/PGID, the Docker socket's group and /app/data),
# then runs the migrations and peekaping as that user. Started with docker run --user, it
# skips that setup and runs both as the given user, who must be able to write /app/data
# when the database is SQLite.
#
# DB_TYPE picks the database. Unset or sqlite, it is the file /app/data/peekaping.db. postgres,
# it is the PostgreSQL server that DB_HOST, DB_PORT, DB_USER, DB_PASS and DB_NAME name, which
# runs in a container of its own (docker-compose.single.postgres.yml) or anywhere else.
set -e

case "${DB_TYPE:-sqlite}" in
    sqlite)
        # The database is always /app/data/peekaping.db, whatever DB_NAME an .env shared with
        # cluster mode sets
        export DB_TYPE=sqlite
        export DB_NAME=/app/data/peekaping.db
        ;;
    postgres)
        if [ -z "${DB_HOST:-}" ]; then
            echo "ERROR: DB_HOST is required with DB_TYPE=postgres: set it to the PostgreSQL server's host name"
            exit 1
        fi
        export DB_PORT="${DB_PORT:-5432}"
        ;;
    *)
        echo "ERROR: DB_TYPE=$DB_TYPE isn't supported in single mode: use sqlite (the default) or postgres"
        exit 1
        ;;
esac

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
    if [ "$DB_TYPE" = sqlite ] && [ ! -w /app/data ]; then
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
