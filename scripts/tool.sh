#!/bin/sh
# POSIX-compliant wrapper for tools that uses asdf if available, otherwise uses system version

# Ensure a command is provided
if [ -z "$1" ]; then
    echo "Usage: $(basename "$0") <command> [args...]"
    echo "Example: $(basename "$0") go version"
    exit 1
fi

# Extract command name (e.g., go, pnpm)
cmd="$1"
shift

# Check if asdf is available
if command -v asdf >/dev/null 2>&1; then
    # asdf is available, use it
    asdf exec "$cmd" "$@"
else
    # asdf not available, use system command
    if command -v "$cmd" >/dev/null 2>&1; then
        "$cmd" "$@"
    else
        echo "Error: $cmd not found. Please install $cmd or use asdf."
        exit 1
    fi
fi
