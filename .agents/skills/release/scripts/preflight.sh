#!/usr/bin/env bash
# Check that origin/main is ready to release a version and print what the
# release will contain. Exits 1 on a blocker; notes and warnings don't fail.
#
# Usage:
#   preflight.sh <X.Y.Z>
#
# Reads origin/main, not the checkout, so it works from any worktree. Run from
# inside the repository, with `gh` signed in.
set -euo pipefail

version=${1:?usage: preflight.sh X.Y.Z}
blockers=0

block() { printf 'BLOCK  %s\n' "$*"; blockers=$((blockers + 1)); }
warn() { printf 'WARN   %s\n' "$*"; }
note() { printf 'note   %s\n' "$*"; }
ok() { printf 'ok     %s\n' "$*"; }

git fetch --quiet --tags origin main
head=$(git rev-parse origin/main)
last=$(git tag -l | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)

printf 'Release %s -> %s from origin/main %s\n\n' "${last:-none}" "$version" "$(git rev-parse --short "$head")"

# Version and tag
if ! [[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    block "'$version' is not X.Y.Z (tags have no v prefix; release candidates, X.Y.Z-rc.N, need no preflight)"
elif [ "$version" = "$last" ] || [ "$(printf '%s\n%s\n' "$last" "$version" | sort -V | tail -1)" != "$version" ]; then
    block "$version is not newer than the last release, $last"
elif git ls-remote --exit-code --tags origin "refs/tags/$version" >/dev/null 2>&1; then
    block "tag $version already exists on origin"
else
    ok "$version is new and follows $last"
fi

# The previous release's bump should still be what main carries
go_version=$(git show origin/main:apps/server/internal/version/version.go | sed -n 's/.*Version = "\(.*\)".*/\1/p')
pkg_version=$(git show origin/main:apps/web/package.json | sed -n 's/.*"version": "\(.*\)".*/\1/p' | head -1)
ts_version=$(git show origin/main:apps/web/src/version.ts | sed -n 's/.*VERSION = "\(.*\)".*/\1/p')
if [ "$go_version" = "$last" ] && [ "$pkg_version" = "$last" ] && [ "$ts_version" = "$last" ]; then
    ok "version files on main all say $last"
else
    warn "version files on main disagree with $last: version.go=$go_version package.json=$pkg_version version.ts=$ts_version"
fi

# CI on main. The Release workflow runs no unit tests of its own, only e2e.
for workflow in "Test and Lint" "Playwright Tests"; do
    run=$(gh run list --branch main --workflow "$workflow" --limit 1 \
        --json status,conclusion,headSha,url \
        --jq '.[0] | select(. != null) | [.status, .conclusion, .headSha, .url] | join("|")')
    if [ -z "$run" ]; then
        warn "no '$workflow' run on main"
        continue
    fi
    # Not tabs: read would collapse the empty conclusion of a run in progress
    IFS='|' read -r status conclusion sha url <<<"$run"
    behind=$(git rev-list --count "$sha..origin/main" 2>/dev/null || echo '?')
    if [ "$status" != completed ]; then
        block "'$workflow' is still $status on $(git rev-parse --short "$sha"): $url"
    elif [ "$conclusion" != success ]; then
        block "'$workflow' $conclusion on $(git rev-parse --short "$sha"): $url"
    else
        # Both workflows are path-filtered, so main may be ahead of their last run.
        ok "'$workflow' passed on $(git rev-parse --short "$sha"), $behind commit(s) behind main"
    fi
done

# Changelog
changelog=$(git show origin/main:CHANGELOG.md)
unreleased=$(printf '%s\n' "$changelog" | awk '/^## \[Unreleased\]/{f=1; next} /^## \[/{if (f) exit} f')
counts=$(printf '%s\n' "$unreleased" | awk '/^### /{s=$2} /^- /{n[s]++} END{for (s in n) printf "%s %d, ", s, n[s]}' | sed 's/, $//')
if [ -z "$counts" ]; then
    block "[Unreleased] in CHANGELOG.md has no entries"
else
    ok "[Unreleased] entries: $counts"
fi
# The workflow copies [Unreleased] with awk, which drops any line starting with # except "### "
if printf '%s\n' "$unreleased" | grep -E '^#' | grep -vqE '^### '; then
    warn "[Unreleased] has a heading other than ### Added/Changed/Fixed; the workflow drops it"
fi

# PRs merged since the last release that [Unreleased] doesn't mention
echo
echo "Merged since $last, not mentioned in [Unreleased] by PR number or ticket key:"
skipped=0
unmentioned=0
# Walk every commit, not the first parent only: a "Merge branch 'main'" that joined a local main
# to origin/main hides the squash-merged PRs it brought in. A "Merge pull request #N" is one PR,
# so its own commits are left out and the merge commit stands for them.
hidden=$(git log "$last..origin/main" --merges --grep='^Merge pull request #' --format='%H' |
    while read -r merge; do git rev-list "$merge^1..$merge^2"; done)
while IFS='|' read -r sha subject; do
    case $subject in
        "chore: bump version"*) continue ;;
        "Merge pull request #"*) ;;
        "Merge "*) continue ;;
    esac
    if printf '%s\n' "$hidden" | grep -qx "$sha"; then continue; fi
    # Nothing a user of Peekaping sees: tests, CI, chores, builds and the agent skills (scope ticket)
    if printf '%s' "$subject" | grep -Eq '(^|[] ])(test|ci|chore|build)(\([^)]*\))?:|\(ticket\):|^Merge pull request #[0-9]+ from [^/]+/(test|ci|chore|build)/'; then
        skipped=$((skipped + 1))
        continue
    fi
    pr=$(printf '%s' "$subject" | sed -n -e 's/.*(#\([0-9][0-9]*\))$/\1/p' -e 's/^Merge pull request #\([0-9][0-9]*\) from .*/\1/p')
    # The ticket key leads the subject (#<n>); older subjects carry PP-<n>
    key=$(printf '%s' "$subject" | grep -oE '^#[0-9]+|PP-[0-9]+' | head -1 || true)
    if [ -n "$pr" ] && printf '%s\n' "$unreleased" | grep -qE "#$pr([^0-9]|\$)"; then continue; fi
    if [ -n "$key" ] && printf '%s\n' "$unreleased" | grep -qE "$key([^0-9]|\$)"; then continue; fi
    printf '  %s  %s\n' "${sha:0:8}" "$subject"
    unmentioned=$((unmentioned + 1))
done < <(git log "$last..origin/main" --format='%H|%s')
[ "$unmentioned" -gt 0 ] || echo '  none'
echo "  ($skipped test/ci/chore/build commits not checked)"

# Database migrations the release ships
echo
migrations=$(git diff --name-only --diff-filter=A "$last" origin/main | grep -i 'migrations/' || true)
if [ -n "$migrations" ]; then
    note "migrations added since $last; the notes should say what they do to an existing database:"
    printf '%s\n' "$migrations" | sed 's/^/         /'
else
    ok "no migrations added since $last"
fi

echo
if [ "$blockers" -gt 0 ]; then
    echo "Blocked: $blockers blocker(s)."
    exit 1
fi
echo "No blockers. Dispatch: gh workflow run docker-release.yml --ref main -f version=$version"
