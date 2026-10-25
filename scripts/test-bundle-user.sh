#!/usr/bin/env bash
# Smoke-tests how a bundle image runs: which user Peekaping runs as, what it can
# write, PUID/PGID, the Docker socket, the startup warnings and that port 8383
# sends the probe protocol (/probe/v1/) to the API. For the
# single-mode bundles (SQLite and PostgreSQL) it also checks that one peekaping
# process serves port 8383 whatever SERVER_PORT says, that ping and Docker
# monitors work as non-root, that docker run --user works, and that docker stop
# and Ctrl+C shut down in order and exit 0. For the PostgreSQL bundle it checks
# PostgreSQL's own role and database as DB_USER and DB_NAME, and a first start
# that fails once PostgreSQL runs.
#
# Usage: scripts/test-bundle-user.sh <sqlite|postgres> [image]
# Without an image, it builds Dockerfile.bundle.<db> from the repo root. Set
# BUNDLE_TEST_PREFIX to name the test containers something else.
# Needs docker, curl and jq.
set -u

db=${1:?usage: $0 <sqlite|postgres> [image]}
image=${2:-}
root=$(cd "$(dirname "$0")/.." && pwd)
prefix=${BUNDLE_TEST_PREFIX:-peekaping-bundle-test-$db}
socket=/var/run/docker.sock
failures=0

case "$db" in
  sqlite | postgres) ;;
  *)
    echo "usage: $0 <sqlite|postgres> [image]"
    exit 2
    ;;
esac

if [ -z "$image" ]; then
  image="$prefix:local"
  echo "Building $image..."
  docker build -q -f "$root/Dockerfile.bundle.$db" -t "$image" "$root" >/dev/null || exit 1
fi

containers="default ids bad-uid bad-gid taken-gid shadow-gid free-ids socket socket-maker ro-data ping user ctrl-c
  pg-superuser pg-database pg-reserved"
cleanup() {
  for name in $containers; do
    docker rm -f "$prefix-$name" >/dev/null 2>&1
  done
  docker volume rm "$prefix-socket" >/dev/null 2>&1
}
trap cleanup EXIT
cleanup

# check <description> <command...>: runs the command, records the result and
# returns it, so that `if check ...` skips the checks that depend on it.
check() {
  if "${@:2}"; then
    echo "ok   - $1"
  else
    echo "FAIL - $1"
    failures=$((failures + 1))
    return 1
  fi
}

eq() {
  [ "$1" = "$2" ] && return 0
  echo "       expected '$2', got '$1'"
  return 1
}

ne() {
  [ "$1" != "$2" ] && return 0
  echo "       expected anything but '$2'"
  return 1
}

# cant_write <container> <path>: the path exists, and the peekaping user can't write it.
cant_write() {
  docker exec "$1" test -e "$2" || { echo "       no $2"; return 1; }
  docker exec -u peekaping "$1" test ! -w "$2"
}

# start <name> [docker run args...]: port 8383 goes to a free host port.
start() {
  # The usage ping and error reports are on by default, and a test must not send either to
  # Peekaping: both point at an address nothing listens on
  docker run -d --name "$1" -p 127.0.0.1::8383 -e DB_USER=peekaping -e DB_PASS=bundle-test \
    -e TELEMETRY_URL=http://127.0.0.1:9/v1/ping -e ERROR_REPORTING_DSN=http://public@127.0.0.1:9/1 \
    "${@:2}" "$image" >/dev/null
}

# url <container>: where the host reaches the container's port 8383
url() { echo "http://$(docker port "$1" 8383/tcp | head -n 1)"; }

wait_healthy() {
  for _ in $(seq 1 180); do
    if [ "$(docker inspect -f '{{.State.Running}}' "$1")" != true ]; then
      break
    fi
    curl -sf -o /dev/null "$(url "$1")/api/v1/health" 2>/dev/null && return 0
    sleep 1
  done
  echo "       $1 didn't become healthy; last log lines:"
  docker logs --tail 15 "$1" 2>&1 | sed 's/^/       /'
  return 1
}

