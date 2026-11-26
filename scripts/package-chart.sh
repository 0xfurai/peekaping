#!/usr/bin/env bash
# Packages the Helm chart for a release. The package's version and appVersion
# are both the release, so the images it installs default to that release (an
# empty image.tag means the appVersion). The PostgreSQL and Redis subcharts are
# inside the package, so installing it needs no `helm dependency build`.
#
# Usage: scripts/package-chart.sh <version> <out dir>
# <version> is the release, such as 0.0.48. Helm requires a SemVer version, so
# there is no leading v. The script writes <out dir>/peekaping-<version>.tgz
# and prints its path. The release workflow pushes that file to
# oci://ghcr.io/<owner>/charts with `helm push`.
#
# Needs helm (3.13 or later) and network access to registry-1.docker.io, where
# the chart's PostgreSQL and Redis dependencies live. It works on a copy of
# charts/ in a temp directory, so the repo gets no charts/charts/*.tgz and no
# Chart.lock.
set -euo pipefail

usage="usage: package-chart.sh <version> <out dir>"
[ $# -eq 2 ] || { echo "$usage" >&2; exit 2; }
version=$1
out=$2

die() { echo "package-chart.sh: $*" >&2; exit 2; }

# SemVer 2.0.0, from https://semver.org
semver='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*))?(\+([0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*))?$'
[[ $version =~ $semver ]] || die "'$version' is not a SemVer version such as 0.0.48 (no leading v)"
command -v helm >/dev/null || die "helm is not installed"

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cp -R "$root/charts" "$work/charts"

mkdir -p "$out"
helm dependency build "$work/charts"
helm lint "$work/charts"
helm package "$work/charts" --version "$version" --app-version "$version" --destination "$out"

package=$out/peekaping-$version.tgz
[ -f "$package" ] || die "helm did not write $package"
echo "$package"
