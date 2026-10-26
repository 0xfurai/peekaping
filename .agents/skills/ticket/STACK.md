# The PR stack

A **stack** is a chain of PRs where each PR's base is the branch of the PR below it, and the bottom PR targets `main`. One PR is a stack of one.

## Branches

- Name each branch `<type>/pp-<n>-<slug>`, with its own slug per PR. The `pp-<n>` lets Linear link the PR to the ticket.
- Types: `fix`, `feat`, `refactor`, `chore`, `docs`, `ci`. For test-only work use `fix/` or `chore/`: the remote has a branch named `test`, so a `test/...` branch can't be pushed. The PR title can still start with `test(...)`.
- The bottom branch is the one setup put the worktree on. Start each next branch from the one below it, in the same worktree: `git switch -c <next branch> <branch below>`.

## Opening the PRs

Push and open bottom up, so each base exists:

```sh
git push -u origin <branch>
gh pr create --base <main or branch below> --head <branch> --title "<title>" --body-file <file>
```

- Title: `PP-<n> [<i>/<total>] <type>(<scope>): <summary>`, e.g. `PP-16 [1/2] feat(server): add a stale-monitor watchdog`. The ticket key comes first. `[<i>/<total>]` is the PR's place in the stack, counted from the bottom; a stack of one leaves it out: `PP-48 fix(server): stop exposing JWT signing secrets`. Scope `server`, `web`, or omitted for both. Summary in plain words, lower case, imperative.
- If the stack gains or loses a PR before it merges, retitle every open PR in it: `gh pr edit <pr> --title "<title>"`. A merge changes nothing: the PRs above keep their numbers.
- Open as ready for review, not draft: CodeRabbit skips drafts.
- CodeRabbit reviews PRs into `main` on its own. A PR into another branch needs a trigger after it opens and after each push: `gh pr comment <pr> --body "@coderabbitai review"`.
- Write the body to a file in the scratchpad, not inline.

## PR body

Use the GLOSSARY.md terms, short sentences, no preamble.

```markdown
## Summary

<What changed and why, in 2-4 sentences, linking the ticket: [PP-<n>](<linear url>).
Then the smallest picture that shows the change: a diff-sketch of the control
flow, a call tree, or a file tree with one comment per file. Skip it when a
sentence says it.>

## Evidence

- **Before:** <failing test name and what it saw, or the observed bug>
  **After:** <the same test passing, or the new behaviour>

<the commands you ran and their result, e.g. `go test ./internal/...` → all packages ok>

## Stack

1. #<n> <title> ← this PR
2. #<n> <title>

## Merge danger

**Door:** <one-way or two-way>. <why, e.g. "the migration drops a table">
**Blast radius:** <what can break: upgrade, API consumers, UI, config>

🤖 Generated with [Claude Code](https://claude.com/claude-code)
```

Omit `## Stack` for a single PR. After opening the whole stack, edit each body so its Stack section lists every PR (`gh pr edit <pr> --body-file <file>`).

## Changing a lower PR

A review fix for a lower PR goes on that PR's branch, as a new commit. Then carry it up by merging each branch into the one above, bottom to top:

```sh
git switch <lower branch>   # commit the fix, push
git switch <next branch> && git merge --no-edit <lower branch> && git push
```

Merging keeps every pushed commit in place, so review threads stay anchored and no force-push is needed.

## Restack after a merge

The repo squash-merges, so after the bottom PR merges, every PR above still carries the merged branch's original commits. Move them off it, bottom up:

```sh
git fetch origin
gh pr view <merged pr> --json headRefOid --jq .headRefOid    # the merged tip
git rev-parse <next branch> <branch above> ...               # every open branch's tip, before any rebase
git rebase --onto origin/main <merged tip> <next branch>
# full local run (TESTING.md), then retarget before pushing:
gh pr edit <next pr> --base main && git push --force-with-lease
```

The order matters. CI runs only for PRs whose base is `main`, and it's the push that starts it; a base change alone starts nothing. Branch auto-delete is off in this repo, so GitHub never retargets the PR for you.

Then for each branch further up, rebase it onto the branch below, cutting at that branch's old tip from the list:

```sh
git rebase --onto <branch below> <old tip of branch below> <branch> && git push --force-with-lease
```

Then wait on the restacked PRs as in [the loop](../ticket-loop/SKILL.md): the new bottom PR now gets CI and CodeRabbit on its own.
