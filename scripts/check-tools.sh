#!/bin/sh
set -u

# Check that the tools needed to develop Peekaping are installed.
# Versions come from the files each ecosystem reads on its own, so any installer works:
# apps/server/go.mod (Go), .nvmrc (Node.js) and the packageManager field in package.json (Bun).
repo_root=$(CDPATH= cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

failed=0

ok() { printf '  ✅ %s\n' "$1"; }
fail() { printf '  ❌ %s\n' "$1"; failed=1; }

# version_ge A B succeeds when dotted version A >= B, e.g. version_ge 22.1.0 22
version_ge() {
    awk -v a="$1" -v b="$2" 'BEGIN {
        n = split(a, x, "."); m = split(b, y, ".")
        for (i = 1; i <= (n > m ? n : m); i++) {
            if (x[i] + 0 > y[i] + 0) exit 0
            if (x[i] + 0 < y[i] + 0) exit 1
        }
        exit 0
    }'
}

go_want=$(sed -n 's/^go \([0-9.]*\)$/\1/p' apps/server/go.mod)
node_want=$(tr -d ' v\n' < .nvmrc)
bun_want=$(sed -n 's/.*"packageManager": *"bun@\([^"]*\)".*/\1/p' package.json)

echo "Checking development tools..."

# Go 1.21+ installed older than the toolchain line in go.mod switches to that version,
# downloading it if needed, so running go inside apps/server reports the version that builds the server.
if ! command -v go >/dev/null 2>&1; then
    fail "Go not found. Install Go 1.21 or newer: https://go.dev/doc/install"
elif ! go_have=$(cd apps/server && go env GOVERSION); then
    fail "Go is on PATH but failed to run in apps/server (see the error above)"
elif ! version_ge "${go_have#go}" "$go_want"; then
    fail "Go ${go_have#go} is older than $go_want (apps/server/go.mod). Install Go 1.21 or newer and leave GOTOOLCHAIN unset, so Go can fetch the right version"
else
    ok "Go ${go_have#go}"
fi

if ! command -v node >/dev/null 2>&1; then
    fail "Node.js not found. Install Node.js $node_want: 'nvm install' or 'fnm install' read .nvmrc, or see https://nodejs.org"
elif ! node_have=$(node -p 'process.versions.node'); then
    fail "Node.js is on PATH but failed to run (see the error above)"
elif ! version_ge "$node_have" "$node_want"; then
    fail "Node.js $node_have is older than $node_want (.nvmrc). Run 'nvm install' or 'fnm install', or see https://nodejs.org"
else
    ok "Node.js $node_have"
fi

# Bun runs whichever version is installed, so it has to match the packageManager pin.
if ! command -v bun >/dev/null 2>&1; then
    fail "Bun not found. Install Bun $bun_want: 'curl -fsSL https://bun.sh/install | bash', 'brew install oven-sh/bun/bun' or 'npm install -g bun@$bun_want' (https://bun.sh/docs/installation)"
elif ! bun_have=$(bun --version); then
    fail "Bun is on PATH but failed to run (see the error above)"
elif [ "$bun_have" != "$bun_want" ]; then
    fail "Bun $bun_have runs here, but package.json pins Bun $bun_want. Run 'bun upgrade' if yours is older, or install that version: 'npm install -g bun@$bun_want'"
else
    ok "Bun $bun_have"
fi

echo ""
if [ "$failed" -ne 0 ]; then
    echo "Fix the items marked ❌, then run 'make setup' again."
    exit 1
fi
echo "All set. Next: 'make install' to install dependencies, then 'make dev'."
