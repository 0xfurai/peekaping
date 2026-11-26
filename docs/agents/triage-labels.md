# Triage Labels

The skills speak in terms of five canonical triage roles. This file maps those roles to the actual label strings used in this repo's issue tracker (labels in the Linear **Peekaping** team).

| Label in mattpocock/skills | Label in our tracker | Meaning                                  |
| -------------------------- | -------------------- | ---------------------------------------- |
| `needs-triage`             | `needs-triage`       | Maintainer needs to evaluate this issue  |
| `needs-info`               | `needs-info`         | Waiting on reporter for more information |
| `ready-for-agent`          | `ready-for-agent`    | Fully specified, ready for an AFK agent  |
| `ready-for-human`          | `ready-for-human`    | Requires human implementation            |
| `wontfix`                  | `wontfix`            | Will not be actioned                     |

When a skill mentions a role (e.g. "apply the AFK-ready triage label"), use the corresponding label string from this table.

Edit the "Label in our tracker" column to match whatever vocabulary you actually use.

## Category roles

The two category roles map to Linear's existing workspace labels. Apply these; don't create `bug` or `enhancement`.

| Role in mattpocock/skills | Label in our tracker | When                                      |
| ------------------------- | -------------------- | ----------------------------------------- |
| `bug`                     | `Bug`                | Something is broken                       |
| `enhancement`             | `Feature`            | A new capability                          |
| `enhancement`             | `Improvement`        | A change to existing behavior             |
