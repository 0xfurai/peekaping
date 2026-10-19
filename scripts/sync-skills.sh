#!/bin/sh
set -eu

# Keep shared skills in .agents/skills and expose them to Claude Code.
repo_root=$(CDPATH= cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

mkdir -p .agents/skills .claude/skills

for skill in .agents/skills/*/; do
    [ -d "$skill" ] || continue
    name=$(basename "$skill")
    link=".claude/skills/$name"

    if [ -e "$link" ] && [ ! -L "$link" ]; then
        printf 'Cannot replace existing file or directory: %s\n' "$link" >&2
        exit 1
    fi

    ln -sfn "../../.agents/skills/$name" "$link"
done