wait_exited() {
  for _ in $(seq 1 180); do
    [ "$(docker inspect -f '{{.State.Running}}' "$1")" = false ] && return 0
    sleep 1
  done
  return 1
}

exit_code() { docker inspect -f '{{.State.ExitCode}}' "$1"; }

# ctrl_c <container>: what Ctrl+C under docker run -it does. The terminal sends
# SIGINT to its foreground process group, every process in it at once; tini
# makes that the startup script's group. Field 8 of /proc/1/stat is that group.
# `kill -INT -<group>` is the form both dash and busybox take: after `-s INT`,
# dash reads the group as an option, and busybox's kill takes no `--`.
ctrl_c() {
  docker exec "$1" sh -c 'kill -INT "-$(awk "{print \$8}" /proc/1/stat)"'
}

# logs_count <container> <pattern>: how many log lines match
logs_count() { docker logs "$1" 2>&1 | grep -c "$2"; }

# pids <container> <process name>: the container's processes with that name
pids() {
  docker exec "$1" sh -c 'for p in /proc/[0-9]*; do
    [ "$(cat "$p/comm" 2>/dev/null)" = "$0" ] && echo "${p#/proc/}"
  done' "$2"
}

# ids <container> <process name>: "<uid>:<gid>" of the first process with that name.
ids() {
  docker exec "$1" sh -c 'for p in /proc/[0-9]*; do
    [ "$(cat "$p/comm" 2>/dev/null)" = "$0" ] || continue
    uid=$(sed -n "s/^Uid:\t\([0-9]*\).*/\1/p" "$p/status")
    gid=$(sed -n "s/^Gid:\t\([0-9]*\).*/\1/p" "$p/status")
    echo "$uid:$gid"; exit
  done' "$2"
}

groups_of() { # groups_of <container> <process name>: supplementary groups of that process
  docker exec "$1" sh -c 'for p in /proc/[0-9]*; do
    [ "$(cat "$p/comm" 2>/dev/null)" = "$0" ] || continue
    sed -n "s/^Groups:\t*//p" "$p/status"; exit
  done' "$2"
}

owner() { docker exec "$1" stat -c %u:%g "$2"; }

has_word() { [[ " $1 " == *" $2 "* ]] || { echo "       '$2' not in '$1'"; return 1; }; }

runs_none() { # runs_none <container> <process names...>
  local found=
  for proc in "${@:2}"; do
    [ -z "$(pids "$1" "$proc")" ] || found="$found $proc"
  done
  [ -z "$found" ] || { echo "       running:$found"; return 1; }
}

logs_mention() { docker logs "$1" 2>&1 | grep -q "$2" || { echo "       no '$2' in the logs"; return 1; }; }

logs_lack() {
  if docker logs "$1" 2>&1 | grep -q "$2"; then
    echo "       '$2' in the logs"
    return 1
  fi
}

# logs_in_order <container> <pattern...>: each pattern is logged after the one before it
logs_in_order() {
  local logs last=0 line
  logs=$(docker logs "$1" 2>&1)
  for pattern in "${@:2}"; do
    line=$(echo "$logs" | grep -n "$pattern" | tail -n 1 | cut -d: -f1)
    if [ -z "$line" ] || [ "$line" -le "$last" ]; then
      echo "       '$pattern' isn't logged after the line before"
      return 1
    fi
    last=$line
  done
}

serves() { # serves <container> <path> <text>: port 8383 answers 200 with the text in the body
  local body
  body=$(curl -sf "$(url "$1")$2") || { echo "       GET $2 failed"; return 1; }
  [[ "$body" == *"$3"* ]] || { echo "       GET $2 has no '$3'"; return 1; }
}

