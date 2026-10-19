#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
for format in json yaml; do
  output="docs/openapi.$format"
  temporary=$(mktemp)
  trap 'rm -f "$temporary"' EXIT HUP INT TERM
  go run ./cmd/openapi -format "$format" > "$temporary"
  if [ "${1:-}" = "--check" ]; then
    if ! cmp -s "$temporary" "$output"; then
      echo "$output is stale; run bun run api:generate" >&2
      exit 1
    fi
  elif ! cmp -s "$temporary" "$output"; then
    mv "$temporary" "$output"
  fi
  rm -f "$temporary"
  trap - EXIT HUP INT TERM
done
