---
name: ticket
description: "Work a Linear ticket (PP-<n>) through the team flow: plan with the user, build with tests, review against the ticket, open a PR stack, and loop on CI and review comments until merged. Use when the user hands over a ticket, or asks to run part of the flow for one: plan it, review their own branch, open its PRs, or handle its PR comments."
argument-hint: "PP-<n> | <pr> [from plan|build|review|pr|loop]"
---

# Ticket

The flow has five steps. Each is its own skill the user can run alone:

| Step | Skill | Starts from | Done when |
| --- | --- | --- | --- |
| plan | `/ticket-plan` | a ticket | the user approved a plan and it's in the ticket |
| build | `/ticket-build` | an approved plan | every acceptance criterion is implemented and tested |
| review | `/ticket-review` | a branch, Claude's or the user's | every criterion has code and test evidence, gaps fixed |
| pr | `/ticket-pr` | reviewed branches | the PR stack is open and the ticket is In Review |
| loop | `/ticket-loop` | open PRs | checks green and every comment handled, then on until merged |

`/ticket <target>` runs all five in order. `/ticket <target> from <step>` starts at that step and runs to the end: `from review` after the user implemented it themselves, `from loop` once the PRs are open. Without `from`, when work already exists, start at the first step whose done-when doesn't hold.

Setup marks the ticket In Progress when work on it starts; only the pr step moves it on, to In Review.

Run [SETUP.md](SETUP.md) once. Then for each step, read its skill file, `../ticket-<step>/SKILL.md` next to this skill's folder, and follow it, skipping its setup line and its closing "run alone" line. Move on when its done-when holds.

The plan step's checkpoint is the one place the flow waits for the user, besides a later step that breaks the plan.
