#!/usr/bin/env bash
# Renders the Helm chart the way an install does and checks the result: the
# chart lints, it renders with default values and with the bundled PostgreSQL
# and Redis, the web Deployment's seed init container runs the same image as the
# web container (also when web.image.tag is set), and the install notes say
# Peekaping runs in cluster mode and link to the Helm docs page.
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

# image_of <rendered file> <name>: the image of the container or init container
# with that name in the web Deployment
image_of() {
  awk -v name="$2" '
    /^# Source: .*\/web-deploy\.yaml$/ { in_web = 1; next }
    /^---$/ { in_web = 0 }
    in_web && $0 ~ "^ *- name: " name "$" { found = 1; next }
    in_web && found && /^ *image: / { print $2; exit }
  ' "$1"
}

# web_matches_seed <rendered file> [expected image]: the init container that
# seeds the web root copies it out of the image the web container serves
web_matches_seed() {
  local seed web
  seed=$(image_of "$1" seed-web-root)
  web=$(image_of "$1" "$release-web")
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
  fi
  if check "bundled PostgreSQL and Redis render" render bundled --set postgresql.enabled=true --set redis.enabled=true; then
    check "bundled PostgreSQL and Redis: the seed init container runs the web image" \
      web_matches_seed "$work/bundled.yaml" || true
  fi
  if check "web.image.tag=0.0.47 renders" render tagged --set web.image.tag=0.0.47; then
    check "web.image.tag=0.0.47: the seed init container runs the web image at that tag" \
      web_matches_seed "$work/tagged.yaml" 0xfurai/peekaping-web:0.0.47 || true
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
