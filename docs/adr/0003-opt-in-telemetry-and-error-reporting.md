# 0003: Opt-in telemetry and error reporting

Status: Accepted
Date: 2026-10-21
Ticket: #232, #233

## Context

Peekaping is self-hosted, and we can't see how it is used or when it crashes. We don't know which versions, modes, storage backends and monitor types are in use, so we can't tell what is safe to change or remove. A crash on someone's server reaches us only if they file an issue.

Two tickets add what is missing. #232 adds a **usage ping** (a small anonymous summary of how an install is set up). #233 adds **error reports** (crash reports). Both send data off servers that watch their owners' private infrastructure, so they share one set of rules, set once here and not argued again in each ticket.

## Decision

### 1. Off by default, two separate opt-ins

Neither is on until an admin turns it on. Each has its own switch, asked on the setup screen and changeable in Settings. Opting in to one does not opt in to the other. An install that upgrades from a version without them stays off until an admin chooses.

### 2. Environment kill switches

Three environment variables override the settings and force the sending off:

- `TELEMETRY=off` turns the usage ping off.
- `ERROR_REPORTING=off` turns error reports off.
- `DO_NOT_TRACK=1` turns both off.

While one applies, nothing is sent, whatever an admin chose.

### 3. The usage ping is an allowlisted payload

The payload has exactly these fields, and the server builds it from them, field by field. It never starts from a larger record and strips out what to hide.

| Field | Content |
| --- | --- |
| `instance_id` | the random instance ID |
| `version` | the Peekaping version |
| `mode` | `single` or `cluster` |
| `db` | the storage backend: `sqlite`, `postgres` or `mongo` |
| `os` | OS and architecture, such as `linux/arm64` |
| `monitors` | how many monitors, as a bucket: `0`, `1-10`, `11-50`, `51-200`, `201-1000` or `1000+` |
| `monitor_types` | the monitor types in use, as type names only |
| `notification_providers` | the notification channel types in use, as type names only |
| `features` | whether status pages, maintenance windows and metrics are on |

Counts are bucketed, and types are names of kinds, never of individual things: no monitor name, URL, host, tag, status page slug or channel setting is sent.

- **Instance ID.** A random UUID, created when an admin opts in and deleted when they opt out. Opting in again creates a new one. It comes from nothing about the install.
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
| `db` tag | the storage backend: `sqlite`, `postgres`, `mongo` or `mysql`, or `unknown` where the process has none. The cluster worker has no `DB_TYPE`, so its `db` tag is `unknown`. |
| `os` tag | OS and architecture, such as `linux/arm64` |
| `component` tag | where the panic was recovered, such as `http`, `asynq`, `executor` or `scheduler` |
| event ID, timestamp, `platform` | a random event ID, the time of the panic in UTC, and `go`. The Sentry protocol needs them to take the event. |

The `mode`, `db` and `component` values are checked: one that is not a short name from the code or the build is sent as `unknown`.

Everything else is dropped: the message text and the exception value (they can hold hostnames and URLs), request data, user, breadcrumbs, server name, local variables and extra context.

- **Only panics.** For now only panics are reported, not every logged error. A panic that was recovered goes on as before. One that ends the process is reported and flushed, for at most 2 seconds, and then raised again.
- **Once a minute for each place.** A place is the component, the exception type and the innermost frame. A check that panics on every run sends one report a minute, not one for each run. A panic that ends the process is always reported.
- **`http.ErrAbortHandler` is not a crash.** `httpx.Recovery` re-panics it instead of reporting it or answering 500, because it is net/http's own way for a handler to abort its response.
- **No IP address.** A report holds none, and Peekaping's Sentry project is set not to store the sender's IP. The code can't enforce that setting. With `ERROR_REPORTING_DSN` set to your own Sentry or GlitchTip, its settings decide.

The DSN is the only coupling to Sentry. Moving to GlitchTip, or to a server we run, later is a change of DSN.

### Rejected

- **On by default, with an opt-out.** Installs would send before anyone chose to.
- **Removing known-sensitive fields from whatever the SDK collects.** A new field in the SDK or in our code would be sent without anyone deciding to. The allowlist sends only what is named.

## Consequences

- The numbers count the installs that opted in, so they are a floor, not a census. Opting out and in again gives a new instance ID, so one install can be counted twice over time.
- Adding a field to either payload is a decision. It changes this ADR, the Worker's validator (or the `BeforeSend` allowlist) and the docs, which must list exactly what is sent. The Worker rejects the new field until it is changed.
- An operator who wants to be sure sets the environment variables in their image or compose file.
- The usage ping numbers are read with `bun run report` in `apps/telemetry`, which needs a Cloudflare token with Account Analytics read access.
- Only panics are reported, so an error that is handled and logged is invisible to us.

Waiting tickets:

- **#232** (usage ping): the Worker in `apps/telemetry`, then the server side (the opt-in, the instance ID, the payload, the daily send) and the setup screen and Settings.
- **#233** (error reports): the Sentry SDK, the `BeforeSend` allowlist, and its own switch on the setup screen and in Settings.