# answers_probe_with_json_error <container>: POST /probe/v1/hello without a token on port
# 8383 reaches the API (401 and a JSON body), not the web app that serves every other path
answers_probe_with_json_error() {
  local out status body
  out=$(curl -sS -X POST -H "Content-Type: application/json" --data '{}' -w '\n%{http_code}' "$(url "$1")/probe/v1/hello") ||
    { echo "       POST /probe/v1/hello failed"; return 1; }
  status=${out##*$'\n'}
  body=${out%$'\n'*}
  eq "$status" 401 || { echo "       body: $body" | head -c 300; echo; return 1; }
  jq -e . >/dev/null 2>&1 <<<"$body" || { echo "       the body isn't JSON: $body" | head -c 300; echo; return 1; }
}

# The API, as the e2e admin. login <container> sets token.
api() { # api <container> <method> <path> [json body]
  local args=(-sS -X "$2" -H "Content-Type: application/json")
  [ -n "${token:-}" ] && args+=(-H "Authorization: Bearer $token")
  [ $# -ge 4 ] && args+=(--data "$4")
  curl "${args[@]}" "$(url "$1")/api/v1/$3"
}

login() {
  local credentials='{"email":"test@test.com","password":"TestPassword1234!"}'
  token=
  token=$(api "$1" POST auth/register "$credentials" | jq -r '.data.accessToken // empty')
  [ -n "$token" ] || token=$(api "$1" POST auth/login "$credentials" | jq -r '.data.accessToken // empty')
  [ -n "$token" ] || { echo "       couldn't register or log in"; return 1; }
}

# monitor_up <container> <type> <config json>: creates a monitor checked every
# 20 seconds and waits for it to be Up
monitor_up() {
  local body id status
  body=$(jq -n --arg type "$2" --arg config "$3" '{type: $type, name: ("bundle test " + $type), interval: 20,
    timeout: 16, max_retries: 0, retry_interval: 20, resend_interval: 0, active: true, notification_ids: [],
    config: $config}')
  id=$(api "$1" POST monitors "$body" | jq -r '.data.id // empty')
  [ -n "$id" ] || { echo "       couldn't create the $2 monitor"; return 1; }
  for _ in $(seq 1 60); do
    status=$(api "$1" GET "monitors/$id" | jq -r '.data.status')
    [ "$status" = 1 ] && return 0
    sleep 1
  done
  echo "       the $2 monitor isn't Up (status $status); its heartbeats:"
  api "$1" GET "monitors/$id/heartbeats?limit=1" | sed 's/^/       /'
  return 1
}

root_procs="tini"
# Nothing of the old bundle may run
gone_procs="supervisord caddy redis-server api"
case "$db" in
  postgres) db_proc=postgres db_user=postgres ;;
  *) db_proc= ;;
esac

