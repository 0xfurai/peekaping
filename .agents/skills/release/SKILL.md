---
name: release
description: "Cut a Peekaping release: check main and the changelog, dispatch the Release workflow, watch it, verify the tag, release notes and images, then open the follow-up PR. Also builds release candidates (X.Y.Z-rc.N)."
disable-model-invocation: true
argument-hint: "[X.Y.Z | X.Y.Z-rc.N]"
---

# Release

The `Release` workflow (`.github/workflows/docker-release.yml`) is started by hand with a version. In 15 to 40 minutes it:

1. builds every image for amd64 and arm64 and pushes them to GHCR and Docker Hub;
2. merges the multi-arch manifests (and `latest`) and runs the e2e suite against the pushed images;
3. bumps `version.go`, `package.json` and `version.ts`, moves `[Unreleased]` in `CHANGELOG.md` under the new version, and pushes that commit straight to `main`;
4. creates the GitHub Release, with its tag on that commit;
5. pushes the Helm chart to `oci://ghcr.io/0xfurai/charts/peekaping`, with `version` and `appVersion` set to the release, so it installs that release's images;
6. deploys the demo.

It runs no unit tests, and none of it can be taken back cleanly: the images and the release are public and the commit is on `main`. So the work is in the steps before the dispatch.

## Rules

- The user decides. You prepare, check, and dispatch only after their explicit go-ahead on the exact version.
- Never pass the workflow's `changelog` input. It is pasted into a shell script, and the entries are full of backticks, which the shell would run. Left empty, the workflow reads `[Unreleased]` itself.
- From the dispatch until the GitHub Release exists, nothing merges into `main`. The bump job pushes to `main` after the e2e suite, from the commit the run started on, and fails if `main` moved since; a merge after that lands in the tag, because the release job cuts the tag at the head of `main`.
- Tags have no `v` prefix: `0.0.47`.
- Other sessions share the main checkout. Files change in a worktree under `.claude/worktrees/`; `git fetch`, never switch branches in the main checkout.

## 1. Choose the version

