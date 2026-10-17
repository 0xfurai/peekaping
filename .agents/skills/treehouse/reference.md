# treehouse reference

Written against treehouse v3.1.x. Run `treehouse <cmd> --help` for the installed version's details.

## Commands

| Command | Notes |
| --- | --- |
| `treehouse` / `treehouse get` | Opens an interactive subshell. **Not for agents**: use `get --lease`. |
| `treehouse get --lease [--json]` | Durable, non-interactive lease. Prints the path (or JSON) to stdout. |
| `treehouse lease <name>` | Leases an existing slot in place. State-only: no reset, fetch or checkout. Refuses if the slot is already leased. |
| `treehouse enter --print-path <name>` | Prints the slot's path. Pool state is untouched. |
| `treehouse status [--json]` | Pool status. Also shows the resolved base branch. |
| `treehouse return [path\|name]` | Kills lingering processes, verifies they're gone, resets, releases the lease, and parks the slot on its base. |
| `treehouse return --all` | Returns every held slot in this pool (leased, in-use, dirty, unverified). Skips available, damaged and recovered slots. |
| `treehouse prune [--all] [--prune-orphans] [-v] [--yes]` | Dry run by default. Removes stale, clean, idle, landed slots. |
| `treehouse destroy <path>` / `destroy <pool> --all` | Dry run by default. Flags: `--yes`, `--include-unlanded`, `--include-in-use`, `--include-leased`. |
| `treehouse init` | Writes a default `treehouse.toml`. |
| `treehouse update` | Self-update. Refused for Nix installs. |

Global flag: `--root <dir>`.

### `get` flags

`--lease`, `--lease-holder <label>` (default `$TREEHOUSE_LEASE_HOLDER`), `--json` (requires `--lease`), `-b/--branch <name>` (fails if the branch exists; not supported on jj), `--base <branch>`, `--no-fetch`, `--include-file <path>`, `--unique-leaf`, `--worktree-path '<template>'`, `--apfs-sharing off|fresh`.

### `return` flags and exit codes

`--force` (clean without prompting), `--all`, `--if-lease-id <id>`, `--if-lease-holder <label>`. The conditions are checked under the pool lock, so a stale caller can't release a later acquisition of the same path.

| Exit | Meaning |
| --- | --- |
| 0 | Returned, and the lease is released. |
| 1 | Failed: lease condition unmet, process termination failed, or the reset failed. Retry is reasonable. |
| 3 | Not returned and left untouched: uncommitted changes, and cleaning was declined or couldn't be confirmed (stdin not a TTY). Retrying won't help. |

A `get` subshell that exits with the slot dirty also exits 3.

## Safety semantics

- **Reuse** requires the slot to belong to this clone, be idle, unleased and clean, and have its HEAD merged into the reset target. Otherwise `get` creates a new slot, up to `max_trees`.
- **Dirty** means tracked changes or untracked files, even if `status.showUntrackedFiles` hides them.
- **In use** means a running process has a cwd inside the slot, or a lifecycle reservation exists.
- **Return** detaches HEAD and resets the slot to its base. Named branches survive. Commits made only on a detached HEAD become unreachable (recoverable only through reflog).
- **Prune** skips leased, in-use, dirty and unlanded slots. If `origin` is unreachable, it skips everything as "cannot verify".

## Configuration

Repo-level `treehouse.toml` (repo root) takes precedence over user-level `~/.config/treehouse/config.toml`:

```toml
max_trees = 16                 # pool size cap
# root = "$HOME/worktrees"     # pool root; "." = in-project pool at <repo>/.treehouse/
# base_branch = "develop"      # branch to cut worktrees from (default: inferred)
# unique_leaf = true           # slot dirs named <repo>-<slot>
# worktree_path = "{repo_parent}/{repo}-{slot}"   # custom placement for new slots
# apfs_sharing = "fresh"       # macOS/APFS copy-on-write sharing of large tracked files
# vcs = "jj"                   # experimental Jujutsu backend
```

Precedence for root, unique_leaf and worktree_path is flag > env (`TREEHOUSE_ROOT`, `TREEHOUSE_UNIQUE_LEAF`, `TREEHOUSE_WORKTREE_PATH`) > repo config > user config > default. For APFS sharing the env var is `TREEHOUSE_APFS_SHARING`, and for the backend it's `TREEHOUSE_VCS`. The root always gets `treehouse` appended to it (`--root .` puts the pool at `<repo>/.treehouse/`).

### Base branch

The default comes from `origin/HEAD`, then the checked-out branch, then `init.defaultBranch`. `--base` overrides `base_branch`. Whichever of the local and `origin/` branch is further ahead wins. A returned slot is parked on the base it was cut from.

### Worktree path templates

Placeholders: `{slot}` (required), `{repo}`, `{repo_parent}`, `{pool}`. The template must also contain `{pool}` or `{repo}` and must resolve to an absolute path. `$VAR` and `${VAR}` expand first, and an empty variable is an error. A template supersedes `unique_leaf`. Templates only affect new slots, and treehouse never adopts an existing directory.

### Seeding gitignored files: `.worktreeinclude`

Commit `.worktreeinclude` (`.gitignore` syntax) to copy selected **ignored** files from the main checkout into every acquired slot:

```gitignore
.env*
!.env.local
local-config/
```

Only the committed version at the worktree's HEAD is used. `get --include-file <path>` replaces it for one acquisition. A missing manifest file is an error. Seeded files are tracked and removed on return or reuse.

### Hooks (user-level config only)

Hooks in repo `treehouse.toml` are ignored for safety.

```toml
[hooks]
post_create = ["./scripts/setup.sh"]   # after provision/reset, before handoff; stdout→stderr under --lease
pre_destroy = ["./scripts/teardown.sh"] # before destroy --yes / prune --yes
```

Hooks run sequentially in the worktree via `/bin/sh -c`. A failing hook is logged and doesn't fail the command.

## Recovery and quarantine

- If `treehouse-state.json` is corrupt or missing entries, treehouse rebuilds them from disk and marks them `leased` (recovered).
- On the next command, treehouse automatically frees a recovered Git slot only if it's provably idle, has unchanged tracked files, and has a landed HEAD. Untracked files are moved to `~/.treehouse/treehouse-recovered-backup-<pool>-<slot>` (Unix).
- If the slot still shows as recovered, `status` gives the reason. Inspect the slot, then `treehouse return <path>` by name, or `treehouse destroy <path> --include-leased --yes`. `return --all` skips such slots.
- Failed seeding, branch checkout or APFS sharing can leave a slot **quarantined** (leased, not published). Inspect it before returning or destroying it.
- A `damaged` slot (missing `.git` marker) is never reused. Remove it with `treehouse destroy <path> --include-unlanded`.

## jj backend (experimental)

Opt in with `vcs = "jj"` (only effective where `.jj` exists). Slots are non-colocated jj workspaces, so run `jj`, not `git`, inside them. `--branch` and `--base` aren't supported. Dirty means `@` is non-empty or has a description.
