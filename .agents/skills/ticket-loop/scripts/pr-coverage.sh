#!/usr/bin/env bash
# Print what Codecov found on a PR as JSON: patch and project coverage, and per
# changed file the head line numbers it saw missed or only partly covered. The
# numbers are from the last upload, so they can lag the head commit.
#
# Usage: pr-coverage.sh <pr>
set -euo pipefail

[ $# -eq 1 ] || { echo "usage: pr-coverage.sh <pr>" >&2; exit 2; }
repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)

# The repo is public, so Codecov's API needs no token. Its line coverage codes
# are 0 hit, 1 miss, 2 partial.
curl -fsS "https://api.codecov.io/api/v2/github/${repo%/*}/repos/${repo#*/}/compare/?pullid=$1" |
    jq --arg pr "$1" '{
        pr: ($pr | tonumber),
        base: .base_commit, head: .head_commit,
        patch: (.totals.patch // {} | {coverage, lines, misses, partials}),
        project: {base: .totals.base.coverage, head: .totals.head.coverage},
        files: [.files[]
          | select(.totals.patch != null and (.totals.patch.misses + .totals.patch.partials) > 0)
          | {file: .name.head, patch: .totals.patch.coverage,
             missed: [.lines[] | select(.added and .coverage.head == 1) | .number.head],
             partial: [.lines[] | select(.added and .coverage.head == 2) | .number.head]}]
      }'
