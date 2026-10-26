#!/usr/bin/env bash
# Checks that the code blocks in the docs that copy a file of this repo are
# still word-for-word copies of it. The Docker pages show the cluster-mode
# Compose files (docker-compose.cluster.<db>.yml) and nginx.conf for readers to
# copy, so a change to one of those files has to reach the page, or the page
# gives a setup that doesn't work.
#
# A copied block is marked with an HTML comment on the line before its code
# fence, naming the file relative to the repo root:
#
#   <!-- snippet: docker-compose.cluster.sqlite.yml -->
#
# The script finds the markers under apps/docs/docs, takes the code block after
# each one, and diffs it with the file. It fails for a block that differs, for
# a marker naming a file that doesn't exist, and for a marker that no code
# block follows. A block with no marker isn't checked: that's for examples and
# excerpts that are deliberately not a full copy, such as the single-mode
# Compose snippets (they run the published image where docker-compose.single.*
# builds from source) and the .env excerpts.
#
# Usage: scripts/test-docs-snippets.sh
# Needs only bash, awk and diff. It reads the repo around the script, so a copy
# of scripts/ and apps/docs/docs/ and the marked files in another directory is
# checked on its own.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
docs=apps/docs/docs
failures=0

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

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

# fail <message...>: says why a check failed
fail() {
  echo "       $*"
  return 1
}

# find_snippets <file...>: for each marker, writes the code block that follows
# it to $work/block.<n> and a line "<n> <page> <marker line> <path> <state>"
# (tab-separated) to $work/snippets.tsv. The state is "block", "nofence" when
# no code block follows the marker, or "unterminated" when the block never
# closes. A marker inside another code block, such as an example of the
# convention, isn't a marker.
find_snippets() {
  awk -v root="$root" -v work="$work" '
    function report(state_name) {
      printf "%d\t%s\t%d\t%s\t%s\n", n, page, marker_line, path, state_name >> (work "/snippets.tsv")
    }
    FNR == 1 {
      if (state == 1) report("nofence")
      if (state == 2) { close(out); report("unterminated") }
      state = 0; in_code = 0
      page = substr(FILENAME, length(root) + 2)
    }
    state == 2 {
      if ($0 == "```") { close(out); report("block"); state = 0 } else print > out
      next
    }
    {
      is_fence = ($0 ~ /^```/)
      is_marker = (!in_code && $0 ~ /^<!-- snippet: .* -->[ \t]*$/)
      if (state == 1) {
        if ($0 ~ /^[ \t]*$/) next
        if (is_fence) {
          out = work "/block." n
          printf "" > out
          state = 2
          next
        }
        report("nofence"); state = 0
      }
      if (is_marker) {
        n++
        path = $0
        sub(/^<!-- snippet: */, "", path)
        sub(/ *-->[ \t]*$/, "", path)
        marker_line = FNR
        state = 1
        next
      }
      if (is_fence) in_code = !in_code
    }
    END {
      if (state == 1) report("nofence")
      if (state == 2) { close(out); report("unterminated") }
    }
  ' "$@"
}

# matches <block> <file> <page:line> <path>: the code block is the file, word
# for word. On a difference, shows it with the lines of the file as - and the
# lines of the block as +.
matches() {
  local changes
  if changes=$(diff -u --label "$4" --label "$3 (the docs)" "$2" "$1"); then
    return 0
  fi
  sed 's/^/       /' <<<"$changes"
  fail "update the block to match $4, or $4 to match the block"
}

echo "# Snippets"
: >"$work/snippets.tsv"
pages=()
while IFS= read -r page; do
  pages+=("$page")
done < <(find "$root/$docs" -type f \( -name '*.md' -o -name '*.mdx' \) | sort)
if [ "${#pages[@]}" -gt 0 ]; then
  find_snippets "${pages[@]}"
fi

snippets=0
while IFS=$'\t' read -r n page line path state; do
  snippets=$((snippets + 1))
  where="$page:$line"
  description="$where copies $path"
  case "$state" in
    nofence)
      check "$description" fail "no code block follows the marker" || true
      continue
      ;;
    unterminated)
      check "$description" fail "the code block after the marker never closes" || true
      continue
      ;;
  esac
  case "$path" in
    /* | *..*)
      check "$description" fail "$path must be a path inside the repo, relative to its root" || true
      ;;
    *)
      if [ ! -f "$root/$path" ]; then
        check "$description" fail "$path is not a file in the repo" || true
      else
        check "$description" matches "$work/block.$n" "$root/$path" "$where" "$path" || true
      fi
      ;;
  esac
done <"$work/snippets.tsv"

# has_snippets: a check that finds nothing would pass for ever if the markers were lost
has_snippets() {
  [ "$snippets" -gt 0 ] || fail "no <!-- snippet: <path> --> marker found under $docs"
}
check "the docs have marked snippets to check" has_snippets || true

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures check(s) failed for the docs snippets"
  exit 1
fi
echo "All checks passed for the docs snippets ($snippets snippets)"
