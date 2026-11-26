---
name: ticket-loop
description: "Loop on a ticket's open PRs: wait for CI and CodeRabbit, handle every comment, push, repeat until the user merges; then restack and close the ticket."
disable-model-invocation: true
argument-hint: "PP-<n> | <pr>..."
---

# Ticket: loop

Start with [setup](../ticket/SETUP.md). The PRs are the ticket's open PRs, or the ones named; a PR whose base is another open PR's branch belongs to the same stack.

Reviewers here are CodeRabbit (automatic, minutes after each push) and the user (whenever they get to it). CI is `test.yml` (server tests, web lint and build), `test-import-mongo.yml` and `playwright.yml` (e2e on two databases), on PRs into `main` only. `test` and CodeRabbit report within about ten minutes, e2e in 15 to 35; Codecov posts `codecov/patch` and `codecov/project` after each of `test`, `import-mongo` and `executor` (the Integration tests job) uploads coverage; `pr-wait.sh` reports them only once all are done, since a result from one upload compares half the coverage with `main`'s whole. A push cancels the PR's runs still in progress.

## Scripts

They live in this skill's `scripts/` folder; call them by absolute path from inside the worktree.

| Script | Does |
| --- | --- |
| `pr-wait.sh [--idle] [--ignore CHECK]... <pr>...` | polls the stack, exits with `ACTION`, `READY`, `CONFLICT`, `MERGED`, `PUSHED` or `TIMEOUT` and a status line per PR. It reports each item once, as soon as it appears |
| `pr-state.sh <pr>` | everything waiting on you, as JSON: failed checks, whether it merges cleanly, open threads, new reviews and comments |
| `pr-state.sh --mark <pr> <fetchedAt>` | records reviews and comments up to that time as handled |
| `pr-coverage.sh <pr>` | Codecov's patch and project coverage, and the missed and partial lines of each changed file, as JSON |
| `pr-reply.sh thread <pr> <comment-id>` / `comment <pr>` / `resolve <thread-id>` | posts with the marker that tells the scripts the reply is yours (body on stdin) |

Post every reply through `pr-reply.sh`. Its hidden marker is how a thread you answered stops counting as waiting on you; a reply posted another way looks like a reviewer comment.

## One round

A round starts on the first thing that needs you, not when every check is done: a failed `test` or CodeRabbit's review is usually there long before e2e finishes, and the push that ends the round restarts CI anyway.

