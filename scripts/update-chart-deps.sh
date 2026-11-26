#!/usr/bin/env bash
# Checks whether the Helm chart's bundled PostgreSQL and Redis are up to date,
# and updates them.
#
# The chart depends on Bitnami's postgresql and redis subcharts (charts/Chart.yaml)
# and runs their images from pinned digests (postgresql.image.digest and
# redis.image.digest in charts/values.yaml). Bitnami's free repo has only the
# `latest` tag, so the digest of `latest` is the version. For each of the two
# subcharts and the two images the script prints `current`, or `behind` with the
# version or digest in the repo and the latest one, or, for a subchart or an
# image whose latest has another major version than the pinned one, `new major`.
# The latest are the newest chart in oci://registry-1.docker.io/bitnamicharts
# and the multi-arch digest of registry-1.docker.io/bitnami/<name>:latest, which
# must cover linux/amd64 and linux/arm64.
#
# Usage:
#   scripts/update-chart-deps.sh [--dir <chart dir>]            # report
#   scripts/update-chart-deps.sh [--dir <chart dir>] --write    # update
#
# Report: exits 0 when everything is current and 1 when anything is behind or
# has a new major version. It exits 2 when it can't tell, for example when a
# registry is unreachable.
# --write: rewrites the version lines in Chart.yaml and the digest lines in
# values.yaml that are behind (adding a digest line to an image that has none),
# and nothing else, then reads the two files again. It exits 0 when every line
# it set now holds the latest value, and 2 with a message when one doesn't, for
# example because the file is laid out in a way the script doesn't know. It
# refuses, and writes nothing, when the latest subchart or image has another
# major version than the pinned one (Bitnami's major versions are breaking, and
# a new PostgreSQL major needs a dump and restore, so move to it by hand) and
# exits 2.
# --dir: the folder with Chart.yaml and values.yaml, charts/ by default.
#
# Needs bash, helm (3.13 or later), curl and jq, and network access to
# registry-1.docker.io and auth.docker.io. It reads the images through the
# registry's HTTP API, so it needs no Docker daemon.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
dir=$root/charts
write=false
while [ $# -gt 0 ]; do
  case $1 in
    --write) write=true ;;
    --dir) dir=${2:?usage: update-chart-deps.sh [--write] [--dir <chart dir>]}; shift ;;
    *) echo "usage: update-chart-deps.sh [--write] [--dir <chart dir>]" >&2; exit 2 ;;
  esac
  shift
done
chart_file=$dir/Chart.yaml
values_file=$dir/values.yaml

die() { echo "update-chart-deps.sh: $*" >&2; exit 2; }

for tool in helm curl jq; do
  command -v "$tool" >/dev/null || die "$tool is not installed"
done
[ -f "$chart_file" ] || die "$chart_file not found"
[ -f "$values_file" ] || die "$values_file not found"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# What is behind, as lines the --write step applies: "chart <name> <version>"
# and "image <name> <digest> <label>"
updates=$work/updates
: >"$updates"
refusals=$work/refusals
: >"$refusals"

charts_repo=oci://registry-1.docker.io/bitnamicharts
registry=https://registry-1.docker.io/v2
accept='application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json'

# chart_version <name>: the version of that dependency in Chart.yaml
chart_version() {
  awk -v name="$1" '
    $1 == "-" && $2 == "name:" { current = $3; next }
    /^  version:/ && current == name { print $2; exit }
  ' "$chart_file"
}

