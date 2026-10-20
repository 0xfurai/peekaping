#!/usr/bin/env bash
# Renders the Helm chart the way an install does and checks the result: the
# chart lints, it renders with default values and with the bundled PostgreSQL
# and Redis, the bundled PostgreSQL and Redis run images pinned by digest, the
# web Deployment's seed init container runs the same image as the web container
# (also when web.image.tag is set), the api, producer, worker and ingester
# Deployments wait for the migrations in an init container that runs the migrate
# image (also when migrate.image.tag is set) while the web Deployment has none,
# an enabled AppArmor profile covers that init container as well as the main
# container, the migrate Job is named after the release revision and retries 6
# times, so it outlasts a restart of the bundled PostgreSQL, and the install
# notes say Peekaping runs in cluster mode and link to the Helm docs page.
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

# job_named <rendered file> <template> <expected name>: the Job that <template>
# renders (such as migrate-job) has that metadata name
job_named() {
  local name
  name=$(awk -v template="$2" '
    /^# Source: / { in_template = ($0 ~ "/" template "\\.yaml$"); next }
    /^---$/ { in_template = 0 }
    in_template && /^kind: / && $2 != "Job" { exit }
    in_template && /^  name: / { print $2; exit }
  ' "$1")
  if [ "$name" != "$3" ]; then
    echo "       the Job in $2 is named '${name:-<none>}', expected $3"
    return 1
  fi
}

# job_retries <rendered file> <template> <expected>: the Job that <template>
# renders (such as migrate-job) has that backoffLimit
job_retries() {
  local limit
  limit=$(awk -v template="$2" '
    /^# Source: / { in_template = ($0 ~ "/" template "\\.yaml$"); next }
    /^---$/ { in_template = 0 }
    in_template && /^  backoffLimit: / { print $2; exit }
  ' "$1")
  if [ "$limit" != "$3" ]; then
    echo "       the Job in $2 has backoffLimit ${limit:-<none>}, expected $3"
    return 1
  fi
}

# pinned_by_digest <rendered file> <subchart>: every image the subchart
# (postgresql or redis) renders is pinned by digest, repository@sha256:..., so a
# rescheduled pod can't pull a newer major version onto the data of an old one
pinned_by_digest() {
  local unpinned
  unpinned=$(awk -v subchart="$2" '
    /^# Source: / { in_subchart = ($0 ~ "^# Source: [^/]+/charts/" subchart "/"); next }
    in_subchart && /^ *image: / {
      image = $2; gsub(/"/, "", image); found++
      if (image !~ /@sha256:[0-9a-f]+$/) print image
    }
    END { if (!found) print "<no image rendered>" }
  ' "$1")
  if [ -n "$unpinned" ]; then
    echo "       $2 runs without a digest: $(paste -sd ' ' - <<<"$unpinned")"
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
    # helm template renders revision 1. An upgrade or a rollback has a higher
    # one, so each revision creates its own Job instead of changing the last.
    check "default values: the migrate Job is named after the release revision" \
      job_named "$work/default.yaml" migrate-job "$release-migrate-1" || true
    # Failed tries back off 10 s, 20 s, 40 s and so on, so 6 tries last long
    # enough for the bundled PostgreSQL to come back from a restart that the
    # same upgrade causes.
    check "default values: the migrate Job retries 6 times" job_retries "$work/default.yaml" migrate-job 6 || true
  fi
  if check "bundled PostgreSQL and Redis render" render bundled --set postgresql.enabled=true --set redis.enabled=true; then
    check "bundled PostgreSQL runs an image pinned by digest" pinned_by_digest "$work/bundled.yaml" postgresql || true
    check "bundled Redis runs an image pinned by digest" pinned_by_digest "$work/bundled.yaml" redis || true
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
