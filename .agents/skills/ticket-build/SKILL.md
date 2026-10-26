---
name: ticket-build
description: "Implement a ticket's agreed plan: split each PR into pieces, have Sonnet agents build them test first, and connect the pieces on the PR branch."
disable-model-invocation: true
argument-hint: "PP-<n>"
---

# Ticket: build

Start with [setup](../ticket/SETUP.md). The ticket needs a `## Agent plan`; without one, stop and tell the user to run `/ticket-plan` first.

Build the plan's PR stack bottom up, one branch per PR ([STACK.md](../ticket/STACK.md)). Each PR goes through the three steps below, with agents doing the building ([DELEGATE.md](../ticket/DELEGATE.md)). The next PR starts once the one below is connected, since it builds on it.

When the code shows the plan won't work as written, stop and take it back to the user (setup, "Throughout").

Done when: every PR is connected, every acceptance criterion is implemented with a green test, and the full local run in [TESTING.md](../ticket/TESTING.md) passes on each branch merged with `origin/main`.

## 1. Split

A **piece** is a part of the PR one agent can build alone: its own acceptance criteria, its own files. Cut along seams the code already has: server and web, one module and the next, one notification provider and another. When two parts need each other's code, write the contract between them before they start, word for word: the type, the function signature, the API payload. When no contract keeps them apart, they are one piece. A PR that doesn't split is one piece.

Each file belongs to one piece. Files every piece would touch (the 45 locale files, `go.mod`, the swagger docs, the generated web client) go to one piece, or you keep them for the connect step.

Done when: every acceptance criterion and approach step of the PR belongs to exactly one piece, and no file belongs to two.

## 2. Build the pieces

One agent per piece, all at once, each in a worktree of its own off the PR branch ([DELEGATE.md](../ticket/DELEGATE.md)). A lone piece gets one too, so connecting works the same for one piece or five. Each brief also carries:

- the piece's acceptance criteria and approach steps, quoted from the plan;
- the files it owns, the contract with the other pieces, and the files you keep, so its report names what it needs in them;
- **red → green**, one criterion at a time, as in [TESTING.md](../ticket/TESTING.md), committing each green slice;
- before it reports: the server tests, plus web lint and build when it touched `apps/web`. The e2e and real-database checks wait for the full local run, since the dev stack and the test containers are one per ticket.

Done when: every agent has reported.

## 3. Connect

In the ticket's worktree, on the PR branch:

1. **Check** each piece against its brief ([DELEGATE.md](../ticket/DELEGATE.md)): its report and `git log -p <PR branch>..piece/pp-<n>-<piece>`.
2. **Merge** the pieces one at a time: `git merge --no-edit piece/pp-<n>-<piece>`. Resolve each conflict keeping what both pieces meant, then remove the piece's worktree and branch.
3. **Glue**: the wiring between pieces, the files you kept, what the reports asked for, and the generated layers when the API contract changed ([TESTING.md](../ticket/TESTING.md)). Commit it.
4. **Verify**: one agent runs the full local run in [TESTING.md](../ticket/TESTING.md) on the PR branch merged with `origin/main` and fixes what fails, in commits on the PR branch. Check its report as you checked the pieces.

Done when: every piece is merged, its worktree and branch are gone, and the full local run passes on the PR branch merged with `origin/main`.

Run alone, end by telling the user the next step: `/ticket <PP-n> from review`.