# image_digest <name>: <name>.image.digest in values.yaml
image_digest() {
  awk -v name="$1" '
    /^[^ #]/ { in_chart = ($0 == name ":"); in_image = 0; next }
    in_chart && /^  [^ #]/ { in_image = ($0 ~ /^  image:[ ]*$/); next }
    in_chart && in_image && /^    digest:/ { print $2; exit }
  ' "$values_file"
}

# set_chart_version <name> <version>: rewrites that dependency's version line
set_chart_version() {
  awk -v name="$1" -v version="$2" '
    $1 == "-" && $2 == "name:" { current = $3 }
    /^  version:/ && current == name { print "  version: " version; next }
    { print }
  ' "$chart_file" >"$work/Chart.yaml"
  cat "$work/Chart.yaml" >"$chart_file"
}

# set_image_digest <name> <digest> <label>: sets <name>.image.digest, with the
# version it is as a comment. It replaces the line, or adds it: under an
# existing `image:`, or in a new `image:` after `enabled:`.
set_image_digest() {
  local mode
  mode=$(awk -v name="$1" '
    /^[^ #]/ { in_chart = ($0 == name ":"); in_image = 0; next }
    in_chart && /^  [^ #]/ { in_image = ($0 ~ /^  image:[ ]*$/); if (in_image) image = 1; if ($0 ~ /^  enabled:/) enabled = 1 }
    in_chart && in_image && /^    digest:/ { digest = 1 }
    END { print digest ? "replace" : image ? "after_image" : enabled ? "after_enabled" : "none" }
  ' "$values_file")
  [ "$mode" != none ] || die "$values_file has no $1.enabled or $1.image to put the digest next to, add $1.image.digest by hand"
  awk -v name="$1" -v mode="$mode" -v line="    digest: $2 # $3" '
    /^[^ #]/ { in_chart = ($0 == name ":"); in_image = 0 }
    in_chart && /^  [^ #]/ { in_image = ($0 ~ /^  image:[ ]*$/) }
    mode == "replace" && in_chart && in_image && /^    digest:/ { print line; next }
    { print }
    mode == "after_image" && in_chart && in_image && /^  image:/ { print line }
    mode == "after_enabled" && in_chart && /^  enabled:/ { print "  image:"; print line }
  ' "$values_file" >"$work/values.yaml"
  cat "$work/values.yaml" >"$values_file"
}

# fetch <token> <url> [curl args...]: a registry GET
fetch() {
  curl -fsSL --max-time 60 --retry 2 -H "Authorization: Bearer $1" -H "Accept: $accept" "${@:3}" "$2"
}

# token <repository>: an anonymous pull token for the repository on Docker Hub
token() {
  curl -fsS --max-time 60 --retry 2 \
    "https://auth.docker.io/token?service=registry.docker.io&scope=repository:$1:pull" | jq -er .token
}

# version_of <repository> <token> <digest>: the version the image at <digest>
# says it is, from its org.opencontainers.image.version label. <digest> may be
# a multi-arch index, then the linux/amd64 image is read.
version_of() {
  local manifest platform config version
  manifest=$(fetch "$2" "$registry/$1/manifests/$3") || die "can't read $1@$3"
  platform=$(jq -r '[(.manifests // [])[] | select(.platform.os == "linux" and .platform.architecture == "amd64")][0].digest // empty' <<<"$manifest")
  if [ -n "$platform" ]; then
    manifest=$(fetch "$2" "$registry/$1/manifests/$platform") || die "can't read $1@$platform"
  fi
  config=$(jq -r '.config.digest // empty' <<<"$manifest")
  [ -n "$config" ] || die "$1@$3 has no image config"
  version=$(fetch "$2" "$registry/$1/blobs/$config" | jq -r '.config.Labels["org.opencontainers.image.version"] // empty') ||
    die "can't read the config of $1@$3"
  [ -n "$version" ] || die "$1@$3 has no org.opencontainers.image.version label"
  echo "$version"
}

# short <sha256:digest>: the digest cut to 12 hex characters
short() { echo "${1:0:19}"; }

# report <what> <current|behind> <text>
report() { printf '%-17s %s\n' "$1" "$2${3:+ ($3)}"; }

# check_chart <name>: compares the dependency in Chart.yaml with the newest
# chart. A newest chart with another major version is a refusal, like an image
# with one: Bitnami's major versions are breaking.
check_chart() {
  local repo latest
  repo=$(chart_version "$1")
  [ -n "$repo" ] || die "no $1 dependency in $chart_file"
  latest=$(helm show chart "$charts_repo/$1" 2>"$work/helm.err" | sed -n 's/^version: *//p') ||
    die "can't read the latest $1 chart from $charts_repo: $(tail -n 1 "$work/helm.err")"
  [ -n "$latest" ] || die "the latest $1 chart in $charts_repo has no version"
  if [ "$repo" = "$latest" ]; then
    report "$1 chart:" current "$repo"
    return
  fi
  if [ "${repo%%.*}" != "${latest%%.*}" ]; then
    report "$1 chart:" "new major ${latest%%.*} (pinned ${repo%%.*}): --write refuses it, a ticket of its own"
    echo "       repo $repo, latest $latest: move to it by hand, after reading the chart's upgrade notes, since a new major version can change the values and the data layout" |
      tee -a "$refusals"
    return
  fi
  report "$1 chart:" behind "repo $repo, latest $latest"
  echo "chart $1 $latest" >>"$updates"
}

# check_image <name> <label>: compares <name>.image.digest in values.yaml with
# the digest of bitnami/<name>:latest
check_image() {
  local name=$1 label=$2 repository=bitnami/$1 repo latest tok body headers platform
  local repo_version latest_version
  repo=$(image_digest "$name")
  # No digest means the image is not pinned yet and floats with `latest`
  [[ -z $repo || $repo =~ ^sha256:[0-9a-f]{64}$ ]] || die "$name.image.digest in $values_file is not a sha256 digest: '$repo'"

  tok=$(token "$repository") || die "can't get a token for $repository from auth.docker.io"
  headers=$work/headers
  body=$(fetch "$tok" "$registry/$repository/manifests/latest" -D "$headers") || die "can't read $repository:latest"
  latest=$(awk 'tolower($1) == "docker-content-digest:" { gsub(/\r/, ""); print $2; exit }' "$headers")
  [[ $latest =~ ^sha256:[0-9a-f]{64}$ ]] || die "$repository:latest has no digest"
  for platform in amd64 arm64; do
    jq -e --arg arch "$platform" '[(.manifests // [])[] | select(.platform.os == "linux" and .platform.architecture == $arch)] | length > 0' \
      <<<"$body" >/dev/null || die "$repository:latest ($latest) has no linux/$platform image, so it can't be pinned"
  done
  latest_version=$(version_of "$repository" "$tok" "$latest")

  if [ "$repo" = "$latest" ]; then
    report "$name image:" current "$(short "$repo"), $label $latest_version"
    return
  fi
  if [ -z "$repo" ]; then
    report "$name image:" behind "repo has no digest, latest $(short "$latest") is $label $latest_version"
    echo "image $name $latest $label $latest_version" >>"$updates"
    return
  fi
  repo_version=$(version_of "$repository" "$tok" "$repo")
  if [ "${repo_version%%.*}" != "${latest_version%%.*}" ]; then
    report "$name image:" "new major ${latest_version%%.*} (pinned ${repo_version%%.*}): --write refuses it, a ticket of its own"
    echo "       repo $(short "$repo") is $label $repo_version, latest $(short "$latest") is $label $latest_version: move to it by hand, with a dump and restore of the data" |
      tee -a "$refusals"
    return
  fi
  report "$name image:" behind "repo $(short "$repo") is $label $repo_version, latest $(short "$latest") is $label $latest_version"
  echo "image $name $latest $label $latest_version" >>"$updates"
}

check_chart postgresql
check_chart redis
check_image postgresql PostgreSQL
check_image redis Redis

if ! $write; then
  [ -s "$updates" ] && exit 1
  [ -s "$refusals" ] && exit 1
  exit 0
fi

if [ -s "$refusals" ]; then
  echo "Not writing anything: a new major version needs a decision of its own." >&2
  exit 2
fi
if [ ! -s "$updates" ]; then
  echo "Nothing to update."
  exit 0
fi
# Each line is written and then read back from the file, because the rewrite
# changes nothing when the file isn't laid out the way it expects, and a
# message that says "Set" must be true.
unwritten=0
while read -r kind name value label; do
  case $kind in
    chart)
      set_chart_version "$name" "$value"
      now=$(chart_version "$name")
      what="the $name chart in $chart_file"
      done_message="Set $what to $value"
      ;;
    image)
      set_image_digest "$name" "$value" "$label"
      now=$(image_digest "$name")
      what="$name.image.digest in $values_file"
      done_message="Set $what to $(short "$value") ($label)"
      ;;
  esac
  if [ "$now" = "$value" ]; then
    echo "$done_message"
  else
    echo "update-chart-deps.sh: wrote $what, but reading it again gives '${now:-nothing}', not $value" >&2
    unwritten=$((unwritten + 1))
  fi
done <"$updates"
if [ "$unwritten" -gt 0 ]; then
  die "$unwritten line(s) still differ from the latest: check how $chart_file and $values_file are laid out, and update those lines by hand"
fi
