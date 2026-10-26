#!/usr/bin/env bash
# Wait until a PR stack needs attention, print why, and exit. Meant to run in
# the background so its exit wakes the agent.
#
# Usage: pr-wait.sh [--idle] [--timeout MIN] [--interval SEC] [--ignore CHECK]... <pr>...
#
# First line of output is the outcome, then one status line per PR:
#   ACTION   something new waits on us: a check failed, a comment came in, or
#            CodeRabbit never reviewed a head (a stacked PR needs `@coderabbitai review`).
#            Reported as soon as it appears, while other checks still run.
#   READY    all checks passed, CodeRabbit reviewed every head, nothing waits on us
#   CONFLICT a PR no longer merges cleanly into its base
#   MERGED   a PR was merged or closed during the wait
#   PUSHED   a PR's head moved while idle (someone else pushed)
#   TIMEOUT  none of the above before the deadline
#
# Each item is reported once: the keys of reported items are kept per PR next to
# pr-state.sh's marks, so a wait started while you handle a round wakes you only
# for what came in since. Items you haven't settled still hold back READY.
#
# --idle waits for reviewers after READY, for up to two days: it never reports
# READY, only new comments, failures, conflicts, merges and pushes. --ignore drops a check you've judged
# unrelated (e.g. a known flake); `--ignore CodeRabbit` stops waiting for its review.
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
idle=false
timeout_min=60
interval=60
ignore='[]'
# The graces below count from when a wait first saw a PR's head, kept beside the
# reported keys: a new wait on the same head doesn't start them again, a push does.
# Checks that haven't registered yet on a fresh push look like "no checks".
min_wait=90
# A PR into main with no CI checks counts as busy for this long; after it, the
# PR is taken to touch no CI paths (docs-only changes run no workflow).
ci_grace=$((10 * 60))
# A head CodeRabbit hasn't reviewed counts as busy for this long, which covers
# both its queue and a stacked PR waiting for an `@coderabbitai review` trigger.
coderabbit_grace=$((20 * 60))
# The checks that upload coverage. Codecov posts its statuses after each upload,
# so a failed one counts only once none of these is pending or failed (a failed
# one never uploaded) and it was posted after the last of them finished. One that
# didn't run on the PR (path filters) holds nothing. A held result keeps the PR busy.
coverage_uploads='["test", "import-mongo", "executor"]'

while [ $# -gt 0 ]; do
    case $1 in
    --idle) idle=true; timeout_min=$((2 * 24 * 60)); interval=120; shift ;;
    --timeout) timeout_min=$2; shift 2 ;;
    --interval) interval=$2; shift 2 ;;
    --ignore) ignore=$(jq -c --arg c "$2" '. + [$c]' <<<"$ignore"); shift 2 ;;
    *) break ;;
    esac
