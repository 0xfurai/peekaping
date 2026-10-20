#!/usr/bin/env bash
# Renders the Helm chart the way an install does and checks the result: the
# chart lints, it renders with default values and with the bundled PostgreSQL
# and Redis, the web Deployment's seed init container runs the same image as the
# web container (also when web.image.tag is set), the api, producer, worker and
# ingester Deployments wait for the migrations in an init container that runs
# the migrate image (also when migrate.image.tag is set) while the web
# Deployment has none, an enabled AppArmor profile covers that init container as
# well as the main container, the init container's script ends at once for a
# MongoDB install, which has no SQL migrations to wait for, and keeps waiting
# for a PostgreSQL one until `./bun db status` reports none unapplied, the
# migrate Job is named after the release revision (and its name fits Kubernetes'
# limit of 63 characters for the longest release name Helm allows), and the
# install notes say Peekaping runs in cluster mode and link to the Helm docs
# page.
#
# Usage: scripts/test-chart.sh
# Needs helm (3.13 or later) and network access to registry-1.docker.io, where
# the chart's PostgreSQL and Redis dependencies live. It works on a copy of
# charts/ in a temp directory, so the repo gets no charts/charts/*.tgz and no
# Chart.lock.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
release=peekaping
failures=0

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cp -R "$root/charts" "$work/charts"
chart="$work/charts"

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

# run <name> <command...>: runs the command, and shows its output when it fails
run() {
  "${@:2}" >"$work/$1.log" 2>&1 || { sed 's/^/       /' "$work/$1.log"; return 1; }
}

# render <name> [helm template args...]: renders the chart into $work/<name>.yaml
render() {
  helm template "$release" "$chart" "${@:2}" >"$work/$1.yaml" 2>"$work/$1.log" ||
    { sed 's/^/       /' "$work/$1.log"; return 1; }
}

# container_of <rendered file> <template> <name>: the lines of the container or
# init container with that name in the rendered <template> (such as web-deploy
# or migrate-job), from its "- name:" line to the end of its block
container_of() {
  awk -v template="$2" -v name="$3" '
    /^# Source: / { in_template = ($0 ~ "/" template "\\.yaml$"); next }
    /^---$/ { in_template = 0 }
    in_template && !found && $0 ~ "^ *- name: " name "$" {
      match($0, /^ */); indent = RLENGTH; found = 1; print; next
    }
    found {
      match($0, /^ */)
      if ($0 !~ /^ *$/ && RLENGTH <= indent) exit
      print
    }
  ' "$1"
}

# image_of <rendered file> <template> <name>: the image of that container
image_of() {
  container_of "$@" | awk '/^ *image: / { print $2; exit }'
}

# env_from_of <rendered file> <template> <name>: the envFrom block of that
# container, which says where its environment variables come from
env_from_of() {
  container_of "$@" | awk '
    /^ *envFrom:$/ { match($0, /^ */); indent = RLENGTH; found = 1; print; next }
    found {
      match($0, /^ */)
      if ($0 !~ /^ *$/ && RLENGTH <= indent) exit
      print
    }
  '
}

# job_name_of <rendered file> <template>: the metadata name of the Job that
# <template> renders (such as migrate-job)
job_name_of() {
  awk -v template="$2" '
    /^# Source: / { in_template = ($0 ~ "/" template "\\.yaml$"); next }
    /^---$/ { in_template = 0 }
    in_template && /^kind: / && $2 != "Job" { exit }
    in_template && /^  name: / { print $2; exit }
  ' "$1"
}

# job_named <rendered file> <template> <expected name>: the Job that <template>
# renders (such as migrate-job) has that metadata name
job_named() {
  local name
  name=$(job_name_of "$1" "$2")
  if [ "$name" != "$3" ]; then
    echo "       the Job in $2 is named '${name:-<none>}', expected $3"
    return 1
  fi
}

