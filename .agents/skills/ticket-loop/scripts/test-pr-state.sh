#!/usr/bin/env bash
# Checks how pr-state.sh and pr-wait.sh read CodeRabbit's report on a PR head:
# a check run (how CodeRabbit reports now) or a status context (how it did),
# with a fake `gh` first on PATH that answers the GraphQL query with fixture
# JSON, so no real PR is touched.
#
# The scripts run inside a throwaway git repository, because that is where they
# keep their state (the marks, the reported keys, when a head was first seen:
# <git common dir>/ticket-loop). The state of a real checkout is never read or
# written. pr-wait.sh counts its graces from when it first saw a head, so a case
# seeds that time instead of waiting for it; `--timeout 0 --interval 1` ends a
# wait that would otherwise hold at its first pass (TIMEOUT means it held).
#
# Usage: .agents/skills/ticket-loop/scripts/test-pr-state.sh
# Needs bash, git and jq. Works under the macOS bash 3.2 and under bash on Linux.
set -euo pipefail

scripts=$(cd "$(dirname "$0")" && pwd)
failures=0

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
repo="$work/repo"
bin="$work/bin"
fixtures="$work/fixtures"
mkdir -p "$repo" "$bin" "$fixtures"
git init -q "$repo"

# The fake gh: `gh repo view` names a repository, `gh api graphql` returns the
# fixture of the PR given by `-F pr=<n>`.
cat >"$bin/gh" <<'EOF'
#!/usr/bin/env bash
if [ "$1 $2" = "repo view" ]; then
  echo test/repo
  exit 0
fi
if [ "$1 $2" = "api graphql" ]; then
  pr=
  while [ $# -gt 0 ]; do
    case $1 in
    pr=*) pr=${1#pr=} ;;
    esac
    shift
  done
  cat "$FAKE_GH_FIXTURES/pr-$pr.json"
  exit 0
fi
echo "fake gh: unexpected call: $*" >&2
exit 1
EOF
chmod +x "$bin/gh"

# check <description> <command...>: runs the command, records the result and
# returns it.
check() {
  if "${@:2}"; then
    echo "ok   - $1"
  else
    echo "FAIL - $1"
    failures=$((failures + 1))
    return 1
  fi
}

head_sha=0123abcd

# run_check <name> <status> <conclusion>: a check run in the rollup
run_check() {
  jq -nc --arg n "$1" --arg s "$2" --arg c "$3" \
    '{__typename: "CheckRun", name: $n, status: $s, conclusion: (if $c == "" then null else $c end),
      detailsUrl: "https://example.test/\($n)", completedAt: (if $s == "COMPLETED" then "2026-10-30T10:00:00Z" else null end)}'
}

# status_context <name> <state>: a status context in the rollup
status_context() {
  jq -nc --arg n "$1" --arg s "$2" \
    '{__typename: "StatusContext", context: $n, state: $s, targetUrl: "https://example.test/\($n)", description: null}'
}

# write_pr <pr> <base> <context>...: the response for PR <pr> on <base> whose head has these contexts
write_pr() {
  local pr=$1 base=$2
  shift 2
  printf '%s\n' "$@" | jq -s --argjson pr "$pr" --arg base "$base" --arg sha "$head_sha" '
    {data: {repository: {pullRequest: {
      number: $pr, url: "https://example.test/pull/\($pr)", state: "OPEN", isDraft: false,
      reviewDecision: null, baseRefName: $base, headRefName: "fix/pp-\($pr)", headRefOid: $sha,
      mergeable: "MERGEABLE", mergeStateStatus: "CLEAN",
      commits: {nodes: [{commit: {statusCheckRollup: {contexts: {nodes: .}}}}]},
      reviewThreads: {nodes: []}, reviews: {nodes: []}, comments: {nodes: []}}}}}' >"$fixtures/pr-$pr.json"
}

# in_repo <command...>: runs the command in the throwaway repository with the fake gh
in_repo() {
  (cd "$repo" && PATH="$bin:$PATH" FAKE_GH_FIXTURES="$fixtures" "$@")
}