echo "# Default run"
socket_args=()
[ -S "$socket" ] && socket_args=(-v "$socket:$socket")
# SERVER_PORT and REDIS_HOST as a cluster-mode .env sets them: single mode
# serves port 8383 regardless, and ignores Redis.
# The ${a[@]+...} form keeps bash 3.2 (macOS) from failing on an empty array under set -u.
start "$prefix-default" -e SERVER_PORT=8034 -e REDIS_HOST=redis ${socket_args[@]+"${socket_args[@]}"}
if check "starts and reports healthy" wait_healthy "$prefix-default"; then
  check "peekaping runs as 1000:1000" eq "$(ids "$prefix-default" peekaping)" 1000:1000
  for proc in $root_procs; do
    check "$proc keeps running as root" eq "$(ids "$prefix-default" "$proc" | cut -d: -f1)" 0
  done
  # shellcheck disable=SC2086 # a list of names
  check "runs no supervisord, Caddy, Redis or cluster-mode services" runs_none "$prefix-default" $gone_procs
  if [ -n "$db_proc" ]; then
    check "$db_proc keeps running as $db_user" eq \
      "$(ids "$prefix-default" "$db_proc" | cut -d: -f1)" "$(docker exec "$prefix-default" id -u "$db_user")"
  fi
  check "peekaping can't write its binary" cant_write "$prefix-default" /app/server/peekaping
  check "peekaping can't write the web assets" cant_write "$prefix-default" /app/web/index.html
  check "peekaping owns /app/data" eq "$(owner "$prefix-default" /app/data)" 1000:1000
  if [ "$db" = sqlite ]; then
    check "peekaping owns the SQLite database" eq "$(owner "$prefix-default" /app/data/peekaping.db)" 1000:1000
  fi
  check "no ping warning on default Docker" logs_lack "$prefix-default" ping_group_range
  # Remote probes reach the API through port 8383, whichever gateway the bundle runs
  check "port 8383 sends POST /probe/v1/hello to the API: 401 without a token, in JSON" \
    answers_probe_with_json_error "$prefix-default"
  check "port 8383 serves the web app" serves "$prefix-default" / '<div id="root">'
  check "port 8383 serves a client-side route with the web app" serves "$prefix-default" /monitors '<div id="root">'
  check "port 8383 serves /env.js with the API on the same origin" serves "$prefix-default" /env.js 'API_URL: ""'
  check "port 8383 serves the API" serves "$prefix-default" /api/v1/health '"status":"success"'
  check "port 8383 serves the websocket" serves "$prefix-default" '/socket.io/?EIO=4&transport=polling' '"sid"'
  check "peekaping listens on 8383, not on SERVER_PORT=8034" logs_mention "$prefix-default" '"Serving the API".*:8383'
  check "the API is reachable as the e2e admin" login "$prefix-default"
  check "a ping monitor to 127.0.0.1 is Up as non-root" monitor_up "$prefix-default" ping \
    '{"host":"127.0.0.1","packet_size":32}'
  if [ -S "$socket" ]; then
    socket_gid=$(docker exec "$prefix-default" stat -c %g "$socket")
    check "peekaping has the Docker socket's group ($socket_gid)" has_word "$(groups_of "$prefix-default" peekaping)" "$socket_gid"
    check "a Docker monitor on its own container is Up" monitor_up "$prefix-default" docker \
      "{\"container_id\":\"$prefix-default\",\"connection_type\":\"socket\",\"docker_daemon\":\"$socket\"}"
    check "no Docker socket warning" logs_lack "$prefix-default" "Docker monitors can't use"
  else
    echo "skip - no $socket on this host, so the Docker socket checks didn't run"
  fi
  docker stop -t 40 "$prefix-default" >/dev/null
  check "docker stop exits 0" eq "$(docker inspect -f '{{.State.ExitCode}}' "$prefix-default")" 0
  if [ "$db" = postgres ]; then
    check "docker stop stops peekaping, then PostgreSQL" logs_in_order "$prefix-default" \
      "Shutdown signal received" "Closed the database" "Peekaping stopped" "Stopping PostgreSQL" \
      "database system is shut down"
  else
    check "docker stop shuts peekaping down in order" logs_in_order "$prefix-default" \
      "Shutdown signal received" "Stopped the HTTP server" "Closed the event bus" "Closed the database" \
      "Peekaping stopped"
  fi
fi
docker rm -f "$prefix-default" >/dev/null

echo "# PUID=1500 PGID=1600"
start "$prefix-ids" -e PUID=1500 -e PGID=1600
if check "starts and reports healthy" wait_healthy "$prefix-ids"; then
  check "peekaping runs as 1500:1600" eq "$(ids "$prefix-ids" peekaping)" 1500:1600
  check "1500:1600 owns /app/data" eq "$(owner "$prefix-ids" /app/data)" 1500:1600
  if [ "$db" = sqlite ]; then
    check "1500:1600 owns the SQLite database" eq "$(owner "$prefix-ids" /app/data/peekaping.db)" 1500:1600
  fi
fi
docker rm -f "$prefix-ids" >/dev/null

echo "# Unusable IDs stop the container"
start "$prefix-bad-uid" -e PUID=0
check "PUID=0 (root's UID) stops the container" wait_exited "$prefix-bad-uid"
check "PUID=0 exits non-zero" ne "$(docker inspect -f '{{.State.ExitCode}}' "$prefix-bad-uid")" 0
check "PUID=0 is named in the error" logs_mention "$prefix-bad-uid" "PUID 0"
if [ "$db" = postgres ]; then
  # Else PostgreSQL would be killed with the container rather than stopped
  check "PUID=0 stops it before PostgreSQL starts" logs_lack "$prefix-bad-uid" "Starting PostgreSQL"
