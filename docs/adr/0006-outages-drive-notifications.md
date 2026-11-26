# 0006: Outages drive notifications, not important heartbeats

Status: Proposed
Date: 2026-10-25
Ticket: PP-338

## Context

Peekaping decides when to notify while it stores each result (`Ingest` in `apps/server/internal/modules/ingester/service.go`). It compares the monitor's status before the result with its status after, and sets two flags on the stored heartbeat:

- **`Important`**: the heartbeat moves the monitor to Up or Down from a different status, or is the monitor's first heartbeat (`isImportantBeat`).
- **`Notified`**: the ingester decided to send a notification for it (`isImportantForNotification`, or the resend interval running out).

When it decides to notify, it publishes `events.ImportantHeartbeat` with the heartbeat. The notification listener (`apps/server/internal/modules/notification_channel/listener.go`) sends that heartbeat to the monitor's channels, and each channel type works out from `hb.Status` what to say.

The logic is a port of Uptime Kuma's `isImportantBeat` and `isImportantForNotification` (`server/model/monitor.js`). Detecting status changes and alerting on them is the right model for an uptime monitor, and this ADR keeps it. What it changes is where the result of that detection lives. Today one flag on one heartbeat row does three jobs, and each job suffers from it:

1. **Event history.** The monitor page's event list and its "since" duration query heartbeats with `important=true`. Heartbeats are deleted after the data retention period, so the history goes with them (PP-188).
2. **Notification trigger.** `ImportantHeartbeat` is also published for resends, whose heartbeats have `Important = false`, so the event's name doesn't say what it means. A channel type can't tell a resend from a first Down notification, and can't tell one outage from the next. PagerDuty dedupes every notification of a monitor by `Peekaping/<monitor ID>` (`providers/pagerduty.go`): every outage of a monitor shares one incident, and certificate expiry and stale-monitor notifications merge into it (PP-136, PP-281).
3. **Delivery record.** `Notified` is set before anything is sent. A send that fails is logged and dropped (`listener.send`), and the heartbeat still says notified. The heartbeat is stored and the event published in two steps, so a crash or a failed publish between them loses the notification. In cluster mode the `notification` queue (ADR 0002, PP-29) retries handing the task to an api replica, not a channel that failed to send.

The rules themselves live in two boolean helpers with no row for Maintenance. That's how Maintenance → Down lost its notification in the port (PP-131). PP-134 and PP-135 are two more rules that are hard to see in that shape.

## Decision

### 1. An outage is a record of its own

An **outage** is the period a monitor is Down: from the heartbeat that took it Down to the heartbeat that brought it back Up. It's stored in an `outages` table, apart from heartbeats:

| Column | Content |
| --- | --- |
| `id` | the outage's ID |
| `monitor_id` | the monitor |
| `started_at`, `ended_at` | when it started, and when it ended (null while it's open) |
| `start_heartbeat_id`, `end_heartbeat_id` | the heartbeats that opened and closed it |
| `message` | the message of the heartbeat that opened it |
| `last_notified_at`, `reminders` | when its last notification went out, and how many resends it has had |

A monitor has at most one open outage, enforced by a partial unique index on SQLite and PostgreSQL.

The outage is the monitor's, not a location's. The result that moves the monitor to Down opens it, and the result that moves it to Up closes it, as `carriesChange` already decides for the status. The outage is opened or closed in the same transaction that records the location (`LocationStore.RecordLocation`), so two results of one monitor stored at once can't open two outages.

Outages are kept past the data retention period. How long is PP-188's question, narrowed from heartbeats to outages. A migration builds outages from the existing important heartbeats once.

Maintenance and pausing don't close an open outage: the monitor wasn't seen to recover. Deleting the monitor deletes its outages.

### 2. One function decides what a status change does

A pure function takes the monitor's status before and after the result, its open outage and its settings, and returns one action: **open**, **close**, **remind** or **nothing**. Its tests are one table with a row for every pair of statuses, including Maintenance and the first heartbeat. PP-131, PP-134 and PP-135 each change rows of that table.

### 3. A notification says what it is

`ImportantHeartbeat` is replaced by a notification event that names its kind:

- **down**: an outage opened
- **up**: an outage closed
- **reminder**: the resend interval ran out on an open outage

It carries the outage and the heartbeat. Certificate expiry and stale-monitor notifications get kinds of their own. Channel types read the kind and stop inferring it from `hb.Status`. Incident tools (PagerDuty, Opsgenie, PagerTree, Grafana OnCall) dedupe outage notifications by `Peekaping/<monitor ID>/outage/<outage ID>`, so each outage is one incident.

This changes one of the four events ADR 0002 routes through the cluster `notification` queue: "an important heartbeat" becomes the outage notification. The routing stays as ADR 0002 describes it.

### 4. A notification is delivered, or we know it wasn't

- The ingester writes the notification to an outbox table in the same transaction as the heartbeat. A sender reads the outbox, so a crash or restart between storing and sending loses nothing, in single and cluster mode.
- Each channel's delivery is recorded: attempts, last error, sent at. A failed send is tried again, on that channel alone, with backoff for a bounded time.
- A notification has a stable ID, so a retried task doesn't send twice.

This part doesn't depend on sections 1 to 3 and can land first. It covers PP-181.

### 5. What happens to the heartbeat flags

`Notified` is replaced by the delivery records. `Important` stays while the outage work lands, then is dropped or kept only to highlight status changes in the bar. The heartbeats API keeps its `important` filter until the web no longer uses it.

## Not decided here

- Whether the resend interval counts time instead of heartbeats. It changes a setting, so it gets its own ticket if wanted.
- Waiting for several Up checks before an outage ends, to quiet a flapping service (PP-342). It's a new setting and optional.

## Considered and rejected

- **Keep the flags and fix the rules.** PP-131, PP-134 and PP-135 are worth doing either way, and they're small. They don't give history that outlives retention, an incident per outage, or delivery that survives a failed send.
- **Level-based alert rules, as Prometheus Alertmanager has.** Alertmanager evaluates a condition on every pass and fires while it holds, with `for:` before it fires and `repeat_interval` between repeats. Max retries and the resend interval already play those two parts, and a monitor has one condition, its status. The edge-triggered model fits; what's missing is the record of the outage.
- **Call it an incident.** Incident tools use the word for their own record, which a Peekaping outage opens, and status pages commonly use it for a message posted by a person. The glossary already steers away from "incident" for important heartbeats. "Outage" says what it is: the time a monitor was Down.

## Consequences

- The monitor page reads its event history from outages, which survive the hourly cleanup.
- Each outage is one incident in PagerDuty and the other incident tools. A missed resolve no longer swallows the next outage, and certificate and stale notifications stop landing in it.
- Every channel type changes how it reads a notification, and its golden files change with it (`providers/testdata`).
- A new table, `outages`, and an outbox with delivery records on SQLite and PostgreSQL, with migrations. ADR 0005 scopes outages through the monitor, like heartbeats.
- GLOSSARY.md gains **Outage** when this is accepted, and **Important heartbeat** is reworded or removed once the flag goes.

## Tickets

- PP-338: parent
- PP-339: record outages, and the decision function (sections 1, 2)
- PP-340: the notification of a kind, and a dedup key per outage (section 3)
- PP-341: outbox, delivery records and retries (section 4)
- PP-342: optional, Up checks before an outage ends
