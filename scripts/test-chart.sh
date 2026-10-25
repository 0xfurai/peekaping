#!/usr/bin/env bash
# Renders the Helm chart the way an install does and checks the result: the
# chart lints, it renders with default values and with the bundled PostgreSQL
# and Redis, the bundled PostgreSQL and Redis run images pinned by digest, the
# web Deployment's seed init container runs the same image as the web container
# (also when web.image.tag is set), the api Deployment waits for the migrations
# in an init container that runs the migrate image (also when migrate.image.tag
# is set) while the probe and web Deployments, which don't use the database, have
# none, an enabled AppArmor profile covers that init container as well as the
# main container, the init container's script keeps waiting until
# `./bun db status` reports none unapplied, the migrate Job is named
# after the release revision (and its name fits Kubernetes' limit of 63
# characters for the longest release name Helm allows) and retries 6 times, so
# it outlasts a restart of the bundled PostgreSQL, the api and probe pods get 35
# seconds to stop, the web pod sets the sysctl that lets
# its non-root nginx bind port 80 (web.sysctls) and mounts the
# writable paths that nginx needs under a read-only root filesystem, the
# ConfigMap sets TRUSTED_PROXIES to loopback and the private ranges (and a
# values file can change it), SQLite as the storage backend (config.env.DB_TYPE,
# in any case) is refused with a message that sends the reader to the bundle
# image, MongoDB (mongo and mongodb) is refused with the message that says MongoDB
# storage was removed and links to the guide for moving to PostgreSQL, and the
# install notes say Peekaping runs in cluster mode and link to the Helm docs page. The Ingress and the Istio VirtualService send the probe
# protocol (/probe) to the api. The producer, worker and ingester are gone: the
# probe Deployment runs the probe image, finds the api through its Service and
# sends the token the api stores on the local probe, from a Secret the chart makes
# or one named in probe.token.existingSecret, and it holds the pod until /ready
# answers; it doesn't get the config secret.
#
# It also runs scripts/update-chart-deps.sh against a fake Docker Hub: a new
# major version of a subchart is reported as such and refused by --write, a
# minor version is written, and current subcharts and images are current.
#
# Every image tag defaults to the chart's appVersion: charts/ has `latest`, and
# a package made by scripts/package-chart.sh (here with version 9.9.9) has the
# release. A component's image.tag still overrides it, for that component's
# containers only. The package is also checked for its version and appVersion,
# for carrying the PostgreSQL and Redis subcharts so installing it needs no
# `helm dependency build`, and for rendering.
#
# Usage: scripts/test-chart.sh
# Needs helm (3.13 or later), jq, and network access to registry-1.docker.io,
# where the chart's PostgreSQL and Redis dependencies live. It works on copies
# of charts/ in a temp directory, so the repo gets no charts/charts/*.tgz and no
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

