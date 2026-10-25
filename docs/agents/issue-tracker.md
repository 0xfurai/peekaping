# Issue tracker: Linear

Issues and specs for this repo live in Linear, in the **Peekaping** team (key `PP`, identifiers like `PP-123`). Use the Linear MCP tools (`mcp__linear-server__*`) for all operations. If the Linear MCP isn't connected, ask the user to connect it or paste the ticket. Don't fall back to GitHub Issues.

Community reports arrive as GitHub issues on `0xfurai/peekaping`. They aren't the tracker: a Linear ticket links the GitHub issues it covers as attachments. Read a linked issue with `gh issue view <n> --comments`; it holds the reporter's logs, configs and repro steps.

## Tickets from the GitHub tracker period

From 2026-10-19 to 2026-10-25 tickets lived in the private repo `0xfurai/peekaping-tracker` as `#<n>`, and PRs, branches and commits from then use that key. PP-1 to PP-216 kept their numbers. Tracker tickets `#217` to `#276` were copied back to Linear, whose numbers can't be chosen, so `#<n>` is `PP-<n+3>` for `n` from 217 on (`#217` is PP-220, `#254` is PP-257). Each copy ends with a line naming its tracker issue. Tickets filed in the tracker after `#276` have no fixed offset: search Linear for `peekaping-tracker#<n>`. The tracker repo is read-only history now: don't write to it.

## Conventions

- **Create an issue**: `save_issue` with `team: "Peekaping"`, `title` and a Markdown `description`. Set `project` (and `milestone`) when the work belongs to one; `list_projects` shows them.
- **Read an issue**: `get_issue` with `PP-<n>`, then `list_comments` for the discussion.
- **List issues**: `list_issues` with `team: "Peekaping"` and `label`, `state`, `project` or `parentId` filters. Text `query` search misses existing tickets, so for duplicate checks also list the backlog of the project the work belongs to.
- **Make an issue a sub-issue of a parent**: `save_issue` with `parentId: "PP-<parent>"`.
- **Comment on an issue**: `save_comment` with `issueId: "PP-<n>"`.
- **Apply / remove labels**: `save_issue` with `addLabels` / `removeLabels`. Never pass `labels`: it replaces the whole set.
- **Link a GitHub issue or PR**: `save_issue` with `links: [{url, title}]`.
- **Close**: comment first, then set `state` to `Done` (finished), `Canceled` (won't do), or `Duplicate` together with `duplicateOf`.

Statuses run Backlog → Todo → In Progress → In Review → Done, plus Canceled and Duplicate.

## Pull requests as a triage surface

**PRs as a request surface: no.** Linear holds no PRs. A community PR worth landing gets its own "Merge #NNN" ticket.

## Triage in Linear

- **Unlabeled** means a Backlog or Todo issue with none of the five state labels. `list_issues` filters on one label at a time, so list the team's Backlog and Todo and filter the results.
- **`wontfix`**: apply the label, then set `state: "Canceled"`.
- **Reporter**: the ticket's creator, or the author of a GitHub issue it links. Community reporters can't see Linear, so when a `needs-info` question is for them, also post the triage notes on the GitHub issue (`gh issue comment`), with the same AI disclaimer. Check both places for replies.

## Closing work

Work closes through PRs. Name the branch `<type>/pp-<n>-<slug>` so Linear links the PR to the ticket. Start the PR title with the ticket key, then the conventional-commit summary: `PP-48 fix(server): stop exposing JWT signing secrets`. A PR in a stack of several adds its place after the key: `PP-16 [1/2] feat(server): add a stale-monitor watchdog` (see `.agents/skills/ticket/STACK.md`). Move the ticket to In Review when the PR opens. Linear's GitHub integration moves it to Done on merge; if it doesn't, set Done yourself.

## When a skill says "publish to the issue tracker"

Create a Linear issue in the Peekaping team, as in **Create an issue** above.

## When a skill says "fetch the relevant ticket"

Read it as in **Read an issue** above, plus any GitHub issues or PRs it links.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a single issue with **child** issues as tickets. Create the `wayfinder:*` labels with `save_issue_label` in the Peekaping team the first time they're needed.

- **Map**: an issue labelled `wayfinder:map`, holding the Notes / Decisions-so-far / Fog body.
- **Child ticket**: a sub-issue of the map (`parentId`), labelled `wayfinder:<type>` (`research`/`prototype`/`grilling`/`task`). Once claimed, the ticket is assigned to the driving dev.
- **Blocking**: Linear's native "blocked by" relation: `save_issue` with `blockedBy: ["PP-<n>"]`. It's append-only; `removeBlockedBy` drops an edge. A ticket is unblocked when every blocker is Done or Canceled.
- **Frontier query**: `list_issues` with `parentId: "PP-<map>"`, keep open unassigned tickets, drop any whose `get_issue` relations show an open blocker; first in map order wins.
- **Claim**: `save_issue` with `assignee: "me"` and `state: "In Progress"`, the session's first write.
- **Resolve**: `save_comment` with the answer, set `state: "Done"`, then append a context pointer (gist + link) to the map's Decisions-so-far with a `save_issue` `patch`.
