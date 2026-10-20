# Issue tracker: GitHub (private tracker repo + Project)

Tickets are issues in the **private** repo `0xfurai/peekaping-tracker`, planned on the private Project <https://github.com/users/0xfurai/projects/4> (owner `0xfurai`, number `4`). Use `gh` for all operations; its token needs the `project` scope (the user runs `! gh auth refresh -s project` once). Linear is the old tracker: don't write to it.

Community reports are issues in the public repo `0xfurai/peekaping`. They aren't the tracker: a ticket covers them and says so in its body (`Reported in 0xfurai/peekaping#NNN`). Read one with `gh issue view <n> -R 0xfurai/peekaping --comments`; it holds the reporter's logs, configs and repro steps.

## Keys

- A ticket's key is `PP-<n>`, where `<n>` is its issue number in the tracker repo: `PP-216` is `0xfurai/peekaping-tracker#216`. New tickets continue the sequence.
- Outside the tracker repo (PR titles and bodies, commits, branch and session names, comments on public issues) write `PP-<n>` or `0xfurai/peekaping-tracker#<n>`, never a bare `#<n>`: that is the public repo's issue or PR. Inside the tracker repo `#<n>` is fine.
- Never create tickets in the public repo, and keep ticket text out of public PRs, commits and comments (the key is enough). The tracker repo is private so that security and business tickets stay private.
- PP-1 to PP-216 were imported from Linear (workspace `yvtech`, team Peekaping). Their comments and attachments exist only there, and each body ends with a link to the Linear ticket. Read them with the Linear MCP (`list_comments`, `get_issue`) when a ticket's history matters.

## Project fields

Every ticket is an item on the Project. Set all of these on a new ticket; leave Priority and Linear project empty when unknown.

| Field | Values | Meaning |
| --- | --- | --- |
| Status | Backlog, Todo, In Progress, In Review, Done | Where the ticket is. Closed tickets are Done, including "not planned" and duplicates |
| Priority | Urgent, High, Medium, Low | Empty means none |
| Linear project | Go backend health, Stabilize & restart releases, Peekaping Cloud, Single-container mode, Remote probes, Uptime Kuma parity | The area of work. Empty means none |
| Milestone | a milestone of the tracker repo, e.g. `2 — Correctness bugs` | The step inside an area. `gh api repos/0xfurai/peekaping-tracker/milestones` lists them |
| Labels | the triage labels and a category, see `triage-labels.md` | |
| Assignees | `@me` once work starts | |
| Parent issue, Sub-issues progress, Linked pull requests | filled in by sub-issue links and `Fixes` lines | Read only |

Field and option ids, for `gh project item-edit`. If a set fails on a missing option, re-read them with `gh project field-list 4 --owner 0xfurai --format json`.

```
PROJECT=PVT_kwHOAK5MrM4Bn0pj
STATUS=PVTSSF_lAHOAK5MrM4Bn0pjzhmXgIA     # Backlog c49193d2, Todo 71b55d8a, In Progress 447cf969, In Review ddf10221, Done b8200ec9
PRIORITY=PVTSSF_lAHOAK5MrM4Bn0pjzhmX_M4   # Urgent 2baf1eae, High d4669fa5, Medium 270d294c, Low d517ff38
AREA=PVTSSF_lAHOAK5MrM4Bn0pjzhmXuu8       # Go backend health 479a9927, Stabilize & restart releases 6c93177b, Peekaping Cloud 3b8d2c4d,
                                          # Single-container mode 9af8b11c, Remote probes a099d166, Uptime Kuma parity faae6c6c
```

The Project's built-in workflows (item closed, PR merged, and so on) are off, so nothing moves Status for you: set it yourself at each step below.

## Conventions

Below, `R=0xfurai/peekaping-tracker`.

- **Create an issue**: `gh issue create -R $R --title "<title>" --body-file <file> --label needs-triage --label <bug|feature|improvement>`, plus `--milestone "<title>"` when it belongs to one. Then put it on the Project and set its fields:
  ```
  ITEM=$(gh project item-add 4 --owner 0xfurai --url <issue url> --format json --jq .id)
  gh project item-edit --id $ITEM --project-id $PROJECT --field-id $STATUS --single-select-option-id <id>
  ```
  A new ticket starts in Backlog. Repeat the `item-edit` for `$PRIORITY` and `$AREA`. `item-add` on an issue already on the Project returns its existing item id, so it is also how you find an item.
- **Read an issue**: `gh issue view <n> -R $R --comments --json title,body,state,labels,milestone,assignees,comments`. Relations: `gh api repos/$R/issues/<n>/parent`, `.../sub_issues`, `.../dependencies/blocked_by`, `.../dependencies/blocking`. Project fields:
  ```
  gh api graphql -F o=0xfurai -F r=peekaping-tracker -F n=<n> -f query='query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){issue(number:$n){projectItems(first:5){nodes{project{number} status:fieldValueByName(name:"Status"){... on ProjectV2ItemFieldSingleSelectValue{name}} priority:fieldValueByName(name:"Priority"){... on ProjectV2ItemFieldSingleSelectValue{name}} area:fieldValueByName(name:"Linear project"){... on ProjectV2ItemFieldSingleSelectValue{name}}}}}}}'
  ```
