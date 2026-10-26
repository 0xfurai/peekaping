# 0003: Telemetry and error reporting, on by default with an opt-out

Status: Accepted, amended 2026-10-22
Date: 2026-10-21
Ticket: #232, #233

> **Amended 2026-10-22: changed to opt-out.** This ADR first decided that the usage ping and error reports are off by default and need an opt-in. They are now on by default, and an admin opts out. Section 1 is the decision as it stands. The original decision and the reasons it gave against opt-out stay below, under "Original decision (2026-10-21)" and "Rejected", so the change can be read in full. The kill switches changed too, as the next paragraph says. Everything else in this ADR (the two allowlists, where the data goes) is unchanged.
>
> **Also amended 2026-10-22: `DO_NOT_TRACK` was dropped.** The kill switches are `TELEMETRY=off` and `ERROR_REPORTING=off` (section 2), and `DO_NOT_TRACK=1` no longer turns anything off.
>
> `ERROR_REPORTING_DSN` (section 4) still works as before and stays undocumented on purpose.

## Context

Peekaping is self-hosted, and we can't see how it is used or when it crashes. We don't know which versions, modes, storage backends and monitor types are in use, so we can't tell what is safe to change or remove. A crash on someone's server reaches us only if they file an issue.

Two tickets add what is missing. #232 adds a **usage ping** (a small anonymous summary of how an install is set up). #233 adds **error reports** (crash reports). Both send data off servers that watch their owners' private infrastructure, so they share one set of rules, set once here and not argued again in each ticket.

## Decision

### 1. On by default, two separate opt-outs

Both are on until an admin turns them off. Each has its own switch: the setup screen asks with a box that starts ticked, and Settings changes it later. Opting out of one does not opt out of the other. The admin's choice is stored when they make it, on the setup screen or in Settings, and a stored choice always wins over the default.

An install that upgrades from a version without them starts sending too. So its admin is told once. A banner in the app says that usage statistics and error reports are on, what each sends and never sends, and links to the cards in Settings that turn them off. The server keeps whether it was shown in the `TELEMETRY_NOTICE_SEEN` setting, which the settings API can't reach. Dismissing the banner, or making a choice for either feature, sets it. It is not shown when the environment switches both features off, and it never comes back on a later upgrade. A new install's admin has just made the choice on the setup screen, so they don't see it.

The reason for the change is the consequence the first version already named: with an opt-in, the numbers count only the installs that chose to send, and a crash reaches us only from an install that chose to report it. The opt-out keeps the rules that made the opt-in acceptable: the kill switches in section 2, the allowlists in sections 3 and 4, a notice, and a switch in Settings.

#### Original decision (2026-10-21)

Off by default, two separate opt-ins. Neither was on until an admin turned it on. Each had its own switch, asked on the setup screen and changeable in Settings. Opting in to one did not opt in to the other. An install that upgraded from a version without them stayed off until an admin chose.

### 2. Environment kill switches

Two environment variables override the settings and force the sending off:

- `TELEMETRY=off` turns the usage ping off.
- `ERROR_REPORTING=off` turns error reports off.

While one applies, nothing is sent for that feature, whatever an admin chose. To send neither, set both. `DO_NOT_TRACK` was a third switch for both at once, and was dropped on 2026-10-22: it is not read.

### 3. The usage ping is an allowlisted payload

The payload has exactly these fields, and the server builds it from them, field by field. It never starts from a larger record and strips out what to hide.

| Field | Content |
| --- | --- |
| `instance_id` | the random instance ID |
| `version` | the Peekaping version |
| `mode` | `single` or `cluster` |
| `db` | the storage backend: `sqlite`, `postgres` or `mysql`. |
| `os` | OS and architecture, such as `linux/arm64` |
| `monitors` | how many monitors, as a bucket: `0`, `1-10`, `11-50`, `51-200`, `201-1000` or `1000+` |
| `monitor_types` | the monitor types in use, as type names only |
| `notification_providers` | the notification channel types in use, as type names only |
| `features` | whether status pages, maintenance windows and metrics are on |

Counts are bucketed, and types are names of kinds, never of individual things: no monitor name, URL, host, tag, status page slug or channel setting is sent.

- **Instance ID.** A random UUID, created the first time it is needed while the usage ping is on, and deleted when an admin opts out. An install that never chose gets one on its first state read or usage ping, soon after it starts; replicas of a cluster that start together end with one ID, because the ID is stored with a create-if-absent in one statement. Opting in again creates a new one. None is made while `TELEMETRY=off` applies. It comes from nothing about the install.
- **Frequency.** At most once a day, from one process: the one process in single mode, and in cluster mode the api replica that wins a Redis `SET NX` claim, which holds for 23 hours. The api has the config the payload reads, such as `METRICS_ENABLED`; the producer leader does not.
- **Destination.** Our own Cloudflare Worker at `telemetry.peekaping.com/v1/ping` (`apps/telemetry`), which writes to Workers Analytics Engine. The data is kept for 3 months. The Worker never reads the client IP, the country or `request.cf`, and stores nothing of the request but the validated payload fields. Of the request it reads only the method, the path, the `Content-Length` header and the body, to route it and turn away a body that is too large.
- **The Worker validates strictly.** It accepts only this payload and answers 400 to any extra field or bad value. A later change in the server can't widen what we collect without a change to the Worker in this repository.

### 4. Error reports go to Sentry, through an allowlist

