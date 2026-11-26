# Delegating to agents

You plan the ticket and answer PR review comments yourself. The rest of the work (code, tests, fixes, local runs, restacks, opening PRs) goes to agents: you brief them, wait for their reports, check what they did, and connect it. Linear and the user stay with you.

Each agent is `general-purpose` on Sonnet at high effort: `model: "sonnet"`, `effort: "high"`.

## Where it works

- One agent at a time can work in the ticket's worktree, on the branch its brief names. Leave the worktree to it until it reports.
- Agents that run at once each get a worktree of their own, off the branch they build on, so none sees another's half-done files:

  ```sh
  git worktree add .claude/worktrees/pp-<n>-<piece> -b piece/pp-<n>-<piece> <branch>
  ```

  Piece branches stay local. Once a piece is merged, remove both: `git worktree remove .claude/worktrees/pp-<n>-<piece> && git branch -d piece/pp-<n>-<piece>`.

## The brief

The agent starts fresh and knows only its brief. Give it:

- the ticket key, the absolute path of the worktree it works in, and its branch;
- the task, with the plan text, criteria or findings it covers quoted word for word;
- what to read first, by path in that worktree: `GLOSSARY.md`, and the ticket skill files the task follows (`.agents/skills/ticket/TESTING.md`, `STACK.md`);
- commits as `<type>(<scope>): <summary> (PP-<n>)`;
- where its work ends: a report, commits on its branch, or pushed branches and open PRs. An agent that pushes pushes only the branches its brief names;
- that when the approach in the brief doesn't hold, it stops and reports why;
- its closing report, under 300 words: what it did with evidence (test names, commands and results, commits, PR numbers), what's left undone, and every deviation from the brief.

## Checking

Read each report, and the diff it describes, against the brief. Send gaps back to the same agent with `SendMessage` and wait for it again; fix a one-line gap yourself.
