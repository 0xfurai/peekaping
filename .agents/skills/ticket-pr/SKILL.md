---
name: ticket-pr
description: "Open a ticket's reviewed branches as a PR stack and move the ticket to In Review."
disable-model-invocation: true
argument-hint: "PP-<n>"
---

# Ticket: PR

Start with [setup](../ticket/SETUP.md).

## Open the stack

The stack is the plan's PR stack, or without a plan, the ticket's branches as they are: one branch, one PR. Ask before splitting commits the user wrote.

Push and open the PRs bottom up, with the title format and body template in [STACK.md](../ticket/STACK.md). A branch that already has a PR keeps it; update its body if the stack around it changed.

Then in Linear: file the follow-ups from the plan or the review as tickets, after the duplicate check in `docs/agents/issue-tracker.md`. Move the ticket to In Review, add each PR as a link, and comment the stack.

Done when: every PR is open with the right base, title and body, and the ticket is In Review with the PRs linked.

Run alone, end by telling the user the next step: `/ticket-loop <PP-n>`.