1. **Wait.** Start `pr-wait.sh <every open PR in the stack>` with Bash `run_in_background`; its exit wakes you. Keep one wait running at a time.
2. **Act on the outcome.**
   - `ACTION`: run `pr-state.sh` on each PR whose status line shows `todo` > 0, a failed check, or `coderabbit` other than `SUCCESS`, then start the next wait in the background at once, so items that arrive while you work wake you too: it reports only what it hasn't reported before. Handle every item (below). When that wait ends with `ACTION` before you push, add its items to this round; any other outcome you take up after the push.
   - `READY` (the stack is green and nothing waits on you): if this is the first `READY` since the last push, send a `PushNotification` and a short message: the PRs, what CodeRabbit raised and how each point was settled, and that it's ready for the user's review. Then wait again with `--idle`, which watches for up to two days.
   - `CONFLICT`: a PR no longer merges cleanly into its base (its status line says `mergeable CONFLICTING`). An agent resolves it ([Conflicts](#conflicts)); then wait again.
   - `MERGED`: an agent restacks ([STACK.md](../ticket/STACK.md), [DELEGATE.md](../ticket/DELEGATE.md)), briefed with the merged PR and every open branch; then wait again on the PRs still open. When none are open, wrap up.
   - `PUSHED`: someone else pushed. `git pull --no-rebase` on that branch, read what changed, and wait again.
   - `TIMEOUT`: wait again. After a non-idle timeout, check the status lines: a check stuck pending for an hour is worth telling the user, and `todo` > 0 or a failed check is an item reported earlier that you never settled, so read `pr-state.sh` and handle it.
3. **Push once per round**, as soon as the round's items are fixed or answered; don't wait for the rest of the old head's checks. Batch every fix of the round into commits, run the full local run ([TESTING.md](../ticket/TESTING.md)), push each changed branch, carry fixes up the stack ([STACK.md](../ticket/STACK.md)), and post `@coderabbitai review` on every pushed PR whose base isn't `main`. Then post the replies, so each can cite its commit.
4. **Mark.** `pr-state.sh --mark <pr> <fetchedAt>` with the `fetchedAt` of the state you handled, on each PR. Back to step 1: the wait started in step 2 is still running and now watches the new heads, so start one only if it has ended.

Between rounds there's nothing to say to the user unless you need a decision or the stack became `READY`. A resolved conflict gets one line in the next `READY` message.

Done when: on every open PR all checks pass, CodeRabbit reviewed the head commit, and nothing waits on you. Then keep looping on the user's comments until they merge the last PR or tell you to stop.

## Conflicts

A conflict appears when the base moved: `main` for the bottom PR, the branch below for a stacked one. Resolve it by merging the base in, never by rebasing a pushed branch. Brief an agent ([DELEGATE.md](../ticket/DELEGATE.md)) with the PR, its branch and base, and this procedure:

```sh
git fetch origin
git switch <branch> && git merge --no-edit origin/<base>   # a stacked PR merges the branch below instead
# resolve each conflicted file so both sides' intent survives, then
git add <files> && git merge --continue
```

The agent keeps both changes where they're independent, and where they collide it reads both commits' messages and the ticket to decide, reporting any file where it had to pick a side. Then the full local run ([TESTING.md](../ticket/TESTING.md)) on the merged tree, push, carry the merge up the stack ([STACK.md](../ticket/STACK.md), **Changing a lower PR**), and `@coderabbitai review` on every pushed PR whose base isn't `main`.

A conflict that a merge can't settle sensibly (the base rewrote what the ticket builds on) is a plan break: stop and ask the user with the brief in [PLAN.md](../ticket-plan/PLAN.md).

A `CONFLICT` right after a `MERGED` is the restack, not a merge: do the restack first and wait again before resolving anything.

## Handling an item

Review comments are claims to check, not orders. Read the code each one points at, then sort it:

- **Valid**: fix it, with a test when the comment found a bug.
- **Valid, out of scope**: reply with where it goes instead, and file it as a follow-up ticket after the duplicate check in `docs/agents/issue-tracker.md`.
- **Wrong or not worth it**: reply with the reason, citing code, a test, the ticket or the agreed plan.
- **Needs the user**: it changes scope or the agreed approach, it's a product call, or you disagree with the user's own comment. Ask the user with the brief in [PLAN.md](../ticket-plan/PLAN.md) and wait for the answer before the next round; then fix or reply as they decided.

When CodeRabbit disputes your reply, weigh its new argument once: fix it, or post a final one-line reply and resolve the thread. A third exchange on the same point goes to the user instead.

Where items live in `pr-state.sh` output:

- `threads`: inline comments still unresolved where the last word isn't yours. Reply with the thread's `replyTo` id (its first comment); GitHub rejects a reply to a reply.
- `reviews`: review bodies. CodeRabbit puts nitpicks and "outside diff range" comments here, not in threads; each one still gets handled. Answer them in one PR comment that lists each point and what you did.
- `comments`: top-level PR comments. Answer them with a PR comment.
- `checks.failed` with `codecov/patch` or `codecov/project`: see [Coverage](#coverage).
- other `checks.failed`: an agent ([DELEGATE.md](../ticket/DELEGATE.md)) reads the run (`gh run view <id> --log-failed`) and commits a fix for the cause, or reports that the failure is unrelated to this PR. Brief it first in the round and handle the comments once it reports, since both change the same worktree. A failure unrelated to this PR (a known flake, an outage) gets one rerun (`gh run rerun <id> --failed`); if it fails again the same way, tell the user and pass `--ignore <check>` to `pr-wait.sh` from then on.
- `coderabbit` not `SUCCESS` after the wait: post `@coderabbitai review` on that PR. If it still doesn't review (it allows 10 reviews an hour), tell the user and continue with `--ignore CodeRabbit`.

## Coverage

Codecov fails `codecov/patch` when under 70% of the PR's changed lines run in CI's tests, and `codecov/project` when the whole project's coverage drops more than 1% (`codecov.yml`). `pr-state.sh` leaves out `codecov/project` on a stacked PR: Codecov compares it with a stale commit of its base branch. CI runs the server tests on SQLite only, so code that runs only on Postgres never counts as covered there.

While `test`, `import-mongo` or `executor` is pending or failed, or Codecov hasn't posted since the last of them finished (it takes a minute or two to process an upload), a Codecov failure compares a partial upload, and `pr-wait.sh` holds it back: leave it, even though `pr-state.sh` lists it. Then run `pr-coverage.sh <pr>` and sort each file's `missed` and `partial` lines:

- **Reachable from a test on SQLite**: an agent ([DELEGATE.md](../ticket/DELEGATE.md)) adds the test, on the behaviour those lines carry, not on the line count. Brief it with the PR, the files and line numbers, and [TESTING.md](../ticket/TESTING.md).
- **Runs only on Postgres** (a dialect branch): covered by the real-database checks of [TESTING.md](../ticket/TESTING.md). Run them if this round's diff touched the file.
- **Not worth a test**: a defensive branch nothing reaches, or a wrapped error from a call that doesn't fail in tests. Leave it.

Then post one PR comment: the patch coverage Codecov reported, the tests you added, the files whose lines you left and why, and which real-database checks you ran. Don't reply to Codecov's own comment. A `codecov/project` drop on a PR into `main` comes from new uncovered code, which the files list shows, or from tests the PR deleted or narrowed, which it doesn't: the lines those tests covered are in files the PR didn't change. Read the PR's test changes before anything else, and restore the coverage like a patch failure. A drop neither explains, often several percent with `lines` in the head's totals far from `main`'s, is Codecov merging a different set of uploads for the head than for `main` (tracker #237): say so in the comment and ignore the check. After your push, Codecov reports again for the new head, and a check that still fails only on the lines you explained gets `--ignore codecov/patch` (or `codecov/project`) on the next wait.

## Replies

Write as the user writes in this repo: lead with the verdict, then the evidence, in one short paragraph.

- Fixed: "Fixed in <short sha>: <what changed>." Add the test name when there is one.
- Declined: "Not changing this here: <reason>." Point at the follow-up ticket when there is one.

Resolve a CodeRabbit thread once its fix is pushed or CodeRabbit accepts your reply (it usually resolves its own). A person's threads are theirs to resolve: reply, and leave them open.

## Wrap up

When the whole stack has merged:

1. Comment on the ticket: what shipped, the acceptance-criteria table with evidence, follow-up tickets.
2. Make sure the ticket is Done (the GitHub integration usually moves it).
3. Tell the user the worktree can go; they remove it.

Done when: the ticket is Done with the summary comment.
