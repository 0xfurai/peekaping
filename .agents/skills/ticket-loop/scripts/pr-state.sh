#!/usr/bin/env bash
# Print one PR's review state as JSON: checks on the head commit, whether it
# merges cleanly into its base, unresolved review threads waiting on us, and
# reviews and comments newer than the mark. `keys` names each of those items,
# so pr-wait.sh can tell a new one from one it already reported.
#
# Usage:
#   pr-state.sh [--brief] <pr>   state as JSON (--brief: last comment per thread only);
#                                reply to a thread with its replyTo comment id
#   pr-state.sh --mark <pr> <ts> record everything up to <ts> (an ISO time) as handled
#
# Our own replies carry MARKER, so a thread whose last comment has it is waiting
# on the reviewer, not on us. Run from inside the repository.
set -euo pipefail

MARKER='<!-- claude-ticket -->'

state_dir() {
    printf '%s/ticket-loop' "$(git rev-parse --path-format=absolute --git-common-dir)"
}

if [ "${1:-}" = "--mark" ]; then
    [ $# -eq 3 ] || { echo "usage: pr-state.sh --mark <pr> <ts>" >&2; exit 2; }
    mkdir -p "$(state_dir)"
    printf '%s\n' "$3" >"$(state_dir)/$2.handled-at"
    exit 0
fi

thread_comments='first: 50'
if [ "${1:-}" = "--brief" ]; then
    thread_comments='last: 1'
    shift
fi
[ $# -eq 1 ] || { echo "usage: pr-state.sh [--brief] <pr>" >&2; exit 2; }
pr=$1

repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
since=$(cat "$(state_dir)/$pr.handled-at" 2>/dev/null || echo '1970-01-01T00:00:00Z')
fetched_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)

query='query($owner: String!, $name: String!, $pr: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $pr) {
      number url state isDraft reviewDecision baseRefName headRefName headRefOid
      mergeable mergeStateStatus
      commits(last: 1) { nodes { commit { statusCheckRollup { contexts(first: 100) { nodes {
        __typename
        ... on CheckRun { name status conclusion detailsUrl completedAt }
        ... on StatusContext { context state targetUrl description }
      } } } } } }
      reviewThreads(first: 100) { nodes {
        id isResolved isOutdated path line
        root: comments(first: 1) { nodes { databaseId } }
        comments('"$thread_comments"') { nodes { databaseId author { login } body url createdAt } }
      } }
      reviews(last: 100) { nodes { author { login } state body submittedAt commit { oid } } }
      comments(last: 100) { nodes { author { login } body url createdAt } }
    }
  }
}'

gh api graphql -f query="$query" -F owner="${repo%/*}" -F name="${repo#*/}" -F pr="$pr" |
    jq --arg marker "$MARKER" --arg since "$since" --arg fetchedAt "$fetched_at" '
    def clean: gsub("<!--[\\s\\S]*?-->"; "") | gsub("^\\s+|\\s+$"; "");
    def mine: (.body // "") | contains($marker);
    def bot: (.author.login // "") | test("\\[bot\\]$|^coderabbitai$|^codecov|^github-actions");

    .data.repository.pullRequest as $p
    | ($p.commits.nodes[0].commit.statusCheckRollup.contexts.nodes // []) as $ctx
    | {
        pr: $p.number, url: $p.url, state: $p.state, isDraft: $p.isDraft,
        reviewDecision: $p.reviewDecision,
        base: $p.baseRefName, head: $p.headRefName, headSha: $p.headRefOid,
        mergeable: $p.mergeable, mergeState: $p.mergeStateStatus,
        since: $since, fetchedAt: $fetchedAt,
        checks: {
          pending: [$ctx[] | select(
              (.__typename == "CheckRun" and .status != "COMPLETED")
              or (.__typename == "StatusContext" and (.state == "PENDING" or .state == "EXPECTED")))
            | (.name // .context)],
          # A stacked PR'"'"'s project coverage is compared with a stale commit of its
          # base branch, so its codecov/project result says nothing.
          failed: [$ctx[] | select(
              (.__typename == "CheckRun" and (.conclusion // "" | test("FAILURE|TIMED_OUT|CANCELLED|ACTION_REQUIRED|STARTUP_FAILURE")))
              or (.__typename == "StatusContext" and (.state == "FAILURE" or .state == "ERROR")))
            | select((.name // .context) != "codecov/project" or $p.baseRefName == "main")
            | {name: (.name // .context), url: (.detailsUrl // .targetUrl)}],
          passed: ([$ctx[] | select(.conclusion == "SUCCESS" or .state == "SUCCESS")] | length),
          completedAt: ([$ctx[] | select(.__typename == "CheckRun" and .completedAt != null) | {(.name): .completedAt}] | add // {}),
          ci: ([$ctx[] | select(.context != "CodeRabbit")] | length),
          coderabbit: ([$ctx[] | select(.context == "CodeRabbit") | .state][0] // "MISSING")
        },
        threads: [$p.reviewThreads.nodes[]
          | select(.isResolved | not)
          | select(.comments.nodes[-1] | mine | not)
          | {id, replyTo: .root.nodes[0].databaseId, path, line, outdated: .isOutdated, url: .comments.nodes[0].url,
             comments: [.comments.nodes[] | {id: .databaseId, author: .author.login, mine: mine, body: (.body | clean)}]}],
        reviews: [$p.reviews.nodes[]
          | select(.submittedAt > $since)
          | select(mine | not)
          | select(((.body // "") | clean) != "" or .state == "CHANGES_REQUESTED" or (.state == "APPROVED" and (bot | not)))
          | {author: .author.login, state, submittedAt, commit: .commit.oid, body: (.body | clean)}],
        comments: [$p.comments.nodes[]
          | select(.createdAt > $since)
          | select(mine | not)
          | select((.body | test("^\\s*@coderabbitai\\s+(full\\s+)?review\\s*$")) | not)
          | select((.author.login == "coderabbitai" and (.body | test("summarize by coderabbit.ai|Action performed"))) | not)
          | select(.author.login | test("^codecov|^github-actions") | not)
          | {author: .author.login, createdAt, url, body: (.body | clean)}]
      }
    | .todo = (.threads + .reviews + .comments | length)
    # One key per item waiting on us. A rerun check gets a new url and a new
    # reply in a thread a new last comment, so either is a new item.
    | .keys = ([.checks.failed[] | "check:\(.name):\(.url):\($p.headRefOid)"]
        + [.threads[] | "thread:\(.id):\(.comments[-1].id)"]
        + [.reviews[] | "review:\(.author):\(.submittedAt)"]
        + [.comments[] | "comment:\(.url)"])
    '