# job_name_fits <release name>: for a release of that name, the migrate Job is
# named <release>-migrate cut to 52 characters, then -<revision>, so a revision
# of up to 10 digits keeps the name within the 63 characters Kubernetes allows.
# Helm allows release names of 53 characters, which <release>-migrate-<revision>
# would exceed from revision 10 on. A name that fits is left as it is, and the
# cut never leaves a hyphen in front of the revision's.
job_name_fits() {
  local name prefix
  helm template "$1" "$chart" >"$work/job-name.yaml" 2>"$work/job-name.log" ||
    { sed 's/^/       /' "$work/job-name.log"; return 1; }
  name=$(job_name_of "$work/job-name.yaml" migrate-job)
  prefix=${name%-1}
  if [ -z "$name" ] || [ "$prefix" = "$name" ]; then
    echo "       the Job is named '${name:-<none>}', expected a name ending in -1 (helm template renders revision 1)"
    return 1
  fi
  if [ "${#prefix}" -gt 52 ]; then
    echo "       the Job is named $name: with a 10 digit revision that is $((${#prefix} + 11)) characters, more than 63"
    return 1
  fi
  if [[ $name == *--* ]]; then
    echo "       the Job is named $name, with a double hyphen where the release name was cut"
    return 1
  fi
  if [[ $1-migrate != "$prefix"* ]]; then
    echo "       the Job is named $name, which doesn't start like $1-migrate"
    return 1
  fi
  if [ "$((${#1} + 8))" -le 52 ] && [ "$name" != "$1-migrate-1" ]; then
    echo "       the Job is named $name, expected $1-migrate-1: a name that fits is left as it is"
    return 1
  fi
}

# web_matches_seed <rendered file> [expected image]: the init container that
# seeds the web root copies it out of the image the web container serves
web_matches_seed() {
  local seed web
  seed=$(image_of "$1" web-deploy seed-web-root)
  web=$(image_of "$1" web-deploy "$release-web")
  if [ -z "$seed" ] || [ -z "$web" ]; then
    echo "       no seed-web-root or $release-web image in the web Deployment"
    return 1
  fi
  if [ "$seed" != "$web" ]; then
    echo "       seed-web-root runs $seed, the web container runs $web"
    return 1
  fi
  if [ -n "${2:-}" ] && [ "$web" != "$2" ]; then
    echo "       expected $2, got $web"
    return 1
  fi
}

# waits_for_migrations <rendered file> <component> [expected image]: the
# component's Deployment holds its pods in an init container that runs the
# migrate image, the one the migrate Job runs, with the same environment as the
# component's own container
waits_for_migrations() {
  local waiter job waiter_env
  waiter=$(image_of "$1" "$2-deploy" wait-for-migrations)
  job=$(image_of "$1" migrate-job "$release-migrate")
  if [ -z "$waiter" ]; then
    echo "       no wait-for-migrations init container in the $2 Deployment"
    return 1
  fi
  if [ "$waiter" != "$job" ]; then
    echo "       wait-for-migrations in the $2 Deployment runs $waiter, the migrate Job runs $job"
    return 1
  fi
  if [ -n "${3:-}" ] && [ "$waiter" != "$3" ]; then
    echo "       expected $3, got $waiter"
    return 1
  fi
  waiter_env=$(env_from_of "$1" "$2-deploy" wait-for-migrations)
  if [ -z "$waiter_env" ] || [ "$waiter_env" != "$(env_from_of "$1" "$2-deploy" "$release-$2")" ]; then
    echo "       wait-for-migrations in the $2 Deployment doesn't read its environment from where the $2 container does"
    return 1
  fi
}

# backend_components_wait <rendered file> [expected image]: the api, producer,
# worker and ingester each wait for the migrations, and web, which doesn't use
# the database, doesn't
backend_components_wait() {
  local component status=0
  for component in api producer worker ingester; do
    waits_for_migrations "$1" "$component" "${2:-}" || status=1
  done
  if [ -n "$(container_of "$1" web-deploy wait-for-migrations)" ]; then
    echo "       the web Deployment has a wait-for-migrations init container"
    status=1
  fi
  return $status
}

# apparmor_annotations_of <rendered file> <template>: the AppArmor annotations,
# one "container.apparmor.security.beta.kubernetes.io/<container>: <profile>"
# per line, in the rendered <template> (such as api-deploy)
apparmor_annotations_of() {
  awk -v template="$2" '
    /^# Source: / { in_template = ($0 ~ "/" template "\\.yaml$"); next }
    /^---$/ { in_template = 0 }
    in_template && /^ *container\.apparmor\.security\.beta\.kubernetes\.io\// { sub(/^ */, ""); print }
  ' "$1"
}

# apparmor_covers_init <rendered file> <component>: the component's pod
# template has the AppArmor annotation for its container and for its
# wait-for-migrations init container. An annotation names one container, so an
# init container left out runs without the profile.
apparmor_covers_init() {
  local annotations container status=0
  annotations=$(apparmor_annotations_of "$1" "$2-deploy")
  for container in "$release-$2" wait-for-migrations; do
    if ! grep -Fqx "container.apparmor.security.beta.kubernetes.io/$container: runtime/default" <<<"$annotations"; then
      echo "       no runtime/default AppArmor annotation for $container in the $2 Deployment"
      status=1
    fi
  done
  return $status
}

