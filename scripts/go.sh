#!/bin/bash
# go wrapper that uses asdf if available, otherwise uses system go

# Check if asdf is available
if command -v asdf >/dev/null 2>&1; then
    # asdf is available, use asdf to run go
    asdf exec go "$@"
else
    # asdf not available, use system go
    if command -v go >/dev/null 2>&1; then
        go "$@"
    else
        echo "Error: go not found. Please install go or use asdf."
        exit 1
    fi
fi
