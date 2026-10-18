#!/usr/bin/env bash
# Post on a PR as the agent. Every body gets pr-state.sh's marker, so the review
# loop knows the reply is ours and the thread now waits on the reviewer.
#
# Usage (body on stdin):
#   pr-reply.sh thread <pr> <comment-id>   reply in a review thread; pass the thread's first
#                                          comment (replyTo in pr-state.sh), replies to replies fail
#   pr-reply.sh comment <pr>               top-level PR comment
#   pr-reply.sh resolve <thread-id>        resolve a review thread (no body)
set -euo pipefail

MARKER='<!-- claude-ticket -->'
repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)

body() {
    printf '%s\n\n%s\n' "$(cat)" "$MARKER"
}

case ${1:-} in
thread)
    [ $# -eq 3 ] || { echo "usage: pr-reply.sh thread <pr> <comment-id>" >&2; exit 2; }
    body | gh api "repos/$repo/pulls/$2/comments/$3/replies" -F body=@- --jq .html_url
    ;;
comment)
    [ $# -eq 2 ] || { echo "usage: pr-reply.sh comment <pr>" >&2; exit 2; }
    body | gh pr comment "$2" --body-file -
    ;;
resolve)
    [ $# -eq 2 ] || { echo "usage: pr-reply.sh resolve <thread-id>" >&2; exit 2; }
    gh api graphql -f query='mutation($id: ID!) { resolveReviewThread(input: {threadId: $id}) { thread { isResolved } } }' \
        -f id="$2" --jq .data.resolveReviewThread.thread.isResolved
    ;;
*)
    sed -n '2,9p' "$0" >&2
    exit 2
    ;;
esac