# state_field <pr> <jq path>: a field of pr-state.sh's output for the PR
state_field() {
  in_repo "$scripts/pr-state.sh" --brief "$1" | jq -r "$2"
}

# state_is <pr> <jq path> <expected>
state_is() {
  local got
  got=$(state_field "$1" "$2")
  [ "$got" = "$3" ] || { echo "       $2 is '$got', expected '$3'"; return 1; }
}

# wait_for <pr> <age> [pr-wait.sh options]: first sees the PR's head <age>
# seconds ago, then waits once; prints the outcome and the status line
wait_for() {
  local pr=$1 age=$2
  shift 2
  rm -rf "$repo/.git/ticket-loop"
  mkdir -p "$repo/.git/ticket-loop"
  echo "$head_sha" >"$repo/.git/ticket-loop/$pr.head"
  echo $(($(date +%s) - age)) >"$repo/.git/ticket-loop/$pr.since"
  in_repo "$scripts/pr-wait.sh" --timeout 0 --interval 1 "$@" "$pr"
}

# outcome_is <pr> <age> <outcome> [pr-wait.sh options]
outcome_is() {
  local pr=$1 age=$2 want=$3 out got
  shift 3
  out=$(wait_for "$pr" "$age" "$@")
  got=$(head -1 <<<"$out")
  [ "$got" = "$want" ] || { echo "       outcome is $got, expected $want"; sed 's/^/       /' <<<"$out"; return 1; }
}

# Ages: past every grace, and inside the CodeRabbit grace (20 minutes) but past
# the 90 seconds a fresh head is busy
old=3600
recent=300

ci_ok=$(run_check test COMPLETED SUCCESS)
ci_ok2=$(run_check web COMPLETED SUCCESS)

echo "pr-state.sh: the coderabbit field"

write_pr 1 main "$ci_ok" "$(run_check CodeRabbit COMPLETED SUCCESS)"
check "a CodeRabbit check run that passed shows coderabbit SUCCESS" state_is 1 .checks.coderabbit SUCCESS || true

write_pr 2 main "$ci_ok" "$(run_check CodeRabbit IN_PROGRESS '')"
check "a CodeRabbit check run in progress shows PENDING" state_is 2 .checks.coderabbit PENDING || true

write_pr 3 main "$ci_ok" "$(run_check CodeRabbit QUEUED '')"
check "a CodeRabbit check run that is queued shows PENDING" state_is 3 .checks.coderabbit PENDING || true

write_pr 4 main "$ci_ok"
check "no CodeRabbit report at all shows MISSING" state_is 4 .checks.coderabbit MISSING || true

write_pr 5 main "$ci_ok" "$(run_check CodeRabbit COMPLETED FAILURE)"
check "a CodeRabbit check run that failed shows FAILURE" state_is 5 .checks.coderabbit FAILURE || true

write_pr 6 main "$ci_ok" "$(status_context CodeRabbit SUCCESS)"
check "a CodeRabbit status context that succeeded shows SUCCESS" state_is 6 .checks.coderabbit SUCCESS || true

write_pr 7 main "$ci_ok" "$(status_context CodeRabbit PENDING)"
check "a CodeRabbit status context that is pending shows PENDING" state_is 7 .checks.coderabbit PENDING || true

write_pr 8 main "$ci_ok" "$(status_context CodeRabbit FAILURE)"
check "a CodeRabbit status context that failed shows FAILURE" state_is 8 .checks.coderabbit FAILURE || true

write_pr 13 main "$ci_ok" "$(status_context CodeRabbit EXPECTED)"
check "a CodeRabbit status context that is expected shows PENDING" state_is 13 .checks.coderabbit PENDING || true

write_pr 9 main "$ci_ok" "$(status_context codecov/patch SUCCESS)"
check "a status context of another name is not read as CodeRabbit's" state_is 9 .checks.coderabbit MISSING || true

echo
echo "pr-state.sh: ci leaves CodeRabbit out"