# apparmor_off <rendered file>: the api, producer, worker and ingester pod
# templates have no AppArmor annotation, which is what apparmor.enabled=false
# means
apparmor_off() {
  local component annotations status=0
  for component in api producer worker ingester; do
    annotations=$(apparmor_annotations_of "$1" "$component-deploy")
    if [ -n "$annotations" ]; then
      echo "       the $component Deployment has AppArmor annotations although apparmor.enabled is false:"
      sed 's/^/       /' <<<"$annotations"
      status=1
    fi
  done
  return $status
}

# wait_script_of <rendered file> <template>: the shell script of the
# wait-for-migrations init container in the rendered <template> (such as
# api-deploy): the lines under `- |` in its command, without their indentation
wait_script_of() {
  container_of "$1" "$2" wait-for-migrations | awk '
    !body && /^ *- \|$/ { match($0, /^ */); indent = RLENGTH; body = 1; next }
    body {
      match($0, /^ */)
      if ($0 !~ /^ *$/ && RLENGTH <= indent) exit
      if (!strip) strip = RLENGTH
      print substr($0, strip + 1)
    }
  '
}

# run_within <seconds> <command...>: runs the command with its output in
# $work/script.out. It returns the command's exit status when the command ends
# within <seconds>, and kills it and returns 124 when it doesn't. (timeout is
# not on every macOS, so this is a small version of it.)
run_within() {
  local limit=$1 pid status=0 tenths=0
  shift
  "$@" >"$work/script.out" 2>&1 &
  pid=$!
  while kill -0 "$pid" 2>/dev/null; do
    if [ "$tenths" -ge $((limit * 10)) ]; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      return 124
    fi
    sleep 0.1
    tenths=$((tenths + 1))
  done
  wait "$pid" || status=$?
  return $status
}

# wait_script_run <rendered file> <db type> <fails|applied> <ends|waits>: runs
# the api Deployment's wait-for-migrations script with sh, as the container
# does, in an empty directory with DB_TYPE=<db type> and a stub ./bun in place
# of the migrate image's. With `fails`, `./bun db status` fails, as it does
# while the database or its migration tables are missing. With `applied` it
# prints "unapplied migrations: empty". The script must end with exit status 0
# within 5 seconds (`ends`), or still be running after 3 (`waits`).
wait_script_run() {
  local dir=$work/wait-script status=0
  rm -rf "$dir"
  mkdir "$dir"
  wait_script_of "$1" api-deploy >"$dir/wait.sh"
  if [ ! -s "$dir/wait.sh" ]; then
    echo "       no script in the wait-for-migrations init container of the api Deployment"
    return 1
  fi
  case $3 in
    fails) printf '#!/bin/sh\necho "pq: relation does not exist" >&2\nexit 1\n' >"$dir/bun" ;;
    applied) printf '#!/bin/sh\necho "unapplied migrations: empty"\n' >"$dir/bun" ;;
  esac
  chmod +x "$dir/bun"
  case $4 in
    ends)
      (cd "$dir" && run_within 5 env "DB_TYPE=$2" sh wait.sh) || status=$?
      if [ "$status" = 124 ]; then
        echo "       DB_TYPE=$2: the script was still waiting after 5 seconds, output so far:"
        sed 's/^/       /' "$work/script.out"
        return 1
      elif [ "$status" != 0 ]; then
        echo "       DB_TYPE=$2: the script exited with status $status:"
        sed 's/^/       /' "$work/script.out"
        return 1
      fi
      ;;
    waits)
      (cd "$dir" && run_within 3 env "DB_TYPE=$2" sh wait.sh) || status=$?
      if [ "$status" != 124 ]; then
        echo "       DB_TYPE=$2: the script ended with status $status within 3 seconds, output:"
        sed 's/^/       /' "$work/script.out"
        return 1
      fi
      ;;
  esac
}

# notes_say <text>: the notes an install prints have the text. A client-side
# dry run prints them and needs no cluster.
notes_say() {
  local output notes
  output=$(helm install "$release" "$chart" --dry-run=client 2>&1) || { sed 's/^/       /' <<<"$output"; return 1; }
  notes=$(sed -n '/^NOTES:/,$p' <<<"$output")
  if [ -z "$notes" ]; then
    echo "       the install printed no NOTES"
    return 1
  fi
  grep -qiF -- "$1" <<<"$notes" || { echo "       no '$1' in the notes:"; sed 's/^/       /' <<<"$notes"; return 1; }
}

