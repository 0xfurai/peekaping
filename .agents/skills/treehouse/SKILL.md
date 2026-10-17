---
name: treehouse
description: Work in isolated, reusable git worktrees with the `treehouse` CLI (github.com/kunchenguid/treehouse). Use when the user mentions treehouse, asks to do a task in an isolated worktree or sandbox checkout, wants several agents or subagents working on the same repo in parallel without conflicts, or wants to check, return, lease, prune, or destroy pooled worktrees.
---

# treehouse

`treehouse` keeps a pool of git worktrees per repository (default `~/.treehouse/<repo>-<hash>/<slot>/<repo>`). When a worktree is returned, it goes back to the pool with dependencies and build caches intact, so the next acquisition starts warm. Use it to isolate work from the user's main checkout, or to give each parallel agent its own checkout.

Check that it's installed with `command -v treehouse && treehouse --version`. If it's missing, show the user the install command (`curl -fsSL https://kunchenguid.github.io/treehouse/install.sh | sh`, or `go install github.com/kunchenguid/treehouse/v3@latest`) and let them run it. Don't pipe it to a shell yourself.

## Rule 1: never open a subshell

`treehouse`, `treehouse get` (without `--lease`) and `treehouse enter <name>` (without `--print-path`) start an interactive subshell, which hangs a non-interactive Bash tool. Use these forms instead:

| Goal | Command |
| --- | --- |
| Acquire a worktree | `treehouse get --lease --lease-holder <label> --json` |
| Get the path of an existing slot | `treehouse enter --print-path <name>` |
| Release | `treehouse return --if-lease-id <id> <path>` |

## Rule 2: commit on a branch, never on a detached HEAD

By default a worktree is handed over on a **detached HEAD**. `treehouse return` resets the slot, and **commits made on a detached HEAD are silently dropped**: the return exits 0, and no branch contains them anymore. Always do one of these:

- acquire with `-b <branch>` (preferred), or
- run `git -C "$WT" switch -c <branch>` before the first commit.

Branches survive a return because they live in the shared repository. Return detaches the slot but never deletes the branch.

## Standard workflow

Run from inside the user's repository:

```sh
treehouse get --lease --lease-holder "claude-<short-task>" -b "<branch>" --json
# stdout: {"path":"...","lease_id":"...","lease_holder":"...","leased_at":"...","base_branch":"main"}
# banners and post_create hook output go to stderr
```

1. **Acquire.** Save `path` and `lease_id` from the JSON output. Useful options:
   - `--base <branch>`: cut from a branch other than the default. Pass a branch name only, not `origin/x`, a tag or a SHA. An unknown base is an error, not a fallback.
   - `--no-fetch`: skip `git fetch origin` (offline work, or the refs were fetched already).
   - `--include-file <manifest>`: seed gitignored files (for example `.env*`) with this manifest instead of the committed `.worktreeinclude`.
   - `--unique-leaf`: name the directory `<repo>-<slot>`, for tooling that keys on the folder name (Compose project, test DB name).
2. **Work** in `$path`. Prefer absolute paths with Read/Edit/Write and `git -C "$path" ...`. For builds and tests, use `cd "$path" && <cmd>`. A reused slot may already contain `node_modules` and build output from earlier work, which is intentional. Run the project's install step anyway if the lockfile changed.
3. **Wrap up.** Commit to the branch and push or open a PR if the user wants that. Then check the slot:
   - `git -C "$path" status --porcelain` should be empty.
   - `git -C "$path" branch --show-current` should name your branch, not print nothing.
4. **Stop** any dev servers or watchers you started there. `return` kills lingering processes in the worktree.
5. **Return.** `treehouse return --if-lease-id "$lease_id" "$path"`
   - exit `0`: returned and the lease is released.
   - exit `3`: the slot has uncommitted changes, so treehouse refused and left the slot and lease alone. Don't retry in a loop. Commit or stash the work, or ask the user before passing `--force`. `--force` discards those changes.
   - exit `1`: failure (lease mismatch, process termination, reset). Read the error, then retry or report it.

If the user wants to keep working in the worktree themselves, don't return it. Give them the path and lease id, and tell them how to release it later.

## Parallel agents

Give each agent its own lease, with distinct `--lease-holder` labels and `-b` branches. Either acquire the leases yourself and pass each subagent its absolute `path` (telling it to work only there and not to return the slot), or have each subagent follow the standard workflow itself. The pool grows on demand up to `max_trees` (default 16). `get` never hands out a leased, in-use or dirty slot.

## Inspecting the pool

```sh
treehouse status          # human-readable: slot name, state, path, branch, holder
treehouse status --json   # name, path, status, branch, detached, lease_id, lease_holder, processes, ...
```

Statuses include `available`, `leased`, `in-use`, `dirty`, `unverified`, `damaged`, and `you're here`. Slot **names** (`1`, `2`, ...) are stable IDs and are accepted by `return`, `lease` and `enter` from inside the repo. Paths work from anywhere.

## Destructive operations: dry-run first, confirm with the user

These delete work or worktrees. Show the user the dry-run output and get explicit approval before adding `--yes` or `--force`:

- `treehouse prune` (current pool) or `treehouse prune --all` (every pool under the global root). These are dry runs. Add `--yes` to delete. Prune only removes clean, idle, unleased slots whose HEAD has landed.
- `treehouse destroy <path>`, or `treehouse destroy <pool> --all`. Also dry runs. Add `--yes` to execute. Each riskier class needs its own opt-in: `--include-unlanded` (data loss), `--include-in-use` (kills processes), `--include-leased` (exact path only).
- `treehouse return --force`: discards uncommitted changes.
- `treehouse return --all`: resets every held slot in the pool, including other agents' leases. Don't use it unless the user asks.

Never return or destroy a slot whose lease you don't hold (check `lease_holder` and `lease_id`) without asking.

## More

See [reference.md](reference.md) for configuration (`treehouse.toml`, user config, `.worktreeinclude`, hooks, root and in-project pools, worktree path templates, base branch, APFS sharing, jj) and recovery or quarantine handling.