- **List issues**: `gh issue list -R $R --state open --label <label> --milestone "<title>" --search "<words> in:title,body" --json number,title,labels --limit 200`. Filtering on Status, Priority or Linear project needs the Project: query `node(id: $PROJECT){... on ProjectV2{items(first:100, after:$cursor){nodes{content{... on Issue{number title state}} status:fieldValueByName(name:"Status"){...}}}}}` as above. `gh project item-list` leaves custom fields out, so don't use it for them.
- **Duplicate check**: search as above with two or three keywords, then list the open issues of the ticket's Linear project and milestone and skim the titles. Search only matches words, so a differently worded duplicate can slip past.
- **Make an issue a sub-issue of a parent**: `gh api -X POST repos/$R/issues/<parent>/sub_issues -F sub_issue_id=$(gh api repos/$R/issues/<child> --jq .id)`.
- **Mark an issue blocked by another**: `gh api -X POST repos/$R/issues/<n>/dependencies/blocked_by -F issue_id=$(gh api repos/$R/issues/<blocker> --jq .id)`. Remove it with `-X DELETE repos/$R/issues/<n>/dependencies/blocked_by/<blocker id>`.
- **Comment on an issue**: `gh issue comment <n> -R $R --body-file <file>`.
- **Edit the description**: read the body with `gh issue view`, change the text locally, write it back with `gh issue edit <n> -R $R --body-file <file>`. Replace only the section you own and leave the rest as written.
- **Apply / remove labels**: `gh issue edit <n> -R $R --add-label X --remove-label Y`. Never set labels with `gh api -X PATCH ... labels[]=`: it replaces the whole set, and an empty list is not sent at all.
- **Assign**: `gh issue edit <n> -R $R --add-assignee @me`.
- **Close**: comment first, then `gh issue close <n> -R $R --reason completed` (finished) or `--reason "not planned"` (won't do). A duplicate gets the `duplicate` label, a comment `Duplicate of PP-<m>`, and the not-planned close. Then set Status to Done.
- **Reopen**: `gh issue reopen <n> -R $R`, and set Status back.
- **Read right after writing**: list endpoints lag a few seconds behind a create. Read the issue by number, not through a list.
- **GraphQL errors**: `Something went wrong while executing your query` is usually transient; retry after a few seconds.

Statuses run Backlog → Todo → In Progress → In Review → Done.

## Pull requests as a triage surface

**PRs as a request surface: no.** The tracker holds no PRs. A community PR worth landing gets its own ticket titled `Merge 0xfurai/peekaping#NNN: <summary>`.

## Triage

- **Unlabeled** means an open issue with none of the five state labels: `gh issue list -R $R --state open --json number,title,labels --limit 300`, then filter the labels.
- **`wontfix`**: add the label, close with `--reason "not planned"`, set Status to Done.
- **Reporter**: for a ticket you wrote, the user. For a community report, the author of the public issue it covers. A `needs-info` question for a community reporter goes on the public issue (`gh issue comment <n> -R 0xfurai/peekaping`) as well as the ticket, and only after the user approves the text. Check both places for replies.

## Closing work

Work closes through PRs. Name the branch `<type>/pp-<n>-<slug>`. Start the PR title with the ticket key, then the conventional-commit summary: `PP-48 fix(server): stop exposing JWT signing secrets`. A PR in a stack of several adds its place after the key: `PP-16 [1/2] feat(server): add a stale-monitor watchdog` (see `.agents/skills/ticket/STACK.md`). When naming or renaming a Claude Code session for ticket work, lead with the key: `PP-48 stop exposing JWT secrets`.

GitHub links a PR to a ticket only through a line in the PR body, and a closing keyword closes the ticket when that PR merges. So put `Fixes 0xfurai/peekaping-tracker#<n>` in the **top** PR of a stack only (the one that merges last), and `Part of 0xfurai/peekaping-tracker#<n>` in the others.

- **PR opens**: set Status to In Review and comment the PR links on the ticket.
- **Stack merged**: check the ticket closed. If the `Fixes` line didn't close it, run `gh issue close <n> -R $R --reason completed`. Then set Status to Done.

## When a skill says "publish to the issue tracker"

Create an issue in the tracker repo and put it on the Project, as in **Create an issue** above.

## When a skill says "fetch the relevant ticket"

Read it as in **Read an issue** above, plus any GitHub issues or PRs it links.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a single issue with **child** issues as tickets. Create the `wayfinder:*` labels with `gh label create -R $R` the first time they're needed.

- **Map**: an issue labelled `wayfinder:map`, holding the Notes / Decisions-so-far / Fog body.
- **Child ticket**: a sub-issue of the map, labelled `wayfinder:<type>` (`research`/`prototype`/`grilling`/`task`). Once claimed, the ticket is assigned to the driving dev.
- **Blocking**: GitHub's "blocked by" dependency, set as in **Mark an issue blocked by another**. A ticket is unblocked when every blocker is closed.
- **Frontier query**: list the map's sub-issues (`gh api repos/$R/issues/<map>/sub_issues`), keep open unassigned tickets, drop any whose `.../dependencies/blocked_by` has an open issue; first in the listed order wins.
- **Claim**: `--add-assignee @me` and Status In Progress, the session's first write.
- **Resolve**: comment the answer, close with `--reason completed`, set Status Done, then append a context pointer (gist + link) to the map's Decisions-so-far with **Edit the description**.