echo "# Dependencies"
if check "helm dependency build fetches PostgreSQL and Redis" run dependency helm dependency build "$chart"; then
  echo "# Lint"
  check "helm lint passes" run lint helm lint "$chart" || true

  echo "# Render"
  if check "default values render" render default; then
    check "default values: the seed init container runs the web image" web_matches_seed "$work/default.yaml" || true
    check "default values: api, producer, worker and ingester wait for the migrations, web doesn't" \
      backend_components_wait "$work/default.yaml" 0xfurai/peekaping-migrate:latest || true
    check "default values: the api, producer, worker and ingester pods have no AppArmor annotation" \
      apparmor_off "$work/default.yaml" || true
    # The migrate Job skips the migrations for MongoDB (run-migrations.sh), and
    # `./bun db status` has no MongoDB support, so a script that only polls it
    # would keep the pods of an existing MongoDB install in Init for good.
    for db_type in mongo mongodb; do
      check "default values: the api's wait-for-migrations script ends at once with DB_TYPE=$db_type, although ./bun db status fails" \
        wait_script_run "$work/default.yaml" "$db_type" fails ends || true
    done
    check "default values: the api's wait-for-migrations script ends with DB_TYPE=postgres when no migration is unapplied" \
      wait_script_run "$work/default.yaml" postgres applied ends || true
    check "default values: the api's wait-for-migrations script keeps waiting with DB_TYPE=postgres while ./bun db status fails" \
      wait_script_run "$work/default.yaml" postgres fails waits || true
    # helm template renders revision 1. An upgrade or a rollback has a higher
    # one, so each revision creates its own Job instead of changing the last.
    check "default values: the migrate Job is named after the release revision" \
      job_named "$work/default.yaml" migrate-job "$release-migrate-1" || true
    # Helm allows release names of up to 53 characters. A Job's name is at most
    # 63, so the name is cut to 52 characters before the revision, which leaves
    # room for 10 digits.
    long_release=abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz1
    check "a release name of 53 characters: the migrate Job's name fits 63 characters up to a 10 digit revision" \
      job_name_fits "$long_release" || true
    check "a release name of 53 characters, cut at a hyphen: the migrate Job's name fits and has no double hyphen" \
      job_name_fits "${long_release:0:51}-z" || true
    check "a release name of 44 characters: the migrate Job's name is <release>-migrate-1" \
      job_name_fits "${long_release:0:44}" || true
  fi
  if check "bundled PostgreSQL and Redis render" render bundled --set postgresql.enabled=true --set redis.enabled=true; then
    check "bundled PostgreSQL and Redis: the seed init container runs the web image" \
      web_matches_seed "$work/bundled.yaml" || true
    check "bundled PostgreSQL and Redis: api, producer, worker and ingester wait for the migrations, web doesn't" \
      backend_components_wait "$work/bundled.yaml" 0xfurai/peekaping-migrate:latest || true
  fi
  if check "web.image.tag=0.0.47 renders" render tagged --set web.image.tag=0.0.47; then
    check "web.image.tag=0.0.47: the seed init container runs the web image at that tag" \
      web_matches_seed "$work/tagged.yaml" 0xfurai/peekaping-web:0.0.47 || true
  fi
  if check "migrate.image.tag=0.0.47 renders" render migrate-tagged --set migrate.image.tag=0.0.47; then
    check "migrate.image.tag=0.0.47: the wait-for-migrations init containers run the migrate image at that tag" \
      backend_components_wait "$work/migrate-tagged.yaml" 0xfurai/peekaping-migrate:0.0.47 || true
  fi
  if check "apparmor.enabled=true for the api, producer, worker and ingester renders" render apparmor \
    --set api.apparmor.enabled=true --set producer.apparmor.enabled=true \
    --set worker.apparmor.enabled=true --set ingester.apparmor.enabled=true; then
    for component in api producer worker ingester; do
      check "$component.apparmor.enabled=true: the pod template annotates $release-$component and wait-for-migrations" \
        apparmor_covers_init "$work/apparmor.yaml" "$component" || true
    done
  fi

  echo "# Notes"
  check "the install notes name cluster mode" notes_say "cluster mode" || true
  check "the install notes link to the Helm docs page" \
    notes_say "https://docs.peekaping.com/self-hosting/kubernetes-helm" || true
fi

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures check(s) failed for the Helm chart"
  exit 1
fi
echo "All checks passed for the Helm chart"
