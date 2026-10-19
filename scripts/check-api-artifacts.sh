#!/bin/sh
set -eu
changes=$(git status --porcelain -- apps/server/docs apps/web/src/api)
if [ -n "$changes" ]; then
  echo "API artifacts are stale; run bun run api:generate and commit the results." >&2
  echo "$changes" >&2
  exit 1
fi
