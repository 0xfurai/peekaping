---
name: ticket-loop
description: "Loop on a ticket's open PRs: wait for CI and CodeRabbit, handle every comment, push, repeat until the user merges; then restack and close the ticket."
disable-model-invocation: true
argument-hint: "PP-<n> | <pr>..."
---

# Ticket: loop

Start with [setup](../ticket/SETUP.md). The PRs are the ticket's open PRs, or the ones named; a PR whose base is another open PR's branch belongs to the same stack.

Reviewers here are CodeRabbit (automatic, minutes after each push) and the user (whenever they get to it). CI is `test.yml` (server tests, web lint and build) and `playwright.yml` (e2e on three databases), on PRs into `main` only.

## Scripts

They live in this skill's `scripts/` folder; call them by absolute path from inside the worktree.

| Script | Does |
| --- | --- |
| `pr-wait.sh [--idle] [--ignore CHECK]... <pr>...` | polls the stack, exits with `ACTION`, `READY`, `MERGED`, `PUSHED` or `TIMEOUT` and a status line per PR |
| `pr-state.sh <pr>` | everything waiting on you, as JSON: failed checks, open threads, new reviews and comments |
| `pr-state.sh --mark <pr> <fetchedAt>` | records reviews and comments up to that time as handled |
| `pr-reply.sh thread <pr> <comment-id>` / `comment <pr>` / `resolve <thread-id>` | posts with the marker that tells the scripts the reply is yours (body on stdin) |

Post every reply through `pr-reply.sh`. Its hidden marker is how a thread you answered stops counting as waiting on you; a reply posted another way looks like a reviewer comment.

## One round

1. **Wait.** Start `pr-wait.sh <every open PR in the stack>` with Bash `run_in_background`; its exit wakes you. Work on nothing else in the worktree meanwhile.
2. **Act on the outcome.**
   - `ACTION`: run `pr-state.sh` on each PR with `todo` > 0 or failed checks, and handle every item (below).
   - `READY` (the stack is green and nothing waits on you): if this is the first `READY` since the last push, send a `PushNotification` and a short message: the PRs, what CodeRabbit raised and how each point was settled, and that it's ready for the user's review. Then wait again with `--idle`.
   - `MERGED`: an agent restacks ([STACK.md](../ticket/STACK.md), [DELEGATE.md](../ticket/DELEGATE.md)), briefed with the merged PR and every open branch; then wait again on the PRs still open. When none are open, wrap up.
   - `PUSHED`: someone else pushed. `git pull --no-rebase` on that branch, read what changed, and wait again.
   - `TIMEOUT`: wait again. After a non-idle timeout, check the status lines: a check stuck pending for an hour is worth telling the user.
3. **Push once per round.** Batch every fix of the round into commits, run the full local run ([TESTING.md](../ticket/TESTING.md)), push each changed branch, carry fixes up the stack ([STACK.md](../ticket/STACK.md)), and post `@coderabbitai review` on every pushed PR whose base isn't `main`. Then post the replies, so each can cite its commit.
4. **Mark.** `pr-state.sh --mark <pr> <fetchedAt>` with the `fetchedAt` of the state you handled, on each PR. Back to step 1.

Between rounds there's nothing to say to the user unless you need a decision or the stack became `READY`.

Done when: on every open PR all checks pass, CodeRabbit reviewed the head commit, and nothing waits on you. Then keep looping on the user's comments until they merge the last PR or tell you to stop.

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
- `checks.failed`: an agent ([DELEGATE.md](../ticket/DELEGATE.md)) reads the run (`gh run view <id> --log-failed`) and commits a fix for the cause, or reports that the failure is unrelated to this PR. Brief it first in the round and handle the comments once it reports, since both change the same worktree. A failure unrelated to this PR (a known flake, an outage) gets one rerun (`gh run rerun <id> --failed`); if it fails again the same way, tell the user and pass `--ignore <check>` to `pr-wait.sh` from then on.
- `coderabbit` not `SUCCESS` after the wait: post `@coderabbitai review` on that PR. If it still doesn't review (it allows 10 reviews an hour), tell the user and continue with `--ignore CodeRabbit`.

## Replies

Write as the user writes in this repo: lead with the verdict, then the evidence, in one short paragraph.

- Fixed: "Fixed in <short sha>: <what changed>." Add the test name when there is one.
- Declined: "Not changing this here: <reason>." Point at the follow-up ticket when there is one.

Resolve a CodeRabbit thread once its fix is pushed or CodeRabbit accepts your reply (it usually resolves its own). A person's threads are theirs to resolve: reply, and leave them open.

## Wrap up

When the whole stack has merged:

1. Comment on the ticket: what shipped, the acceptance-criteria table with evidence, follow-up tickets.
2. Make sure the ticket is closed and its Status is Done (the top PR's `Fixes` line should have closed it; if not, close it yourself).
3. `ExitWorktree` with `keep`, then `git worktree remove .claude/worktrees/pp-<n>`.

Done when: the ticket is Done with the summary comment, and the worktree is gone.