done
[ $# -gt 0 ] || { echo "usage: pr-wait.sh [--idle] [--timeout MIN] [--interval SEC] [--ignore CHECK]... <pr>..." >&2; exit 2; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
start=$(date +%s)
deadline=$((start + timeout_min * 60))
seen_dir="$(git rev-parse --path-format=absolute --git-common-dir)/ticket-loop"
mkdir -p "$seen_dir"

status_line() {
    jq -r '"#\(.pr) \(.state) \(.base)<-\(.head) | checks: \(.checks.pending | length) pending, "
        + "\(.checks.failed | length) failed\(if (.checks.failed | length) > 0 then " (" + ([.checks.failed[].name] | join(", ")) + ")" else "" end), "
        + "\(.checks.passed) passed | coderabbit \(.checks.coderabbit) | mergeable \(.mergeable) | todo \(.todo)"' "$1"
}

finish() {
    echo "$1"
    for f in "$tmp"/*.now; do [ -f "$f" ] && status_line "$f"; done
    if [ "$1" = ACTION ]; then
        for f in "$tmp"/*.keys; do
            [ -f "$f" ] || continue
            seen="$seen_dir/$(basename "$f" .keys).seen"
            sort -u "$f" "$seen" -o "$seen" 2>/dev/null || sort -u "$f" -o "$seen"
        done
    fi
    exit 0
}

while :; do
    now=$(date +%s)
    busy=false attention=false new=false merged=false pushed=false conflict=false open=0

    for pr in "$@"; do
        # A failed fetch says nothing about the PR, so it can't end the wait.
        "$here/pr-state.sh" --brief "$pr" >"$tmp/$pr.next" 2>/dev/null || { busy=true; continue; }
        mv "$tmp/$pr.next" "$tmp/$pr.now"
        [ -f "$tmp/$pr.start" ] || cp "$tmp/$pr.now" "$tmp/$pr.start"
        s="$tmp/$pr.now"

        start_state=$(jq -r .state "$tmp/$pr.start")
        state=$(jq -r .state "$s")
        [ "$start_state" = OPEN ] && [ "$state" != OPEN ] && merged=true
        [ "$state" = OPEN ] || continue
        open=$((open + 1))

        head=$(jq -r .headSha "$s")
        [ "$(jq -r .headSha "$tmp/$pr.start")" != "$head" ] && pushed=true
        if [ "$(cat "$seen_dir/$pr.head" 2>/dev/null)" != "$head" ]; then
            echo "$head" >"$seen_dir/$pr.head"
            echo "$now" >"$seen_dir/$pr.since"
        fi
        age=$((now - $(cat "$seen_dir/$pr.since")))
        [ $age -lt $min_wait ] && busy=true
        # The items waiting on us; the ones not reported before end the wait.
        held=$(jq --argjson uploads "$coverage_uploads" '
            .checks.completedAt as $at
            | ([$at[$uploads[]] // empty] | max) as $uploaded
            | ([.checks.pending[], .checks.failed[].name] | any(IN($uploads[])))
              or any(.checks.failed[] | select(.name | startswith("codecov/")); ($at[.name] // "") <= ($uploaded // ""))' "$s")
        $held && busy=true
        jq -r --argjson ignore "$ignore" --argjson held "$held" '
            . as $s
            | [.checks.failed[] | select(.name | IN($ignore[])) | "check:\(.name):\(.url):\($s.headSha)"] as $ignored
            | .keys[]
            | select(IN($ignored[]) | not)
            | select($held and startswith("check:codecov/") | not)' \
            "$s" >"$tmp/$pr.keys"
        # GitHub computes mergeability lazily; UNKNOWN means ask again.
        case $(jq -r .mergeable "$s") in
        CONFLICTING) conflict=true ;;
        UNKNOWN) busy=true ;;
        esac
        jq -e --argjson ignore "$ignore" \
            '[.checks.pending[] | select(. as $n | $ignore | index($n) | not)] | length > 0' \
            "$s" >/dev/null && busy=true
        jq -e '.base == "main" and .checks.ci == 0' "$s" >/dev/null &&
            [ $age -lt $ci_grace ] && busy=true
        if jq -e --argjson ignore "$ignore" '$ignore | index("CodeRabbit") | not' "$s" >/dev/null; then
            case $(jq -r .checks.coderabbit "$s") in
            SUCCESS) ;;
            PENDING | MISSING) [ $age -lt $coderabbit_grace ] && busy=true || echo "coderabbit:$head" >>"$tmp/$pr.keys" ;;
            *) echo "coderabbit:$head" >>"$tmp/$pr.keys" ;;
            esac
        fi
        [ -s "$tmp/$pr.keys" ] && attention=true
        grep -vxF -f "$seen_dir/$pr.seen" "$tmp/$pr.keys" >/dev/null 2>&1 && new=true
        [ -f "$seen_dir/$pr.seen" ] || { [ -s "$tmp/$pr.keys" ] && new=true; }
    done

    $merged && finish MERGED
    [ $open -eq 0 ] && ls "$tmp"/*.now >/dev/null 2>&1 && finish MERGED
    $conflict && finish CONFLICT
    $idle && $pushed && finish PUSHED
    $new && finish ACTION
    $busy || $attention || $idle || finish READY
    [ "$now" -ge "$deadline" ] && finish TIMEOUT
    sleep "$interval"
done
