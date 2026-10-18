#!/usr/bin/env bash
# Smoke-tests the user a bundle image runs api, producer, worker and ingester
# as: which user each process runs as, what it can write, PUID/PGID, the
# Docker socket and the startup warnings.
#
# Usage: scripts/test-bundle-user.sh <sqlite|mongo|postgres> [image]
# Without an image, it builds Dockerfile.bundle.<db> from the repo root.
set -u

db=${1:?usage: $0 <sqlite|mongo|postgres> [image]}
image=${2:-}
root=$(cd "$(dirname "$0")/.." && pwd)
prefix="peekaping-bundle-test-$db"
socket=/var/run/docker.sock
failures=0

if [ -z "$image" ]; then
  image="$prefix:local"
  echo "Building $image..."
  docker build -q -f "$root/Dockerfile.bundle.$db" -t "$image" "$root" >/dev/null || exit 1
fi

cleanup() {
  docker rm -f "$prefix-default" "$prefix-ids" "$prefix-bad-uid" "$prefix-bad-gid" "$prefix-taken-gid" \
    "$prefix-socket" "$prefix-ro-data" "$prefix-ping" >/dev/null 2>&1
}
trap cleanup EXIT
cleanup

# check <description> <command...>: runs the command and records the result.
check() {
  if "${@:2}"; then
    echo "ok   - $1"
  else
    echo "FAIL - $1"
    failures=$((failures + 1))
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

# cant_write <container> <path>: as peekaping, which must exist.
cant_write() { docker exec -u peekaping "$1" test ! -w "$2"; }

start() { # start <name> [docker run args...]
  docker run -d --name "$1" -e DB_USER=peekaping -e DB_PASS=bundle-test "${@:2}" "$image" >/dev/null
}

wait_healthy() {
  for _ in $(seq 1 180); do
    docker exec "$1" curl -sf -o /dev/null http://localhost:8383/api/v1/health 2>/dev/null && return 0
    [ "$(docker inspect -f '{{.State.Running}}' "$1")" = true ] || break
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

logs_mention() { docker logs "$1" 2>&1 | grep -q "$2" || { echo "       no '$2' in the logs"; return 1; }; }

logs_lack() {
  if docker logs "$1" 2>&1 | grep -q "$2"; then
    echo "       '$2' in the logs"
    return 1
  fi
}

services="api producer worker ingester"

echo "# Default run"
socket_args=()
[ -S "$socket" ] && socket_args=(-v "$socket:$socket")
# The ${a[@]+...} form keeps bash 3.2 (macOS) from failing on an empty array under set -u.
start "$prefix-default" ${socket_args[@]+"${socket_args[@]}"}
if check "starts and reports healthy" wait_healthy "$prefix-default"; then
  for svc in $services; do
    check "$svc runs as 1000:1000" eq "$(ids "$prefix-default" "$svc")" 1000:1000
  done
  for proc in supervisord caddy redis-server; do
    check "$proc keeps running as root" eq "$(ids "$prefix-default" "$proc" | cut -d: -f1)" 0
  done
  case "$db" in
    mongo) db_proc=mongod db_user=mongodb ;;
    postgres) db_proc=postgres db_user=postgres ;;
    *) db_proc= ;;
  esac
  if [ -n "$db_proc" ]; then
    check "$db_proc keeps running as $db_user" eq \
      "$(ids "$prefix-default" "$db_proc" | cut -d: -f1)" "$(docker exec "$prefix-default" id -u "$db_user")"
  fi
  check "peekaping can't write the api binary" cant_write "$prefix-default" /app/server/api
  check "peekaping can't write the web assets" cant_write "$prefix-default" /app/web/index.html
  check "peekaping owns /app/data" eq "$(owner "$prefix-default" /app/data)" 1000:1000
  if [ "$db" = sqlite ]; then
    check "peekaping owns the SQLite database" eq "$(owner "$prefix-default" /app/data/peekaping.db)" 1000:1000
  fi
  check "no ping warning on default Docker" logs_lack "$prefix-default" ping_group_range
  if [ -S "$socket" ]; then
    socket_gid=$(docker exec "$prefix-default" stat -c %g "$socket")
    check "worker has the Docker socket's group ($socket_gid)" has_word "$(groups_of "$prefix-default" worker)" "$socket_gid"
    check "peekaping can use the Docker socket" eq \
      "$(docker exec -u peekaping "$prefix-default" curl -s -o /dev/null -w '%{http_code}' --unix-socket "$socket" http://docker/_ping)" 200
    check "no Docker socket warning" logs_lack "$prefix-default" "Docker monitors can't use"
  else
    echo "skip - no $socket on this host, so the Docker socket checks didn't run"
  fi
fi
docker rm -f "$prefix-default" >/dev/null

echo "# PUID=1500 PGID=1600"
start "$prefix-ids" -e PUID=1500 -e PGID=1600
if check "starts and reports healthy" wait_healthy "$prefix-ids"; then
  for svc in $services; do
    check "$svc runs as 1500:1600" eq "$(ids "$prefix-ids" "$svc")" 1500:1600
  done
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
start "$prefix-bad-gid" -e PGID=staff
check "PGID=staff (not a number) stops the container" wait_exited "$prefix-bad-gid"
check "PGID=staff exits non-zero" ne "$(docker inspect -f '{{.State.ExitCode}}' "$prefix-bad-gid")" 0
check "PGID=staff is named in the error" logs_mention "$prefix-bad-gid" "PGID must be a number"
start "$prefix-taken-gid" -e PGID=0
check "PGID=0 (root's group) stops the container" wait_exited "$prefix-taken-gid"
check "PGID=0 is named in the error" logs_mention "$prefix-taken-gid" "PGID 0 is already used"

echo "# A Docker socket its group can't use"
# A stand-in socket, mode 600, so the user's new group gives no access.
docker run -d --name "$prefix-socket" -e DB_USER=peekaping -e DB_PASS=bundle-test --entrypoint /bin/sh "$image" \
  -c "nc -lU $socket & sleep 1; chmod 600 $socket; exec /app/startup.sh" >/dev/null
if check "starts and reports healthy" wait_healthy "$prefix-socket"; then
  check "startup warns that Docker monitors can't use the socket" logs_mention "$prefix-socket" "Docker monitors can't use"
fi

echo "# /app/data where root can't change owners"
if [ "$db" = sqlite ]; then
  echo "skip - the SQLite bundle writes its database there before this step"
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

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures check(s) failed for the $db bundle"
  exit 1
fi
echo "All checks passed for the $db bundle"