Error reports use the Sentry SDK. Its `BeforeSend` hook (`apps/server/internal/reporting/event.go`) builds a new event from the allowlist below. It copies nothing else, so a field the SDK or our code adds later is not sent either.

| Part | Content |
| --- | --- |
| Exception type | the Go type of the panic value, such as `runtime.boundsError`. Never its text. |
| Stack frames | the innermost 100, each with `function`, `module` (the package), `filename`, `lineno` and `in_app` (whether it is Peekaping's own code). The file is the package path and the file name, never a path on the machine that built the binary. |
| `release` | the Peekaping version |
| `level` | `error` for a panic that was recovered, `fatal` for one that ends the process |
| `mode` tag | `single` or `cluster` |
| `db` tag | the storage backend: `sqlite`, `postgres` or `mysql`, or `unknown` where the process has none. The cluster worker has no `DB_TYPE`, so its `db` tag is `unknown`. |
| `os` tag | OS and architecture, such as `linux/arm64` |
| `component` tag | where the panic was recovered, such as `http`, `asynq`, `executor` or `scheduler` |
| event ID, timestamp, `platform`, envelope `dsn` | a random event ID, the time of the panic in UTC, `go`, and the DSN in the envelope header, which is the address of the project the report goes to. The Sentry protocol needs them to take the event. |

The `mode`, `db` and `component` values are checked: one that is not a short name from the code or the build is sent as `unknown`.

Everything else is dropped: the message text and the exception value (they can hold hostnames and URLs), request data, user, breadcrumbs, server name, local variables and extra context.

- **Only panics.** For now only panics are reported, not every logged error. A panic that was recovered goes on as before. One that ends the process is reported and flushed, for at most 2 seconds, and then raised again.
- **Once a minute for each place.** A place is the component, the exception type and the innermost frame. A check that panics on every run sends one report a minute, not one for each run. A panic that ends the process is always reported.
- **`http.ErrAbortHandler` is not a crash.** `httpx.Recovery` re-panics it instead of reporting it or answering 500, because it is net/http's own way for a handler to abort its response.
- **No IP address.** A report holds none, and Peekaping's Sentry project is set not to store the sender's IP. The code can't enforce that setting. With `ERROR_REPORTING_DSN` set to your own Sentry or GlitchTip, its settings decide.
- **Known limitation: `sent_at`.** sentry-go writes the envelope header `sent_at` (when the report was sent) in the server's local time, so the server's UTC offset reaches Sentry. `BeforeSend` can't change an envelope header. The event's own timestamp is in UTC.

The DSN is the only coupling to Sentry. Moving to GlitchTip, or to a server we run, later is a change of DSN.

### Rejected

- **Off by default, with an opt-in** (the original decision). It leaves the numbers as a floor and the crashes mostly unseen. The objection it was chosen to avoid, "installs would send before anyone chose to", is the cost of the opt-out, and the upgrade notice, the setup screen that shows both boxes, the switch in Settings and the kill switches are how that cost is kept small.
- **Removing known-sensitive fields from whatever the SDK collects.** A new field in the SDK or in our code would be sent without anyone deciding to. The allowlist sends only what is named.

## Consequences

- The numbers count the installs that haven't opted out or set a kill switch, so they are closer to a census, but still a floor: an operator who sets `TELEMETRY=off` or blocks the Worker isn't counted. Opting out and in again gives a new instance ID, so one install can be counted twice over time.
- Upgrading starts sending without a choice from the admin. The notice is the one place that says so, and the kill switches are for operators who must be sure nothing goes out. An operator who needs that before the first start sets them in the image or the compose file, as the install guides show.
- The default has to be kept out of the repository's own dev and test setups: the dev compose files, the e2e stack and the Go tests all point the ping and the reports at an address nothing listens on, and a guard test fails on a test that could reach the built-in Sentry DSN.
- Adding a field to either payload is a decision. It changes this ADR, the Worker's validator (or the `BeforeSend` allowlist) and the docs, which must list exactly what is sent. The Worker rejects the new field until it is changed.
- An operator who wants to be sure sets the environment variables in their image or compose file, before the first start.
- The usage ping numbers are read with `bun run report` in `apps/telemetry`, which needs a Cloudflare token with Account Analytics read access.
- Only panics are reported, so an error that is handled and logged is invisible to us.

Waiting tickets:

- **#232** (usage ping): the Worker in `apps/telemetry`, then the server side (the choice, the instance ID, the payload, the daily send) and the setup screen and Settings.
- **#233** (error reports): the Sentry SDK, the `BeforeSend` allowlist, and its own switch on the setup screen and in Settings.

## Update

Date: 2026-10-25
Ticket: PP-277

The producer, worker and ingester are removed (ADR 0002). Some statements above no longer hold:

- Section 3 says the api sends the usage ping in cluster mode because it has the config the payload reads and the producer leader does not. There is no producer leader. The api still sends it.
- Section 4's `db` tag row says the cluster worker has no `DB_TYPE`, so its `db` tag is `unknown`. The worker is gone. The api and `peekaping` are the only processes that send error reports, and both have a `DB_TYPE`. `unknown` stays for a value that names none of the backends.

The api used to copy the admin's error reporting choice to Redis, so that the producer, worker and ingester could follow it. The copy is gone with them. The cluster api turns error reports on or off in its own process, as `peekaping` does. It applies the stored choice at start and again every minute, so a change made on another api replica reaches it within a minute. The stored setting is still the source of truth, and the kill switch, the allowlist and the destination are unchanged.