fi
start "$prefix-bad-gid" -e PGID=staff
check "PGID=staff (not a number) stops the container" wait_exited "$prefix-bad-gid"
check "PGID=staff exits non-zero" ne "$(docker inspect -f '{{.State.ExitCode}}' "$prefix-bad-gid")" 0
check "PGID=staff is named in the error" logs_mention "$prefix-bad-gid" "PGID must be a number"
start "$prefix-taken-gid" -e PGID=0
check "PGID=0 (root's group) stops the container" wait_exited "$prefix-taken-gid"
check "PGID=0 exits non-zero" ne "$(docker inspect -f '{{.State.ExitCode}}' "$prefix-taken-gid")" 0
check "PGID=0 is named in the error" logs_mention "$prefix-taken-gid" "PGID 0 is already used"
start "$prefix-shadow-gid" -e PGID=42
check "PGID=42 (the shadow group, which can read /etc/shadow) stops the container" wait_exited "$prefix-shadow-gid"
check "PGID=42 is named in the error" logs_mention "$prefix-shadow-gid" "PGID 42 is already used"

echo "# PUID=405 PGID=999, IDs Alpine gives users and groups nothing in the image uses"
# Alpine's guest user and ping group, which nothing in the image uses. Debian,
# which earlier bundles and the PostgreSQL bundle run on, has neither ID.
start "$prefix-free-ids" -e PUID=405 -e PGID=999
if check "starts and reports healthy" wait_healthy "$prefix-free-ids"; then
  check "peekaping runs as 405:999" eq "$(ids "$prefix-free-ids" peekaping)" 405:999
fi
docker rm -f "$prefix-free-ids" >/dev/null

echo "# A Docker socket its group can't use"
# A stand-in socket, mode 600, so the user's new group gives no access.
if [ "$db" = sqlite ]; then
  # Alpine has no tool that listens on a Unix socket, so a Node container makes
  # it in a volume that becomes the bundle's /run (and so /var/run).
  docker volume create "$prefix-socket" >/dev/null
  docker run -d --name "$prefix-socket-maker" -v "$prefix-socket:/run" node:22-alpine node -e \
    "require('net').createServer().listen('$socket', () => require('fs').chmodSync('$socket', 0o600))" >/dev/null
  for _ in $(seq 1 30); do
    docker exec "$prefix-socket-maker" test -S "$socket" && break
    sleep 1
  done
  start "$prefix-socket" -v "$prefix-socket:/run"
else
  docker run -d --name "$prefix-socket" -p 127.0.0.1::8383 -e DB_USER=peekaping -e DB_PASS=bundle-test \
    --entrypoint /bin/sh "$image" -c "nc -lU $socket & sleep 1; chmod 600 $socket; exec /app/startup.sh" >/dev/null
fi
if check "starts and reports healthy" wait_healthy "$prefix-socket"; then
  check "startup warns that Docker monitors can't use the socket" logs_mention "$prefix-socket" "Docker monitors can't use"
fi

echo "# /app/data where root can't change owners"
if [ "$db" = sqlite ]; then
  echo "skip - the SQLite bundle keeps its database there, so it can't start"
else
  start "$prefix-ro-data" --tmpfs /app/data:ro
  if check "starts and reports healthy" wait_healthy "$prefix-ro-data"; then
    check "startup warns that it couldn't give /app/data away" logs_mention "$prefix-ro-data" "couldn't give /app/data"
  fi
fi

echo "# ping_group_range without the service group"
start "$prefix-ping" --sysctl 'net.ipv4.ping_group_range=0 0'
if check "starts and reports healthy" wait_healthy "$prefix-ping"; then
  check "startup warns about ping_group_range" logs_mention "$prefix-ping" "net.ipv4.ping_group_range"
