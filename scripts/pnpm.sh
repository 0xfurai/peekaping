#!/bin/bash
# pnpm wrapper that uses asdf if available, otherwise uses system pnpm

# Check if asdf is available
if command -v asdf >/dev/null 2>&1; then
    # asdf is available, use asdf to run pnpm
    asdf exec pnpm "$@"
else
    # asdf not available, use system pnpm
    if command -v pnpm >/dev/null 2>&1; then
        pnpm "$@"
    else
        echo "Error: pnpm not found. Please install pnpm or use asdf."
        exit 1
    fi
fi
