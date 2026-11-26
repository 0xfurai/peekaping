---
name: ticket-review
description: "Review a ticket's branch, Claude's or your own, against the ticket: every acceptance criterion implemented and tested, gaps fixed, full local run green."
disable-model-invocation: true
argument-hint: "PP-<n> | <branch>"
---

# Ticket: review

Start with [setup](../ticket/SETUP.md).

## 1. Pin the criteria

Use the acceptance criteria in the ticket's `## Agent plan`. Without a plan (the user implemented straight from the ticket), write them from the ticket the way [PLAN.md](../ticket-plan/PLAN.md) defines them: one observable behaviour per requirement, numbered AC1, AC2, and so on. With no ticket at all, take them from the PR description or ask the user.

Also pin the branches: the plan's PR stack, or the ticket's branches as they are, bottom to top. Uncommitted changes in the checkout may be the user's unfinished work: ask whether they belong in it, and leave them untouched until they answer.

Done when: you have a numbered list of criteria, the branch list, and a clean or explained working tree.

## 2. Check

Brief an agent ([DELEGATE.md](../ticket/DELEGATE.md)) with only pointers: the ticket key, the criteria, the branch list, and `git diff origin/main...<top branch>`. Its task:

> For each acceptance criterion, report: implemented (file:line), tested (test name), or missing. Then list: changes outside the criteria; likely bugs; GLOSSARY terms misused; UI strings missing from any of the 45 locale files; code that brings back MongoDB storage (ADR 0001: it was removed; the MongoDB monitor type and `bun db import-mongo` stay). Quote the criterion for each finding. Under 400 words.

## 3. Fix

Sort the findings, then brief one agent ([DELEGATE.md](../ticket/DELEGATE.md)) with every real one: missing or weak tests (red first, as in [TESTING.md](../ticket/TESTING.md)), missing translations, bugs, code that brings back MongoDB storage. It commits each fix on the branch it belongs to and carries it up the stack ([STACK.md](../ticket/STACK.md)).

When the branch is the user's own work, the brief says to fix it the way they built it. A finding whose fix would change their approach or scope goes to the user first, with the brief in [PLAN.md](../ticket-plan/PLAN.md).

The same agent then runs the full local run in [TESTING.md](../ticket/TESTING.md) on each branch merged with `origin/main`. Repeat the check until it comes back clean.

Done when: every criterion has code and test evidence in a table, no finding is open, and the full local run passes.

Show the user the table. Run alone, end by telling them the next step: `/ticket <PP-n> from pr`.