fi

echo "# docker run --user 1500:1600"
# The user owns the data volume, as the docs ask
if [ "$db" = sqlite ]; then
  data_dir=/app/data
else
  data_dir=/var/lib/postgresql/data
fi
start "$prefix-user" --user 1500:1600 --tmpfs "$data_dir:uid=1500,gid=1600,mode=0700"
if check "starts and reports healthy" wait_healthy "$prefix-user"; then
  check "peekaping runs as 1500:1600" eq "$(ids "$prefix-user" peekaping)" 1500:1600
  if [ -n "$db_proc" ]; then
    check "$db_proc runs as 1500:1600" eq "$(ids "$prefix-user" "$db_proc")" 1500:1600
  fi
  check "startup says PUID and PGID don't apply" logs_mention "$prefix-user" "docker run --user"
  docker stop -t 40 "$prefix-user" >/dev/null
  check "docker stop exits 0" eq "$(docker inspect -f '{{.State.ExitCode}}' "$prefix-user")" 0
fi

echo "# Ctrl+C under docker run -it"
start "$prefix-ctrl-c" -it
if check "starts and reports healthy" wait_healthy "$prefix-ctrl-c"; then
  check "the terminal's foreground process group takes SIGINT" ctrl_c "$prefix-ctrl-c"
  check "Ctrl+C stops the container" wait_exited "$prefix-ctrl-c"
  check "Ctrl+C exits 0" eq "$(exit_code "$prefix-ctrl-c")" 0
  check "peekaping takes one shutdown signal" logs_lack "$prefix-ctrl-c" "Second shutdown signal"
  if [ "$db" = postgres ]; then
    # PostgreSQL takes its shutdown request from the startup script, once peekaping has stopped
    check "Ctrl+C stops peekaping, then PostgreSQL" logs_in_order "$prefix-ctrl-c" \
      "Shutdown signal received" "Closed the database" "Peekaping stopped" "Stopping PostgreSQL" \
      "received fast shutdown request" "database system is shut down"
  else
    check "Ctrl+C shuts peekaping down in order" logs_in_order "$prefix-ctrl-c" \
      "Shutdown signal received" "Closed the database" "Peekaping stopped"
  fi
fi

if [ "$db" = postgres ]; then
  echo "# DB_USER=postgres and DB_NAME=postgres, PostgreSQL's own role and database"
  start "$prefix-pg-superuser" -e DB_USER=postgres
  start "$prefix-pg-database" -e DB_NAME=postgres
  for name in pg-superuser pg-database; do
    if check "$name starts and reports healthy" wait_healthy "$prefix-$name"; then
      check "$name: the API is reachable as the e2e admin" login "$prefix-$name"
      check "$name: the PostgreSQL setup completed" logs_mention "$prefix-$name" "PostgreSQL initialization completed"
    fi
  done

  echo "# A first start that fails once PostgreSQL runs"
  # Role names starting with pg_ are reserved, so creating DB_USER fails
  start "$prefix-pg-reserved" -e DB_USER=pg_peekaping
  check "the failed setup stops the container" wait_exited "$prefix-pg-reserved"
  check "the failed setup exits non-zero" ne "$(exit_code "$prefix-pg-reserved")" 0
  check "the failed setup stops PostgreSQL rather than leaving it to be killed" logs_in_order "$prefix-pg-reserved" \
    'role name "pg_peekaping" is reserved' "Stopping PostgreSQL" "database system is shut down"
  docker start "$prefix-pg-reserved" >/dev/null
  check "a second start stops the container" wait_exited "$prefix-pg-reserved"
  check "a second start keeps the cluster: initdb ran once" eq "$(logs_count "$prefix-pg-reserved" "Initializing PostgreSQL")" 1
  check "a second start finishes the setup" logs_mention "$prefix-pg-reserved" "Finishing the PostgreSQL setup"
fi

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures check(s) failed for the $db bundle"
  exit 1
fi
echo "All checks passed for the $db bundle"