write_pr 10 main "$ci_ok" "$ci_ok2" "$(run_check CodeRabbit COMPLETED SUCCESS)"
check "ci does not count the CodeRabbit check run" state_is 10 .checks.ci 2 || true

write_pr 11 main "$ci_ok" "$ci_ok2" "$(status_context CodeRabbit SUCCESS)"
check "ci does not count the CodeRabbit status context" state_is 11 .checks.ci 2 || true

write_pr 12 main "$(run_check CodeRabbit IN_PROGRESS '')"
check "ci is 0 when CodeRabbit is the only report" state_is 12 .checks.ci 0 || true

echo
echo "pr-wait.sh: READY, and what holds the wait"

write_pr 20 main "$ci_ok" "$(run_check CodeRabbit COMPLETED SUCCESS)"
check "READY with a passed CodeRabbit check run, without --ignore CodeRabbit" outcome_is 20 $old READY || true

write_pr 21 main "$ci_ok" "$(status_context CodeRabbit SUCCESS)"
check "READY with a succeeded CodeRabbit status context" outcome_is 21 $old READY || true

write_pr 22 main "$ci_ok" "$(run_check CodeRabbit IN_PROGRESS '')"
check "a CodeRabbit check run in progress holds the wait within the CodeRabbit grace" outcome_is 22 $recent TIMEOUT || true

write_pr 23 main "$ci_ok" "$(run_check CodeRabbit IN_PROGRESS '')"
check "a CodeRabbit check run in progress past the grace is ACTION: it never reviewed" outcome_is 23 $old ACTION || true

write_pr 24 main "$ci_ok" "$(status_context CodeRabbit PENDING)"
check "a CodeRabbit status context pending past the grace is ACTION" outcome_is 24 $old ACTION || true

write_pr 25 main "$ci_ok"
check "no CodeRabbit report holds the wait within the grace" outcome_is 25 $recent TIMEOUT || true

write_pr 26 main "$ci_ok"
check "no CodeRabbit report past the grace is ACTION" outcome_is 26 $old ACTION || true

write_pr 27 main "$(run_check CodeRabbit COMPLETED SUCCESS)"
check "a PR into main whose only report is CodeRabbit still waits for CI to register" outcome_is 27 $recent TIMEOUT || true

write_pr 28 main "$ci_ok" "$(run_check CodeRabbit COMPLETED FAILURE)"
check "a CodeRabbit check run that failed is ACTION" outcome_is 28 $old ACTION || true

echo
echo "pr-wait.sh: --ignore CodeRabbit"

write_pr 30 main "$ci_ok" "$(run_check CodeRabbit IN_PROGRESS '')"
check "--ignore CodeRabbit: READY while its check run is still running, within the grace" outcome_is 30 $recent READY --ignore CodeRabbit || true
check "--ignore CodeRabbit: READY while its check run is still running, past the grace" outcome_is 30 $old READY --ignore CodeRabbit || true

write_pr 31 main "$ci_ok"
check "--ignore CodeRabbit: READY with no CodeRabbit report" outcome_is 31 $recent READY --ignore CodeRabbit || true

write_pr 32 main "$ci_ok" "$(run_check CodeRabbit COMPLETED FAILURE)"
check "--ignore CodeRabbit: READY with a failed CodeRabbit check run" outcome_is 32 $old READY --ignore CodeRabbit || true

write_pr 33 main "$ci_ok" "$(status_context CodeRabbit PENDING)"
check "--ignore CodeRabbit: READY with a pending CodeRabbit status context" outcome_is 33 $recent READY --ignore CodeRabbit || true

write_pr 34 main "$(run_check test IN_PROGRESS '')" "$(run_check CodeRabbit COMPLETED SUCCESS)"
check "--ignore CodeRabbit leaves other pending checks holding the wait" outcome_is 34 $old TIMEOUT --ignore CodeRabbit || true

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures check(s) failed for the ticket-loop scripts"
  exit 1
fi
echo "All checks passed for the ticket-loop scripts"
