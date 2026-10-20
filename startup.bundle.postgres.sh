#!/bin/bash
# The PostgreSQL bundle's startup script (Dockerfile.bundle.postgres), run by tini.
#
# It runs PostgreSQL and peekaping side by side. As root, it initializes the cluster on the
# first start and runs PostgreSQL as postgres; it sets up the peekaping user (PUID/PGID and the
# Docker socket's group), then runs the migrations and peekaping as that user. Started with
# docker run --user, it runs everything as the given user, who must own the data directory.
#
# On SIGTERM or SIGINT (docker stop), it stops peekaping, then PostgreSQL, and exits 0 once
# both have stopped cleanly. If either exits on its own, it stops the other and exits non-zero.

# No pipefail: startup.user.sh runs `getent ... | cut`, where getent fails for a free ID.
set -eu

: "${PGDATA:=/var/lib/postgresql/data}"
initialized_marker="$PGDATA/.postgres_initialized"

errors=0
if [ -z "${DB_USER:-}" ]; then
    echo "ERROR: DB_USER is required and must be set"
    errors=1
fi
if [ -z "${DB_PASS:-}" ]; then
    echo "ERROR: DB_PASS is required and must be set"
    errors=1
fi
if [ "$errors" -eq 1 ]; then
    echo "Environment validation failed. Please fix the above errors."
    exit 1
fi

export DB_TYPE="${DB_TYPE:-postgres}"
export DB_HOST="${DB_HOST:-localhost}"
export DB_PORT="${DB_PORT:-5432}"
export DB_NAME="${DB_NAME:-peekaping}"

# peekaping serves the web app, the API and the websocket on port 8383, whatever SERVER_PORT
# the .env sets: before single mode, the API listened on SERVER_PORT behind Caddy on 8383.
export SERVER_PORT=8383
export WEB_DIR=/app/web
export CLIENT_URL="${CLIENT_URL:-http://localhost:8383}"
export MODE="${MODE:-prod}"
export TZ="${TZ:-UTC}"

cd /app/server

# How PostgreSQL and peekaping are run: as their users under root, as the given user otherwise
if [ "$(id -u)" = 0 ]; then
    as_postgres=(gosu postgres)
    as_peekaping=(gosu peekaping)
else
    as_postgres=()
    as_peekaping=()
    echo "Running as $(id -u):$(id -g) (docker run --user), so PUID, PGID and the Docker socket's group don't apply"
fi

pg_pid=
app_pid=

# The signal handler only takes note. The steps below check for it, since bash runs the
# handler only once the command in progress, such as the migrations, has finished.
stop_requested=
trap 'stop_requested=1' TERM INT

# reap <pid>: waits for the process to exit, through any signal that interrupts the wait, and
# sets reaped to its exit status
reap() {
    local status
    while :; do
        status=0
        wait "$1" || status=$?
        if ! kill -0 "$1" 2>/dev/null; then
            reaped=$status
            return
        fi
    done
}

# shutdown <exit code>: stops peekaping, then PostgreSQL, and exits. The code is raised to 1
# if either doesn't stop cleanly.
shutdown() {
    local code=$1

    if [ -n "$app_pid" ] && kill -0 "$app_pid" 2>/dev/null; then
        echo "Stopping peekaping..."
        kill -TERM "$app_pid" 2>/dev/null || true
        reap "$app_pid"
        if [ "$reaped" != 0 ]; then
            echo "peekaping exited with $reaped"
            [ "$code" != 0 ] || code=1
        fi
    fi

    if [ -n "$pg_pid" ] && kill -0 "$pg_pid" 2>/dev/null; then
        echo "Stopping PostgreSQL..."
        if ! "${as_postgres[@]}" pg_ctl stop -D "$PGDATA" -m fast -w -t 60; then
            kill -INT "$pg_pid" 2>/dev/null || true
        fi
        reap "$pg_pid"
        if [ "$reaped" != 0 ]; then
            echo "PostgreSQL exited with $reaped"
            [ "$code" != 0 ] || code=1
        fi
    fi

    echo "Stopped"
    exit "$code"
}

# stop_if_requested: shuts down cleanly if a signal came during the last step
stop_if_requested() {
    if [ -n "$stop_requested" ]; then
        shutdown 0
    fi
}

