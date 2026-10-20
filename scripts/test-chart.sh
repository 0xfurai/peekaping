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
# Every image tag defaults to the chart's appVersion: charts/ has `latest`, and
# a package made by scripts/package-chart.sh (here with version 9.9.9) has the
# release. A component's image.tag still overrides it, for that component's
# containers only. The package is also checked for its version and appVersion,
# for carrying the PostgreSQL and Redis subcharts so installing it needs no
# `helm dependency build`, and for rendering.
#
# Usage: scripts/test-chart.sh
# Needs helm (3.13 or later) and network access to registry-1.docker.io, where
# the chart's PostgreSQL and Redis dependencies live. It works on copies of
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

# render_from <chart> <name> [helm template args...]: renders the chart (a
# directory or a package) into $work/<name>.yaml
render_from() {
  helm template "$release" "$1" "${@:3}" >"$work/$2.yaml" 2>"$work/$2.log" ||
    { sed 's/^/       /' "$work/$2.log"; return 1; }
}

# render <name> [helm template args...]: renders charts/ into $work/<name>.yaml
render() {
  render_from "$chart" "$@"
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

# images_of <rendered file>: "<template> <image>" for every container and init
# container of the Peekaping templates (the Deployments and the migrate Job),
# not those of the PostgreSQL and Redis subcharts
images_of() {
  awk '
    /^# Source: / {
      in_template = ($0 ~ "^# Source: [^/]+/templates/")
      n = split($0, parts, "/"); template = parts[n]; sub(/\.yaml$/, "", template)
      next
    }
    /^---$/ { in_template = 0 }
    in_template && /^ *image: / { image = $2; gsub(/"/, "", image); print template, image }
  ' "$1"
}

# images_at <rendered file> <tag>: every image of the Peekaping templates ends
# in :<tag>. There are 11: the api, producer, worker and ingester containers
# and their wait-for-migrations init containers, the web container and its
# seed-web-root init container, and the migrate Job's container.
images_at() {
  local images count off
  images=$(images_of "$1")
  count=$(grep -c . <<<"$images" || true)
  if [ "$count" != 11 ]; then
    echo "       found $count images in the Peekaping templates, expected 11:"
    sed 's/^/       /' <<<"$images"
    return 1
  fi
  off=$(awk -v suffix=":$2" 'substr($2, length($2) - length(suffix) + 1) != suffix { print "       " $1 " runs " $2 }' <<<"$images")
  if [ -n "$off" ]; then
    echo "       expected every image at :$2"
    echo "$off"
    return 1
  fi
}

# tag_overrides <rendered file> <repository> <tag> <default tag> <count>:
# exactly <count> images run <repository>, all at <tag>, and every other image
# of the Peekaping templates runs <default tag>. So a component's image.tag
# reaches every container that runs its image, init containers included, and
# no other component.
tag_overrides() {
  local problems
  problems=$(images_of "$1" | awk -v repository="$2" -v tag="$3" -v fallback="$4" -v count="$5" '
    {
      i = match($2, /:[^:]*$/)
      image_repository = substr($2, 1, i - 1); image_tag = substr($2, i + 1)
      if (image_repository == repository) {
        matched++
        if (image_tag != tag) print "       " $1 " runs " $2 ", expected " repository ":" tag
      } else if (image_tag != fallback) {
        print "       " $1 " runs " $2 ", expected the tag " fallback
      }
    }
    END { if (matched != count) print "       found " matched + 0 " images of " repository ", expected " count }
  ')
  if [ -n "$problems" ]; then
    echo "$problems"
    return 1
  fi
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

# chart_field <package> <field>: the value of a top-level field of the
# package's Chart.yaml, such as version or appVersion
chart_field() {
  helm show chart "$1" | awk -v field="$2" '$0 ~ "^" field ": " { gsub(/"/, "", $2); print $2; exit }'
}

# package_versions <package> <version> <appVersion>: the package's chart has
# that version and appVersion
package_versions() {
  local version app_version
  version=$(chart_field "$1" version)
  app_version=$(chart_field "$1" appVersion)
  if [ "$version" != "$2" ] || [ "$app_version" != "$3" ]; then
    echo "       the package has version '${version:-<none>}' and appVersion '${app_version:-<none>}', expected $2 and $3"
    return 1
  fi
}

# package_has_subcharts <package>: the package carries the PostgreSQL and Redis
# subcharts, so an install from it needs no `helm dependency build`
package_has_subcharts() {
  local files subchart status=0
  files=$(tar -tzf "$1")
  for subchart in postgresql redis; do
    if ! grep -qx "$release/charts/$subchart/Chart.yaml" <<<"$files"; then
      echo "       the package has no $subchart subchart"
      status=1
    fi
  done
  return $status
}

# repo_unchanged <listing before>: charts/ in the repo lists the same files as
# before, so packaging left no charts/charts/*.tgz and no Chart.lock behind
repo_unchanged() {
  local after
  after=$(find "$root/charts" | sort)
  if [ "$after" != "$1" ]; then
    echo "       charts/ changed:"
    diff <(echo "$1") <(echo "$after") | sed 's/^/       /' || true
    return 1
  fi
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
    # charts/ has appVersion latest, so a checkout installs the latest images
    check "default values: every image runs the tag latest, the appVersion of charts/" \
      images_at "$work/default.yaml" latest || true
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
    check "web.image.tag=0.0.47: the web container and its seed init container change tag, nothing else does" \
      tag_overrides "$work/tagged.yaml" 0xfurai/peekaping-web 0.0.47 latest 2 || true
  fi
  if check "migrate.image.tag=0.0.47 renders" render migrate-tagged --set migrate.image.tag=0.0.47; then
    check "migrate.image.tag=0.0.47: the wait-for-migrations init containers run the migrate image at that tag" \
      backend_components_wait "$work/migrate-tagged.yaml" 0xfurai/peekaping-migrate:0.0.47 || true
    check "migrate.image.tag=0.0.47: the migrate Job and the 4 wait-for-migrations init containers change tag, nothing else does" \
      tag_overrides "$work/migrate-tagged.yaml" 0xfurai/peekaping-migrate 0.0.47 latest 5 || true
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

echo "# Package"
# What the release workflow does: the chart packaged with the release's version
# and appVersion, here 9.9.9
before=$(find "$root/charts" | sort)
if check "package-chart.sh packages the chart as 9.9.9" run package "$root/scripts/package-chart.sh" 9.9.9 "$work/dist"; then
  package=$work/dist/$release-9.9.9.tgz
  check "the package is $release-9.9.9.tgz" test -f "$package" || true
  check "the package's version and appVersion are 9.9.9" package_versions "$package" 9.9.9 9.9.9 || true
  check "the package carries the PostgreSQL and Redis subcharts" package_has_subcharts "$package" || true
  check "package-chart.sh leaves charts/ in the repo as it was" repo_unchanged "$before" || true
  # An empty image.tag means the appVersion, so every image runs the release
  if check "the package renders" render_from "$package" package-default; then
    check "the package: every image runs the tag 9.9.9, the appVersion" images_at "$work/package-default.yaml" 9.9.9 || true
    check "the package: the seed init container runs the web image at 9.9.9" \
      web_matches_seed "$work/package-default.yaml" 0xfurai/peekaping-web:9.9.9 || true
    check "the package: api, producer, worker and ingester wait for the migrations with the migrate image at 9.9.9, web doesn't" \
      backend_components_wait "$work/package-default.yaml" 0xfurai/peekaping-migrate:9.9.9 || true
  fi
  # A component's image.tag overrides the appVersion for the images of that
  # component only: one container each for the api, producer, worker and
  # ingester, two for web (the container and seed-web-root), and five for
  # migrate (the Job and the wait-for-migrations init containers)
  for component in api:1 producer:1 worker:1 ingester:1 web:2 migrate:5; do
    name=${component%%:*}
    count=${component##*:}
    if check "the package: $name.image.tag=custom renders" render_from "$package" "package-$name" --set "$name.image.tag=custom"; then
      check "the package: $name.image.tag=custom changes the $count image(s) of $name, nothing else" \
        tag_overrides "$work/package-$name.yaml" "0xfurai/peekaping-$name" custom 9.9.9 "$count" || true
    fi
  done
  check "the package: web.image.tag=custom, the seed init container runs the web image at that tag" \
    web_matches_seed "$work/package-web.yaml" 0xfurai/peekaping-web:custom || true
  check "the package: migrate.image.tag=custom, the wait-for-migrations init containers run the migrate image at that tag" \
    backend_components_wait "$work/package-migrate.yaml" 0xfurai/peekaping-migrate:custom || true
  if check "the package renders with the bundled PostgreSQL and Redis" \
    render_from "$package" package --set postgresql.enabled=true --set redis.enabled=true; then
    check "the package's bundled PostgreSQL runs an image pinned by digest" pinned_by_digest "$work/package.yaml" postgresql || true
    check "the package's bundled Redis runs an image pinned by digest" pinned_by_digest "$work/package.yaml" redis || true
  fi
fi

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures check(s) failed for the Helm chart"
  exit 1
fi
echo "All checks passed for the Helm chart"