Take it from the argument. With none, propose the next patch after the last tag (`git tag -l | sort -V`): every release so far is `0.0.N`. An `X.Y.Z-rc.N` version goes to [release candidates](#release-candidates).

## 2. Preflight

From the repo root (or any worktree of it):

```sh
.agents/skills/release/scripts/preflight.sh <X.Y.Z>
```

It reads `origin/main` and reports: the version is new, CI on `main`, the `[Unreleased]` entries, PRs merged since the last tag that no entry mentions, migrations added since, and whether the Helm chart's bundled PostgreSQL and Redis are behind the latest. `BLOCK` lines exit 1.

- A red or still-running CI is a blocker: the release workflow won't catch what `Test and Lint` and `Playwright Tests` catch.
- The PR list is a worklist, not a verdict. Entries cite PR numbers, issue numbers or nothing, so read the entries for each PR on it. Tests, CI and refactors with no visible change need no entry.
- Every migration listed should have an entry that says what it does to an existing database.
- A `WARN Helm chart:` line never blocks. It says one of three things:
  - `... are behind the latest`: Bitnami has published newer PostgreSQL or Redis subcharts or images than the chart pins. Run `scripts/update-chart-deps.sh --write` in a PR, before the release or after it.
  - `... has a new major version`: a subchart or an image has another major version than the chart pins. `--write` refuses it and writes nothing, not even for the lines that are only behind, because Bitnami's major versions are breaking. It is a ticket of its own: tell the user, and don't run `--write`.
  - `couldn't check ...`, or `origin/main has no scripts/update-chart-deps.sh`: nothing is known to be behind. Tools (helm, curl, jq), files on `origin/main` or access to Docker Hub are missing. Fix that and run the preflight again, or run `scripts/update-chart-deps.sh` by hand to see its report. `--write` is not the next step.

Done when: no blockers, and every PR on the list has an entry or a reason not to.

## 3. Curate the changelog

Only when step 2 found gaps. In a worktree (`git worktree add .claude/worktrees/release-<X.Y.Z> -b chore/release-<X.Y.Z>-changelog origin/main`), edit `[Unreleased]` in `CHANGELOG.md`:

- One entry per user-visible change, under Added, Changed or Fixed, in the style of its neighbours: `type: what the user sees now (#issue, #pr) (thanks @author)`. Read the PR (`gh pr view <n>`) for what changed; credit authors the way the neighbouring entries do.
- A change that needs the user to act on upgrade, such as a migration, a new default or a moved setting, says so in the entry, starting `Upgrade note:`.
- Keep only the `### ` headings. The workflow copies `[Unreleased]` with `awk`, which drops every other line that starts with `#`.

Open one PR as in [STACK.md](../ticket/STACK.md), without a ticket key: `docs: complete the changelog for <X.Y.Z>`. The user merges it. Then run the preflight again.

## 4. Get the go-ahead

Ask with the brief in [PLAN.md](../ticket-plan/PLAN.md), one `AskUserQuestion`: dispatch `<X.Y.Z>` or wait. The brief gives the version and the last tag, the `origin/main` commit, what `[Unreleased]` says in a few plain lines, the upgrade notes and migrations, and anything the preflight flagged and how it settled. Ask the user not to merge into `main` until you report the release is out.

Done when: the user said to dispatch that version. If `origin/main` moved since you asked, run the preflight again and re-confirm.

## 5. Dispatch

```sh
gh workflow run docker-release.yml --ref main -f version=<X.Y.Z>
gh run list --workflow=docker-release.yml --limit 1 --json databaseId,headSha,status
```

The run takes a few seconds to appear. Check that its `headSha` is the commit the user approved. Then watch with `gh run watch <id> --exit-status` as a Bash `run_in_background`; its exit wakes you. The jobs run in this order: `build-images` (per architecture) and `build-bundle-containers`, `create-manifest`, `playwright-tests`, `bump-version`, `release`, then `publish-chart` and `deploy-demo` side by side.

## 6. If the run fails

Find the cause: `gh run view <id> --log-failed`. Then see whether the bump landed: `git fetch origin && git log origin/main --author=github-actions -3 --format='%h %s'`, looking for `chore: bump version to <X.Y.Z> and update changelog`.

- **No bump commit.** `main` is untouched, but once `create-manifest` has passed the version tags and `latest` are public. A flake or an outage gets one `gh run rerun <id> --failed`, which reruns the failed jobs and the ones after them on the same commit. A real failure stops here: tell the user the cause. The fix is a normal PR; then preflight and dispatch the same version again, which overwrites the manifests (nothing rolled `[Unreleased]`). If `bump-version` failed because `main` moved, a rerun fails the same way, since the job starts from the commit the run started on; dispatch again from the new head, after the go-ahead of step 4.
- **Bump commit on `main`.** Only `release`, `publish-chart` and `deploy-demo` run after it. Never dispatch the same version again: the changelog step would roll an empty `[Unreleased]` into a second `## [<X.Y.Z>]` heading. A flake gets one `gh run rerun <id> --failed`. A real failure goes to the user with the brief in [PLAN.md](../ticket-plan/PLAN.md): the images and the rolled changelog exist, the release doesn't, and the way out is to fix forward and release the next patch.

## 7. Verify

Don't report the release out until each of these is checked:

- **Run.** `gh run view <id>` shows success. `deploy-demo` doesn't count: its deploy step is `continue-on-error`, so a failure shows only as a "Demo not updated" warning on the run. Read that job's log: it prints the demo's `image:` lines, which should end in `<X.Y.Z>`.
- **Main.** `git log origin/main --author=github-actions -1` is the bump commit. `git show origin/main:CHANGELOG.md | head -30` shows an empty `[Unreleased]` and `## [<X.Y.Z>] - <today>` under it.
- **Tag and release.** `gh release view <X.Y.Z> --json tagName,isPrerelease,url,body`: not a pre-release, the body starts with the notes you reviewed. `git rev-parse <X.Y.Z>^{commit}` is the bump commit; if not, say which commits landed after it.
- **Images.** For each of `web api migrate probe bundle-sqlite bundle-postgres`, `docker buildx imagetools inspect 0xfurai/peekaping-<name>:<X.Y.Z>` lists `linux/amd64` and `linux/arm64`. For one of them, `--format '{{.Manifest.Digest}}'` is the same for `:<X.Y.Z>` and `:latest`. This checks Docker Hub; GHCR pushes come from the same green `create-manifest` job, and an anonymous inspect of GHCR can answer 403.
- **Helm chart.** `helm show chart oci://ghcr.io/0xfurai/charts/peekaping --version <X.Y.Z>` shows `version` and `appVersion` of `<X.Y.Z>`. After the first release that publishes it, the GHCR package `charts/peekaping` is private: in its package settings on GitHub, link it to the repository and make it public, or the command answers 401 for everyone else.

## 8. Open the OpenAPI PR

A known gap in the workflow: the bump changes the API's version, which is written into `apps/server/docs/openapi.json` and `openapi.yaml`, but the workflow doesn't regenerate them, and its push starts no CI. The first PR that touches `apps/server` or `apps/web` then fails `bun run api:check`.

In a worktree off the updated `origin/main`, run `bun run api:generate`. The diff should be the `version` line in those two files and nothing else. If `go` doesn't run on this machine, change that one line in each file by hand; CI's `api:check` confirms it. Commit `chore(server): regenerate the OpenAPI docs for <X.Y.Z>`, open the PR as in [STACK.md](../ticket/STACK.md) without a ticket key, and give the user the link: it should merge before any other PR. Delete this step when the workflow regenerates the docs itself.

## 9. Wrap up

Tell the user: the release URL, the run URL, what you verified, anything to look at (the demo, an upgrade note), and the OpenAPI PR. Remove the release worktrees once their PRs merge: `git worktree remove .claude/worktrees/release-<X.Y.Z>`.

Done when: the GitHub Release is out with the right notes, both architectures are on Docker Hub, `main` has the bump, and the OpenAPI PR is open.

## Release candidates

`X.Y.Z-rc.N` runs `release-candidate.yml`. It runs the web lint and build and the server tests first, pushes every image tagged with the RC version only (never `latest`), and publishes a pre-release at the head of the branch it ran on. It leaves `CHANGELOG.md` and the version files alone, so the preflight and changelog steps (2, 3), the bump case of step 6 and step 8 don't apply.

1. Next number: `git tag -l 'X.Y.Z-rc.*' | sort -V | tail -1`, plus one; `rc.1` for the first.
2. Ask which branch to build: `main`, or the branch of a PR the user wants to try. The RC is public, so get the go-ahead on the version and branch, as in step 4.
3. `gh workflow run release-candidate.yml --ref <branch> -f version=<X.Y.Z-rc.N>`, with no `changelog` input, then watch as in step 5.
4. Verify as in step 7: the run, `gh release view` shows a pre-release, and the images carry both architectures. On a PR branch the workflow comments the images on the PR.