# The data directory
if [ "$(id -u)" = 0 ]; then
    mkdir -p "$PGDATA"
    chown -R postgres:postgres "$PGDATA"
    chmod 700 "$PGDATA"
elif [ ! -w "$PGDATA" ]; then
    echo "ERROR: $PGDATA isn't writable by $(id -u):$(id -g). Give the volume to that user, or start the container without --user."
    exit 1
fi

# Initialize the cluster on the first start. A directory without the marker holds no finished
# initialization, so it is cleared first.
fresh=false
if [ ! -f "$initialized_marker" ]; then
    echo "Initializing PostgreSQL..."
    rm -rf "${PGDATA:?}"/*
    if [ "$(id -u)" != 0 ] && ! getent passwd "$(id -u)" >/dev/null; then
        # initdb needs its user's name, and docker run --user can give a UID with none
        nss_dir=$(mktemp -d)
        echo "postgres:x:$(id -u):$(id -g):PostgreSQL:$PGDATA:/bin/false" >"$nss_dir/passwd"
        echo "postgres:x:$(id -g):" >"$nss_dir/group"
        LD_PRELOAD=libnss_wrapper.so NSS_WRAPPER_PASSWD="$nss_dir/passwd" NSS_WRAPPER_GROUP="$nss_dir/group" \
            initdb -D "$PGDATA" --username=postgres
        rm -rf "$nss_dir"
    else
        "${as_postgres[@]}" initdb -D "$PGDATA" --username=postgres
    fi
    fresh=true
fi
stop_if_requested

echo "Starting PostgreSQL..."
"${as_postgres[@]}" postgres -D "$PGDATA" -p "$DB_PORT" &
pg_pid=$!

echo "Waiting for PostgreSQL to accept connections..."
until pg_isready -q -p "$DB_PORT" -U postgres; do
    stop_if_requested
    if ! kill -0 "$pg_pid" 2>/dev/null; then
        reap "$pg_pid"
        echo "ERROR: PostgreSQL exited with $reaped before it accepted connections"
        exit 1
    fi
    sleep 1
done
stop_if_requested

if [ "$fresh" = true ]; then
    echo "Creating the database and its user..."
    "${as_postgres[@]}" psql -q -v ON_ERROR_STOP=1 -p "$DB_PORT" -U postgres -d postgres \
        -v db_user="$DB_USER" -v db_pass="$DB_PASS" -v db_name="$DB_NAME" <<'SQL'
CREATE USER :"db_user" WITH PASSWORD :'db_pass';
CREATE DATABASE :"db_name" OWNER :"db_user";
GRANT CONNECT ON DATABASE :"db_name" TO :"db_user";
\connect :"db_name"
GRANT USAGE ON SCHEMA public TO :"db_user";
GRANT CREATE ON SCHEMA public TO :"db_user";
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO :"db_user";
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO :"db_user";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO :"db_user";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO :"db_user";
SQL
    "${as_postgres[@]}" touch "$initialized_marker"
    echo "PostgreSQL initialization completed!"
fi

if [ "$(id -u)" = 0 ]; then
    # The peekaping user: PUID/PGID, the Docker socket and /app/data
    # shellcheck source=startup.bundle.user.sh
    . /app/startup.user.sh
fi
stop_if_requested

echo "Running database migrations..."
if ! "${as_peekaping[@]}" ./run-migrations.sh; then
    echo "ERROR: Migrations failed"
    shutdown 1
fi
stop_if_requested

"${as_peekaping[@]}" /app/server/peekaping &
app_pid=$!

# Until a signal comes, or one of them exits
while :; do
    exited=
    status=0
    wait -n -p exited "$pg_pid" "$app_pid" || status=$?
    stop_if_requested
    if [ "$exited" = "$app_pid" ]; then
        echo "peekaping exited with $status, stopping PostgreSQL"
        app_pid=
        if [ "$status" = 0 ]; then status=1; fi
        shutdown "$status"
    elif [ "$exited" = "$pg_pid" ]; then
        echo "PostgreSQL exited with $status, stopping peekaping"
        pg_pid=
        if [ "$status" = 0 ]; then status=1; fi
        shutdown "$status"
    fi
done
