---
name: ticket-plan
description: "Research a Linear ticket, agree a plan with the user, and record it in the ticket."
disable-model-invocation: true
argument-hint: "PP-<n>"
---

# Ticket: plan

Start with [setup](../ticket/SETUP.md).

## 1. Research

Read the glossary and the ADRs in `docs/adr/` that touch the area. Find where the change lands, every caller it affects, the repositories when persistence is involved, and the existing tests at those seams. MongoDB storage was removed (`docs/adr/0001-deprecate-mongodb-storage.md`): don't plan code that brings it back. The MongoDB monitor type and `bun db import-mongo` stay. For a bug, reproduce it: a failing test, or a command whose output shows it. Use Explore subagents (`effort: "high"`) for wide sweeps so your context keeps conclusions, not file dumps.

Done when: every requirement in the ticket points at the code it changes and the test seam that will prove it, and a bug is **red** (seen failing) or you can say why it can't fail locally.

## 2. Plan

Write the plan from the template in [PLAN.md](PLAN.md): numbered acceptance criteria, approach, test plan, PR stack, out of scope, follow-ups, open decisions with your recommendation, merge danger.

Done when: every acceptance criterion is checkable and has a test or a stated manual verification, and every PR in the stack is reviewable alone and leaves `main` green.

## 3. Checkpoint: agree the plan

Present the **brief** from [PLAN.md](PLAN.md), then ask with `AskUserQuestion`: the open decisions, then approval. Revise and ask again until the user approves. Answers that change scope or approach change the plan text before the next round.

Done when: the user has approved the plan as written.

## 4. Record the plan in Linear

Append the approved plan to the ticket description as a `## Agent plan` section (`save_issue` with a `patch` append; on later revisions replace that section only, leaving the reporter's text above it as written). Then `save_comment`: plan agreed, branch name, PR count.

Done when: the ticket shows the approved plan.

Run alone, end by telling the user the worktree path and the next step: `/ticket-build` to have Claude implement it, or implement it there yourself and run `/ticket <PP-n> from review`.