# render_refused <name> <text> [helm template args...]: rendering charts/ fails,
# and the message says <text>. The output goes to $work/<name>.log.
render_refused() {
  local name=$1 text=$2
  if helm template "$release" "$chart" "${@:3}" >"$work/$name.yaml" 2>"$work/$name.log"; then
    echo "       helm template succeeded, expected it to fail"
    return 1
  fi
  grep -qF -- "$text" "$work/$name.log" || {
    echo "       no '$text' in the message:"
    sed 's/^/       /' "$work/$name.log"
    return 1
  }
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
# in :<tag>. There are 6: the api container and its wait-for-migrations init
# container, the probe container, the web container and its seed-web-root init
# container, and the migrate Job's container.
images_at() {
  local images count off
  images=$(images_of "$1")
  count=$(grep -c . <<<"$images" || true)
  if [ "$count" != 6 ]; then
    echo "       found $count images in the Peekaping templates, expected 6:"
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

# config_env <rendered file> <key>: the value the release's ConfigMap sets for
# <key> (a config.env entry), or nothing when it sets none
config_env() {
  awk -v key="$2" '
    /^# Source: / { in_config = ($0 ~ "/templates/config\\.yaml$"); next }
    /^---$/ { in_config = 0 }
    in_config && index($0, "  " key ": ") == 1 {
      value = substr($0, length(key) + 5); gsub(/^"|"$/, "", value); print value; exit
    }
  ' "$1"
}

# config_sets <rendered file> <key> <value>: the ConfigMap sets <key> to <value>
config_sets() {
  local got
  got=$(config_env "$1" "$2")
  if [ "$got" != "$3" ]; then
    echo "       the ConfigMap sets $2 to '${got:-<nothing>}', expected '$3'"
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

# pod_sysctls_of <rendered file> <template>: the sysctls of the pod in the
# rendered <template> (such as web-deploy), one "<name>=<value>" per line
pod_sysctls_of() {
  awk -v template="$2" '
    /^# Source: / { in_template = ($0 ~ "/" template "\\.yaml$"); in_sysctls = 0; next }
    /^---$/ { in_template = 0; in_sysctls = 0 }
    in_template && /^ *sysctls:/ { in_sysctls = 1; match($0, /^ */); indent = RLENGTH; next }
    in_sysctls {
      match($0, /^ */)
      if ($0 !~ /^ *$/ && RLENGTH <= indent) { in_sysctls = 0; next }
      if ($0 ~ /- name:/) { sub(/^.*- name: */, ""); gsub(/"/, ""); name = $0 }
      if ($0 ~ /^ *value:/) { sub(/^ *value: */, ""); gsub(/"/, ""); print name "=" $0 }
    }
  ' "$1"
}

# web_binds_port_80 <rendered file>: the web pod sets the sysctl that lets its
# non-root nginx bind port 80 on any container runtime, and mounts the writable
# paths nginx-unprivileged needs under readOnlyRootFilesystem: /tmp for its pid
# file and temporary files, and the web root, where the entrypoint writes env.js
web_binds_port_80() {
  local sysctls container path status=0
  sysctls=$(pod_sysctls_of "$1" web-deploy)
  if ! grep -Fqx "net.ipv4.ip_unprivileged_port_start=0" <<<"$sysctls"; then
    echo "       the web pod doesn't set net.ipv4.ip_unprivileged_port_start to 0; its sysctls:"
    sed 's/^/       /' <<<"$sysctls"
    status=1
  fi
  container=$(container_of "$1" web-deploy "$release-web")
  if ! grep -Fq "readOnlyRootFilesystem: true" <<<"$container"; then
    echo "       the web container doesn't have a read-only root filesystem"
    status=1
  fi
  for path in /tmp /usr/share/nginx/html; do
    if ! grep -Eq "^ *mountPath: $path\$" <<<"$container"; then
      echo "       the web container doesn't mount a volume at $path"
      status=1
    fi
  done
  return $status
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

# backend_components_wait <rendered file> [expected image]: the api waits for
# the migrations, and the probe and web, which don't use the database, don't
backend_components_wait() {
  local component status=0
  waits_for_migrations "$1" api "${2:-}" || status=1
  for component in probe web; do
    if [ -n "$(container_of "$1" "$component-deploy" wait-for-migrations)" ]; then
      echo "       the $component Deployment has a wait-for-migrations init container"
      status=1
    fi
  done
  return $status
}

# no_old_components <rendered file>: nothing of the producer, worker or ingester
# is rendered, whose work the api replicas and the probes took over on SQL storage
no_old_components() {
  local found
  found=$(grep -nE -- "$release-(producer|worker|ingester)|peekaping-(producer|worker|ingester)" "$1" || true)
  if [ -n "$found" ]; then
    echo "       the chart still renders the old components:"
    sed 's/^/       /' <<<"$found"
    return 1
  fi
}

# probe_runs <rendered file> <image> <replicas>: the probe Deployment runs that
# image, with that many replicas
probe_runs() {
  local image replicas
  image=$(image_of "$1" probe-deploy "$release-probe")
  replicas=$(awk '
    /^# Source: / { in_template = ($0 ~ "/probe-deploy\\.yaml$"); next }
    /^---$/ { in_template = 0 }
    in_template && /^  replicas: / { print $2; exit }
  ' "$1")
  if [ "$image" != "$2" ] || [ "$replicas" != "$3" ]; then
    echo "       the probe Deployment runs ${image:-<no image>} with ${replicas:-<no>} replicas, expected $2 with $3"
    return 1
  fi
}

# env_entry_of <rendered file> <template> <container> <variable>: the lines of
# that variable's entry in the container's env, after its "- name:" line
env_entry_of() {
  container_of "$1" "$2" "$3" | awk -v var="$4" '
    $0 ~ "^ *- name: " var "$" { match($0, /^ */); indent = RLENGTH; found = 1; next }
    found {
      match($0, /^ */)
      if ($0 !~ /^ *$/ && RLENGTH <= indent) exit
      print
    }
  '
}

# probe_env_is <rendered file> <variable> <value>: the probe container sets that
# variable to that value
probe_env_is() {
  local got
  got=$(env_entry_of "$1" probe-deploy "$release-probe" "$2" | awk '$1 == "value:" { gsub(/"/, "", $2); print $2; exit }')
  if [ "$got" != "$3" ]; then
    echo "       the probe sets $2 to '${got:-<nothing>}', expected '$3'"
    return 1
  fi
}

# secret_ref_is <rendered file> <template> <container> <variable> <secret> <key>:
# that container reads that variable from that key of that Secret
secret_ref_is() {
  local got
  got=$(env_entry_of "$1" "$2" "$3" "$4" | awk '$1 == "name:" { name = $2 } $1 == "key:" { key = $2 } END { if (name != "") print name " " key }')
  if [ "$got" != "$5 $6" ]; then
    echo "       $3 reads $4 from '${got:-<nothing>}', expected '$5 $6'"
    return 1
  fi
}

# probe_ready <rendered file> <port>: the probe listens for /ready on all
# addresses at that port, and the readiness probe asks /ready there
probe_ready() {
  local status=0 container
  probe_env_is "$1" PROBE_READY_ADDR "0.0.0.0:$2" || status=1
  container=$(container_of "$1" probe-deploy "$release-probe")
  if ! awk -v port="$2" '
    /^ *readinessProbe:/ { in_probe = 1; next }
    in_probe && $1 == "path:" && $2 == "/ready" { path = 1 }
    in_probe && $1 == "port:" && $2 == port { found_port = 1 }
    END { exit !(path && found_port) }
  ' <<<"$container"; then
    echo "       the probe container has no readinessProbe for /ready on port $2"
    status=1
  fi
  return $status
}

# probe_env_from_is_config_map_only <rendered file>: the probe container reads
# the release's ConfigMap and no Secret
probe_env_from_is_config_map_only() {
  local env_from
  env_from=$(env_from_of "$1" probe-deploy "$release-probe")
  if ! grep -Fq "configMapRef" <<<"$env_from" || grep -Fq "secretRef" <<<"$env_from"; then
    echo "       the probe container's envFrom is not the ConfigMap alone:"
    sed 's/^/       /' <<<"$env_from"
    return 1
  fi
}

# probe_token_secret_data <rendered file>: the base64 token in the Secret that
# the chart makes for the local probe, or nothing when it makes none
probe_token_secret_data() {
  awk '
    /^# Source: / { in_secret = ($0 ~ "/templates/probe-token-secret\\.yaml$"); next }
    /^---$/ { in_secret = 0 }
    in_secret && $1 == "PROBE_BOOTSTRAP_TOKEN:" { print $2; exit }
  ' "$1"
}

# probe_token_secret <rendered file> <name>: the chart makes a Secret of that
# name for the local probe, with a token of at least 32 characters (the shortest
# the api takes)
probe_token_secret() {
  local data token name
  data=$(probe_token_secret_data "$1")
  token=$(base64 --decode <<<"$data" 2>/dev/null || true)
  name=$(awk '
    /^# Source: / { in_secret = ($0 ~ "/templates/probe-token-secret\\.yaml$"); next }
    /^---$/ { in_secret = 0 }
    in_secret && /^  name: / { print $2; exit }
  ' "$1")
  if [ "$name" != "$2" ]; then
    echo "       the Secret is named '${name:-<none>}', expected $2"
    return 1
  fi
  if [ "${#token}" -lt 32 ]; then
    echo "       the token has ${#token} characters, expected at least 32"
    return 1
  fi
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

# apparmor_off <rendered file>: the api and probe pod templates have no
# AppArmor annotation, which is what apparmor.enabled=false means
apparmor_off() {
  local component annotations status=0
  for component in api probe; do
    annotations=$(apparmor_annotations_of "$1" "$component-deploy")
    if [ -n "$annotations" ]; then
      echo "       the $component Deployment has AppArmor annotations although apparmor.enabled is false:"
      sed 's/^/       /' <<<"$annotations"
      status=1
    fi
  done
  return $status
}

# grace_periods <rendered file>: the api and probe pods get 35 seconds to stop,
# the 30 (api) or 25 (probe) their binaries take to shut down and a margin.
# Kubernetes' default of 30 would kill an api pod that uses its whole time.
grace_periods() {
  local component seconds status=0
  for component in api probe; do
    seconds=$(awk -v template="$component-deploy" '
      /^# Source: / { in_template = ($0 ~ "/" template "\\.yaml$"); next }
      /^---$/ { in_template = 0 }
      in_template && /^ *terminationGracePeriodSeconds:/ { print $2; exit }
    ' "$1")
    if [ "$seconds" != 35 ]; then
      echo "       the $component pod's terminationGracePeriodSeconds is '${seconds:-<none>}', expected 35"
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

# The checks of scripts/update-chart-deps.sh run it against a fake Docker Hub, so
# that CI doesn't spend anonymous pulls on it and the answers are known. The
# stubs for helm and curl, the fake registry (one file for each URL) and a small
# chart (Chart.yaml and values.yaml) live in $work/deps. The latest PostgreSQL
# and Redis images are 18.6.0 and 8.10.2, and the chart pins their digests, so
# the images are current. What a scenario sets is the version of each subchart
# in the chart and the latest version of it.

# hex64 <character>: a 64 character hex string made of that character
hex64() { printf '%064d' 0 | tr 0 "$1"; }

# fake_image <repository> <latest> <amd64> <arm64> <config> <version>: adds the
# repository to the fake registry, with a multi-arch `latest` image of that
# version. The digests are 64 character hex strings, without the sha256: prefix.
fake_image() {
  local dir=$work/deps/registry/registry-1.docker.io/v2/$1
  mkdir -p "$dir/manifests" "$dir/blobs"
  printf '{"manifests":[{"digest":"sha256:%s","platform":{"os":"linux","architecture":"amd64"}},{"digest":"sha256:%s","platform":{"os":"linux","architecture":"arm64"}}]}' \
    "$3" "$4" >"$dir/manifests/latest"
  cp "$dir/manifests/latest" "$dir/manifests/sha256:$2"
  printf 'Docker-Content-Digest: sha256:%s\r\n' "$2" >"$dir/manifests/latest.headers"
  printf '{"config":{"digest":"sha256:%s"}}' "$5" >"$dir/manifests/sha256:$3"
  printf '{"config":{"Labels":{"org.opencontainers.image.version":"%s"}}}' "$6" >"$dir/blobs/sha256:$5"
}

# deps_setup <postgresql chart in the repo> <latest postgresql chart> <redis
# chart in the repo> <latest redis chart>: builds $work/deps from scratch
deps_setup() {
  local dir=$work/deps latest_postgresql latest_redis
  rm -rf "$dir"
  mkdir -p "$dir/bin" "$dir/helm" "$dir/chart" "$dir/before" "$dir/registry/auth.docker.io"

  # curl: answers a GET of https://<host>/<path> with the file
  # $FAKE_REGISTRY/<host>/<path>, without the query, and writes the headers
  # stored beside it where curl is told to with -D
  cat >"$dir/bin/curl" <<'STUB'
#!/bin/sh
prev=
dump=
url=
for arg in "$@"; do
  if [ "$prev" = -D ]; then dump=$arg; fi
  prev=$arg
  url=$arg
done
path=${url#https://}
path=${path%%\?*}
file=$FAKE_REGISTRY/$path
if [ ! -f "$file" ]; then
  echo "curl: (22) The requested URL returned error: 404 $url" >&2
  exit 22
fi
if [ -n "$dump" ] && [ -f "$file.headers" ]; then cp "$file.headers" "$dump"; fi
cat "$file"
STUB
  # helm: answers `helm show chart oci://<repository>/<name>` with the version
  # in $FAKE_HELM/<name>
  cat >"$dir/bin/helm" <<'STUB'
#!/bin/sh
for arg in "$@"; do chart=$arg; done
name=${chart##*/}
printf 'apiVersion: v2\nname: %s\nversion: %s\n' "$name" "$(cat "$FAKE_HELM/$name")"
STUB
  chmod +x "$dir/bin/curl" "$dir/bin/helm"
  printf '{"token":"stub"}' >"$dir/registry/auth.docker.io/token"
  printf '%s\n' "$2" >"$dir/helm/postgresql"
  printf '%s\n' "$4" >"$dir/helm/redis"

  latest_postgresql=$(hex64 a)
  latest_redis=$(hex64 1)
  fake_image bitnami/postgresql "$latest_postgresql" "$(hex64 b)" "$(hex64 c)" "$(hex64 d)" 18.6.0
  fake_image bitnami/redis "$latest_redis" "$(hex64 2)" "$(hex64 3)" "$(hex64 4)" 8.10.2

  cat >"$dir/chart/Chart.yaml" <<CHART
apiVersion: v2
name: peekaping
version: 1.0.0
dependencies:
- name: redis
  version: $3
  repository: oci://registry-1.docker.io/bitnamicharts
  condition: redis.enabled

- name: postgresql
  version: $1
  repository: oci://registry-1.docker.io/bitnamicharts
  condition: postgresql.enabled
CHART
  cat >"$dir/chart/values.yaml" <<VALUES
postgresql:
  enabled: false
  image:
    digest: sha256:$latest_postgresql # PostgreSQL 18.6.0

redis:
  enabled: false
  image:
    digest: sha256:$latest_redis # Redis 8.10.2
VALUES
  cp "$dir/chart/Chart.yaml" "$dir/chart/values.yaml" "$dir/before/"
}

# update_deps [args...]: runs update-chart-deps.sh on the chart in $work/deps
# against the fake registry. Its output goes to $work/deps/out and its exit
# status to $deps_status.
update_deps() {
  deps_status=0
  PATH="$work/deps/bin:$PATH" FAKE_REGISTRY="$work/deps/registry" FAKE_HELM="$work/deps/helm" \
    "$root/scripts/update-chart-deps.sh" --dir "$work/deps/chart" "$@" >"$work/deps/out" 2>&1 || deps_status=$?
}

# deps_outcome <exit status> [pattern...]: the last update_deps run exited with
# that status, and its output has a line matching each pattern (grep -E)
deps_outcome() {
  local status=$1 pattern
  shift
  if [ "$deps_status" != "$status" ]; then
    echo "       update-chart-deps.sh exited with status $deps_status, expected $status:"
    sed 's/^/       /' "$work/deps/out"
    return 1
  fi
  for pattern in "$@"; do
    if ! grep -Eq -- "$pattern" "$work/deps/out"; then
      echo "       no line matching '$pattern' in the output:"
      sed 's/^/       /' "$work/deps/out"
      return 1
    fi
  done
}

# deps_chart_version <name>: the version of that dependency in the chart's Chart.yaml
deps_chart_version() {
  awk -v name="$1" '
    $1 == "-" && $2 == "name:" { current = $3; next }
    /^  version:/ && current == name { print $2; exit }
  ' "$work/deps/chart/Chart.yaml"
}

# deps_unchanged: Chart.yaml and values.yaml are as deps_setup wrote them
deps_unchanged() {
  local file status=0
  for file in Chart.yaml values.yaml; do
    if ! cmp -s "$work/deps/before/$file" "$work/deps/chart/$file"; then
      echo "       $file changed:"
      diff "$work/deps/before/$file" "$work/deps/chart/$file" | sed 's/^/       /' || true
      status=1
    fi
  done
  return $status
}

# deps_refuses_new_major_chart: with postgresql 19.0.0 out and the chart on
# 18.12.14, the report calls it a new major version, not behind, and exits 1,
# like it does for an image. --write exits 2 and writes nothing, not even the
# redis chart that is only a minor version behind.
deps_refuses_new_major_chart() {
  deps_setup 18.12.14 19.0.0 28.3.1 28.4.0
  update_deps
  deps_outcome 1 '^postgresql chart: +new major 19 \(pinned 18\)' '^redis chart: +behind' || return 1
  update_deps --write
  deps_outcome 2 'new major' || return 1
  deps_unchanged
}

# deps_updates_minor_chart: a subchart that is behind by a minor version is
# reported as behind, and --write sets its version
deps_updates_minor_chart() {
  deps_setup 18.12.14 18.13.0 28.3.1 28.3.1
  update_deps
  deps_outcome 1 '^postgresql chart: +behind \(repo 18\.12\.14, latest 18\.13\.0\)' '^redis chart: +current' || return 1
  if grep -q ' new major ' "$work/deps/out"; then
    echo "       the report calls a minor version a new major version:"
    sed 's/^/       /' "$work/deps/out"
    return 1
  fi
  update_deps --write
  deps_outcome 0 'Set the postgresql chart' || return 1
  if [ "$(deps_chart_version postgresql)" != 18.13.0 ] || [ "$(deps_chart_version redis)" != 28.3.1 ]; then
    echo "       Chart.yaml has postgresql $(deps_chart_version postgresql) and redis $(deps_chart_version redis), expected 18.13.0 and 28.3.1"
    return 1
  fi
}

# deps_all_current: when the subcharts and the images are the latest, the
# report exits 0
deps_all_current() {
  deps_setup 18.12.14 18.12.14 28.3.1 28.3.1
  update_deps
  deps_outcome 0 '^postgresql chart: +current' '^redis chart: +current' '^postgresql image: +current' '^redis image: +current'
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

# ingress_routes <rendered file> <path> <service>: the rendered Ingress sends
# the path to the service
ingress_routes() {
  local routed
  routed=$(awk '
    /^---/ { kind = ""; current = "" }
    /^kind: / { kind = $2 }
    kind == "Ingress" && $1 == "-" && $2 == "path:" { current = $3 }
    kind == "Ingress" && $1 == "name:" && current != "" { print current " " $2; current = "" }
  ' "$1")
  grep -qxF -- "$2 $3" <<<"$routed" && return 0
  echo "       no '$2 $3' among the Ingress routes:"
  sed 's/^/       /' <<<"$routed"
  return 1
}

# virtualservice_routes <rendered file> <prefix> <host>: the rendered
# VirtualService sends the URI prefix to the host
virtualservice_routes() {
  local routed
  routed=$(awk '
    /^---/ { kind = ""; prefix = "" }
    /^kind: / { kind = $2 }
    kind == "VirtualService" && $1 == "prefix:" { prefix = $2 }
    kind == "VirtualService" && $1 == "host:" && prefix != "" { print prefix " " $2; prefix = "" }
  ' "$1")
  grep -qxF -- "$2 $3" <<<"$routed" && return 0
  echo "       no '$2 $3' among the VirtualService routes:"
  sed 's/^/       /' <<<"$routed"
  return 1
}

echo "# Updating the dependencies"
check "update-chart-deps.sh calls a new major version of a subchart a new major version, and --write refuses it" \
  deps_refuses_new_major_chart || true
check "update-chart-deps.sh --write updates a subchart that is behind by a minor version" \
  deps_updates_minor_chart || true
check "update-chart-deps.sh reports current subcharts and images as current" deps_all_current || true

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
    check "default values: the web pod binds port 80 as non-root: the sysctl, and /tmp and the web root writable" \
      web_binds_port_80 "$work/default.yaml" || true
    check "default values: the api waits for the migrations, the probe and web don't" \
      backend_components_wait "$work/default.yaml" 0xfurai/peekaping-migrate:latest || true
    check "default values: the api and probe pods have no AppArmor annotation" \
      apparmor_off "$work/default.yaml" || true
    check "default values: the api and probe pods get 35 seconds to stop" \
      grace_periods "$work/default.yaml" || true
    # Cluster mode on SQL storage is the api replicas and the probes: no process
    # of the old layout is left to run
    check "default values: no producer, worker or ingester is rendered" \
      no_old_components "$work/default.yaml" || true
    check "default values: the probe runs the probe image, 3 replicas" \
      probe_runs "$work/default.yaml" 0xfurai/peekaping-probe:latest 3 || true
    # PEEKAPING_URL is plain http inside the cluster, which the probe only accepts
    # with PROBE_ALLOW_INSECURE
    check "default values: the probe finds the api through its Service, on the Service's port" \
      probe_env_is "$work/default.yaml" PEEKAPING_URL "http://$release-api.default.svc:8034" || true
    check "default values: the probe allows its plain http address to the api" \
      probe_env_is "$work/default.yaml" PROBE_ALLOW_INSECURE true || true
    check "default values: the probe answers /ready on 0.0.0.0:8081 and the readiness probe asks it there" \
      probe_ready "$work/default.yaml" 8081 || true
    check "default values: the probe reads the ConfigMap but not the config secret, which holds the database password" \
      probe_env_from_is_config_map_only "$work/default.yaml" || true
    check "default values: the chart makes the Secret $release-probe-token, with a token of at least 32 characters" \
      probe_token_secret "$work/default.yaml" "$release-probe-token" || true
    check "default values: the api reads PROBE_BOOTSTRAP_TOKEN from that Secret" \
      secret_ref_is "$work/default.yaml" api-deploy "$release-api" PROBE_BOOTSTRAP_TOKEN "$release-probe-token" PROBE_BOOTSTRAP_TOKEN || true
    check "default values: the probe reads PROBE_TOKEN from the same Secret and key" \
      secret_ref_is "$work/default.yaml" probe-deploy "$release-probe" PROBE_TOKEN "$release-probe-token" PROBE_BOOTSTRAP_TOKEN || true
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
    # Failed tries back off 10 s, 20 s, 40 s and so on, so 6 tries last long
    # enough for the bundled PostgreSQL to come back from a restart that the
    # same upgrade causes.
    check "default values: the migrate Job retries 6 times" job_retries "$work/default.yaml" migrate-job 6 || true
    # The api trusts X-Forwarded-For only from these addresses: loopback and the
    # private networks, which is where the ingress or gateway reaches it from.
    # Without the setting the login lockout would count every client as the
    # ingress.
    check "default values: the ConfigMap sets TRUSTED_PROXIES to loopback and the private ranges" \
      config_sets "$work/default.yaml" TRUSTED_PROXIES \
      "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7" || true
  fi
  # The chart has no volume for a SQLite file, so each pod would get its own database.
  # The bundle image runs single mode on SQLite, so the message points to it.
  for db_type in sqlite SQLite SQLITE; do
    check "config.env.DB_TYPE=$db_type: rendering fails, saying the chart supports PostgreSQL only" \
      render_refused "refused-$db_type" "supports PostgreSQL only" --set "config.env.DB_TYPE=$db_type" || true
    check "config.env.DB_TYPE=$db_type: the message points to the bundle image" \
      grep -qF "0xfurai/peekaping-bundle-sqlite" "$work/refused-$db_type.log" || true
    check "config.env.DB_TYPE=$db_type: the message links to the SQLite docs page" \
      grep -qF "https://docs.peekaping.com/self-hosting/docker-with-sqlite" "$work/refused-$db_type.log" || true
  done
  # MongoDB storage was removed. The message is the one every binary stops with
  # (config.ErrMongoStorageRemoved), for a chart that supports PostgreSQL only.
  for db_type in mongo mongodb MongoDB MONGO; do
    check "config.env.DB_TYPE=$db_type: rendering fails, saying MongoDB storage was removed" \
      render_refused "refused-$db_type" "MongoDB storage was removed" --set "config.env.DB_TYPE=$db_type" || true
    check "config.env.DB_TYPE=$db_type: the message points to bun db import-mongo" \
      grep -qF 'with "bun db import-mongo"' "$work/refused-$db_type.log" || true
    check "config.env.DB_TYPE=$db_type: the message links to the guide for moving to PostgreSQL" \
      grep -qF "https://docs.peekaping.com/self-hosting/docker-with-mongo#move-to-sqlite-or-postgresql" "$work/refused-$db_type.log" || true
  done
  if check "config.env.TRUSTED_PROXIES=203.0.113.0/24 renders" render trusted-proxies --set config.env.TRUSTED_PROXIES=203.0.113.0/24; then
    check "config.env.TRUSTED_PROXIES=203.0.113.0/24: the ConfigMap sets TRUSTED_PROXIES to it" \
      config_sets "$work/trusted-proxies.yaml" TRUSTED_PROXIES 203.0.113.0/24 || true
  fi
  # A Secret of the admin's own: the chart makes none, and the api and the probe read that one
  if check "probe.token.existingSecret renders" render probe-secret \
    --set probe.token.existingSecret=my-probe-token --set probe.token.existingSecretKey=token; then
    check "probe.token.existingSecret: the chart makes no Secret" \
      test -z "$(probe_token_secret_data "$work/probe-secret.yaml")" || true
    check "probe.token.existingSecret: the api reads PROBE_BOOTSTRAP_TOKEN from that Secret and key" \
      secret_ref_is "$work/probe-secret.yaml" api-deploy "$release-api" PROBE_BOOTSTRAP_TOKEN my-probe-token token || true
    check "probe.token.existingSecret: the probe reads PROBE_TOKEN from that Secret and key" \
      secret_ref_is "$work/probe-secret.yaml" probe-deploy "$release-probe" PROBE_TOKEN my-probe-token token || true
  fi
  if check "probe.replicaCount=5 and api.service.port=9000 render" render probe-scaled \
    --set probe.replicaCount=5 --set api.service.port=9000; then
    check "probe.replicaCount=5: the probe runs 5 replicas" \
      probe_runs "$work/probe-scaled.yaml" 0xfurai/peekaping-probe:latest 5 || true
    check "api.service.port=9000: the probe finds the api on port 9000" \
      probe_env_is "$work/probe-scaled.yaml" PEEKAPING_URL "http://$release-api.default.svc:9000" || true
  fi
  if check "probe.sysctls=[] renders" render probe-no-sysctls --set-json 'probe.sysctls=[]'; then
    check "probe.sysctls=[]: the probe pod sets no sysctls" \
      test -z "$(pod_sysctls_of "$work/probe-no-sysctls.yaml" probe-deploy)" || true
  fi
  check "default values: the probe pod sets the sysctl that lets it send ICMP echo requests as non-root" \
    grep -Fqx "net.ipv4.ping_group_range=0 2147483647" <<<"$(pod_sysctls_of "$work/default.yaml" probe-deploy)" || true
  # The probe protocol (/probe/v1/) is served by the api, like /api and /socket.io, so
  # the Ingress or the Istio VirtualService has to send it there and not to the web app
  if check "ingress.enabled=true renders" render ingress --set ingress.enabled=true --set ingress.host=peekaping.example.com; then
    check "ingress.enabled=true: the Ingress routes /probe to the api" \
      ingress_routes "$work/ingress.yaml" /probe "$release-api" || true
  fi
  if check "istio.virtualservice.enabled=true renders" render istio \
    --set ingress.enabled=true --set ingress.host=peekaping.example.com --set istio.virtualservice.enabled=true; then
    check "istio.virtualservice.enabled=true: the VirtualService routes /probe/ to the api" \
      virtualservice_routes "$work/istio.yaml" /probe/ "$release-api.default.svc.cluster.local" || true
  fi
  if check "bundled PostgreSQL and Redis render" render bundled --set postgresql.enabled=true --set redis.enabled=true; then
    check "bundled PostgreSQL runs an image pinned by digest" pinned_by_digest "$work/bundled.yaml" postgresql || true
    check "bundled Redis runs an image pinned by digest" pinned_by_digest "$work/bundled.yaml" redis || true
    check "bundled PostgreSQL and Redis: the seed init container runs the web image" \
      web_matches_seed "$work/bundled.yaml" || true
    check "bundled PostgreSQL and Redis: the api waits for the migrations, the probe and web don't" \
      backend_components_wait "$work/bundled.yaml" 0xfurai/peekaping-migrate:latest || true
  fi
  if check "web.image.tag=0.0.47 renders" render tagged --set web.image.tag=0.0.47; then
    check "web.image.tag=0.0.47: the seed init container runs the web image at that tag" \
      web_matches_seed "$work/tagged.yaml" 0xfurai/peekaping-web:0.0.47 || true
    check "web.image.tag=0.0.47: the web container and its seed init container change tag, nothing else does" \
      tag_overrides "$work/tagged.yaml" 0xfurai/peekaping-web 0.0.47 latest 2 || true
  fi
  if check "web.sysctls=[] renders" render web-no-sysctls --set-json 'web.sysctls=[]'; then
    check "web.sysctls=[]: the web pod sets no sysctls" \
      test -z "$(pod_sysctls_of "$work/web-no-sysctls.yaml" web-deploy)" || true
  fi
  if check "migrate.image.tag=0.0.47 renders" render migrate-tagged --set migrate.image.tag=0.0.47; then
    check "migrate.image.tag=0.0.47: the wait-for-migrations init containers run the migrate image at that tag" \
      backend_components_wait "$work/migrate-tagged.yaml" 0xfurai/peekaping-migrate:0.0.47 || true
    check "migrate.image.tag=0.0.47: the migrate Job and the api's wait-for-migrations init container change tag, nothing else does" \
      tag_overrides "$work/migrate-tagged.yaml" 0xfurai/peekaping-migrate 0.0.47 latest 2 || true
  fi
  if check "apparmor.enabled=true for the api and probe renders" render apparmor \
    --set api.apparmor.enabled=true --set probe.apparmor.enabled=true; then
    check "api.apparmor.enabled=true: the pod template annotates $release-api and wait-for-migrations" \
      apparmor_covers_init "$work/apparmor.yaml" api || true
    # The probe has no init container, so its annotation names the one container
    check "probe.apparmor.enabled=true: the pod template annotates $release-probe" \
      grep -Fqx "container.apparmor.security.beta.kubernetes.io/$release-probe: runtime/default" \
      <<<"$(apparmor_annotations_of "$work/apparmor.yaml" probe-deploy)" || true
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
    check "the package: the api waits for the migrations with the migrate image at 9.9.9, the probe and web don't" \
      backend_components_wait "$work/package-default.yaml" 0xfurai/peekaping-migrate:9.9.9 || true
  fi
  # A component's image.tag overrides the appVersion for the images of that
  # component only: one container each for the api and probe, two for web
  # (the container and seed-web-root), and two for migrate (the Job and the
  # api's wait-for-migrations init container)
  for component in api:1 probe:1 web:2 migrate:2; do
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
