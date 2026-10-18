---
name: ticket-build
description: "Implement a ticket's agreed plan, test first, one acceptance criterion at a time."
disable-model-invocation: true
argument-hint: "PP-<n>"
---

# Ticket: build

Start with [setup](../ticket/SETUP.md). The ticket needs a `## Agent plan`; without one, stop and tell the user to run `/ticket-plan` first.

## Implement

Build the plan's PR stack bottom up, one branch per PR ([STACK.md](../ticket/STACK.md)). Inside each PR, work in **red → green** slices, one acceptance criterion at a time ([TESTING.md](../ticket/TESTING.md)). Commit each green slice: `<type>(<scope>): <summary> (PP-<n>)`.

When the code shows the plan won't work as written, stop and take it back to the user (setup, "Throughout").

Done when: every acceptance criterion is implemented, its test is green, and the full local run in [TESTING.md](../ticket/TESTING.md) passes on each branch merged with `origin/main`.

Run alone, end by telling the user the next step: `/ticket <PP-n> from review`.
