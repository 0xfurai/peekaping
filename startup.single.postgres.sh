#!/bin/bash
# The PostgreSQL bundle's startup script (Dockerfile.single.postgres), run by tini.
#
# It runs PostgreSQL and peekaping side by side. As root, it sets up the peekaping user
# (PUID/PGID and the Docker socket's group), initializes the cluster on the first start and
# runs PostgreSQL as postgres, then runs the migrations and peekaping as peekaping. Started
# with docker run --user, it runs everything as the given user, who must own the data
# directory.
#
# On SIGTERM (docker stop) or SIGINT (Ctrl+C under docker run -it), it stops peekaping, then
# PostgreSQL, and exits 0 once both have stopped cleanly. If either exits on its own, or a step
# fails while PostgreSQL runs, it stops what still runs and exits non-zero.

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
app_stopping=
stopping=
stop_requested=

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

# stop_app: sends peekaping SIGTERM, once: a second one makes it exit without finishing its
# shutdown
stop_app() {
    if [ -n "$app_pid" ] && [ -z "$app_stopping" ]; then
        app_stopping=1
        echo "Stopping peekaping..."
        kill -TERM "$app_pid" 2>/dev/null || true
    fi
}

# shutdown <exit code>: stops peekaping, then PostgreSQL, and exits. The code is raised to 1
# if either doesn't stop cleanly.
shutdown() {
    local code=$1
    stopping=1

    if [ -n "$app_pid" ] && kill -0 "$app_pid" 2>/dev/null; then
        stop_app
        reap "$app_pid"
        app_pid=
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

# A signal starts the shutdown in the handler: peekaping, once it runs, gets SIGTERM at once,
# so the wait below returns whenever the signal comes. The steps check stop_requested and
# finish the shutdown outside the handler: bash re-raises the signal on itself when a handler
# waits for a process. bash runs the handler once the command in progress, such as the
# migrations, has finished.
on_signal() {
    stop_requested=1
    stop_app
}
trap on_signal TERM INT

# stop_if_requested: finishes the shutdown if a signal came
stop_if_requested() {
    if [ -n "$stop_requested" ]; then
        shutdown 0
    fi
}

# Any other exit, such as a failed step under set -e, stops what still runs, so PostgreSQL
# shuts down cleanly rather than being killed with the container
on_exit() {
    local code=$?
    if [ -z "$stopping" ] && [ -n "$pg_pid$app_pid" ]; then
        shutdown "$code"
    fi
}
trap on_exit EXIT

# spawn <variable> <command...>: starts the command in the background and stores its PID in the
# variable, unless a signal came. The command runs in a session of its own: Ctrl+C under
# docker run -it sends SIGINT to the terminal's whole foreground process group, and only this
# script may take it, so that peekaping stops before PostgreSQL and takes a single signal.
spawn() {
    local variable=$1
    shift
    stop_if_requested
    setsid "$@" &
    printf -v "$variable" %s "$!"
    # A signal before the PID was stored
    stop_if_requested
}

if [ "$(id -u)" = 0 ]; then
    # The peekaping user: PUID/PGID, the Docker socket and /app/data. First, so that a bad
    # PUID or PGID stops the container before PostgreSQL starts.
    # shellcheck source=startup.single.user.sh
    . /app/startup.user.sh
fi

# The data directory
if [ "$(id -u)" = 0 ]; then
    mkdir -p "$PGDATA"
    chown -R postgres:postgres "$PGDATA"
    chmod 700 "$PGDATA"
elif [ ! -w "$PGDATA" ]; then
    echo "ERROR: $PGDATA isn't writable by $(id -u):$(id -g). Give the volume to that user, or start the container without --user."
    exit 1
fi

# The setup runs until the marker is written. initdb runs only where there's no cluster yet
# (no PG_VERSION): whatever is left in such a directory is cleared first. A cluster whose
# setup didn't finish, such as after a failed first start, keeps its files, and the setup runs
# on it again.
setup_pending=false
if [ ! -f "$initialized_marker" ]; then
    setup_pending=true
    if [ ! -f "$PGDATA/PG_VERSION" ]; then
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
    else
        echo "Finishing the PostgreSQL setup that an earlier start didn't finish..."
    fi
fi

echo "Starting PostgreSQL..."
spawn pg_pid "${as_postgres[@]}" postgres -D "$PGDATA" -p "$DB_PORT"

echo "Waiting for PostgreSQL to accept connections..."
until pg_isready -q -p "$DB_PORT" -U postgres; do
    stop_if_requested
    if ! kill -0 "$pg_pid" 2>/dev/null; then
        reap "$pg_pid"
        pg_pid=
        echo "ERROR: PostgreSQL exited with $reaped before it accepted connections"
        if [ "$setup_pending" = true ]; then
            echo "ERROR: $PGDATA holds a cluster whose setup never finished. If it holds no data, empty it and start again."
        fi
        exit 1
    fi
    sleep 1
done
stop_if_requested

if [ "$setup_pending" = true ]; then
    # Each step can run again: the role and the database are created only if missing, so
    # PostgreSQL's own (DB_USER=postgres, DB_NAME=postgres) are kept as they are
    echo "Creating the database and its user..."
    if ! "${as_postgres[@]}" psql -q -v ON_ERROR_STOP=1 -p "$DB_PORT" -U postgres -d postgres \
        -v db_user="$DB_USER" -v db_pass="$DB_PASS" -v db_name="$DB_NAME" <<'SQL'
SELECT format('CREATE USER %I WITH PASSWORD %L', :'db_user', :'db_pass')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'db_user') \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'db_name', :'db_user')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'db_name') \gexec
GRANT CONNECT ON DATABASE :"db_name" TO :"db_user";
\connect :"db_name"
GRANT USAGE ON SCHEMA public TO :"db_user";
GRANT CREATE ON SCHEMA public TO :"db_user";
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO :"db_user";
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO :"db_user";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO :"db_user";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO :"db_user";
SQL
    then
        stop_if_requested
        echo "ERROR: Couldn't create the database and its user. The next start tries again."
        shutdown 1
    fi
    "${as_postgres[@]}" touch "$initialized_marker"
    echo "PostgreSQL initialization completed!"
fi
stop_if_requested

echo "Running database migrations..."
if ! "${as_peekaping[@]}" ./run-migrations.sh; then
    # Ctrl+C reaches the migrations too
    stop_if_requested
    echo "ERROR: Migrations failed"
    shutdown 1
fi

spawn app_pid "${as_peekaping[@]}" /app/server/peekaping

# Until a signal comes, or one of them exits. on_signal has sent peekaping SIGTERM by the time
# the wait returns on a signal, or peekaping exits on it.
while :; do
    stop_if_requested
    status=0
    wait -n -p exited "$pg_pid" "$app_pid" || status=$?
    # wait unsets exited when a signal interrupts it
    exited=${exited:-}
    if [ "$exited" = "$app_pid" ]; then
        app_pid=
        if [ -n "$stop_requested" ]; then
            # It stopped on the signal
            if [ "$status" != 0 ]; then
                echo "peekaping exited with $status"
                shutdown 1
            fi
            shutdown 0
        fi
        echo "peekaping exited with $status, stopping PostgreSQL"
        if [ "$status" = 0 ]; then status=1; fi
        shutdown "$status"
    elif [ "$exited" = "$pg_pid" ]; then
        echo "PostgreSQL exited with $status, stopping peekaping"
        pg_pid=
        if [ "$status" = 0 ]; then status=1; fi
        shutdown "$status"
    fi
done
