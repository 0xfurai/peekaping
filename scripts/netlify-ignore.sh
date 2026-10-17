#!/bin/sh
# Netlify "Ignore builds" command: skip the deploy when none of the given paths changed.
# Usage: sh scripts/netlify-ignore.sh apps/docs [more paths...]
# Netlify convention: exit 0 = skip the build, exit 1 = build. Any error falls through to building.

cd "$(git rev-parse --show-toplevel)" || exit 1

if [ "${PULL_REQUEST:-false}" = "true" ]; then
    # Deploy Preview: compare the whole PR against the point where it branched off main
    git fetch --quiet --no-tags origin main || exit 1
    base=$(git merge-base HEAD FETCH_HEAD) || exit 1
else
    # Branch/production deploy: compare against the last commit Netlify built.
    # Without a build cache CACHED_COMMIT_REF equals COMMIT_REF, so build to be safe.
    base="${CACHED_COMMIT_REF:-}"
    if [ -z "$base" ] || [ "$base" = "${COMMIT_REF:-}" ]; then
        exit 1
    fi
fi

if git diff --quiet "$base" HEAD -- "$@"; then
    echo "No changes in $* since $base, skipping build"
    exit 0
fi

echo "Changes found in $*, building"
exit 1
