# Peekaping

Self-hosted uptime monitoring. Peekaping checks services on a schedule, records each result, tells people when a service goes down or recovers, and shows the state on public status pages.

## Language

### Monitoring

**Monitor**:
A target Peekaping watches (a URL, host, container or other service), with the settings for how it's checked.
_Avoid_: Check (that's one run), probe, service

**Monitor type**:
The kind of check a monitor runs, such as HTTP, TCP, ping, DNS, Docker or push.
_Avoid_: Connection type, check type

**Check**:
One attempt by a monitor to reach its target and judge whether it's healthy.
_Avoid_: Health check, probe, ping

**Heartbeat**:
The recorded result of one check, or of one push to a push monitor: a status, a message and a response time.
_Avoid_: Beat, ping, health check result

**Push monitor**:
A monitor that runs no checks of its own. The monitored service sends heartbeats to the monitor's push URL, and a push that doesn't arrive in time counts as Down.
_Avoid_: Passive monitor, heartbeat monitor

**Push URL**:
The unique URL a push monitor receives heartbeats on. Its secret part is the push token.
_Avoid_: Webhook, push endpoint

**Check interval**:
The time between a monitor's checks. For a push monitor, it's how long the monitor waits for the next push.
_Avoid_: Heartbeat interval, frequency

**Response time**:
How long a check took to get its answer, in milliseconds.
_Avoid_: Ping (that's a monitor type), latency

**Uptime**:
The share of a monitor's heartbeats in a period that were Up.
_Avoid_: Availability, SLA

**Paused**:
A monitor that's switched off. It runs no checks until it's resumed.
_Avoid_: Inactive, disabled, stopped

**Stale monitor**:
An active monitor Peekaping has stopped checking: its latest heartbeat is older than max(3 × check interval, check interval + timeout + 60s). The watchdog looks for them every minute, gets each one checked again right away, and notifies the monitor's channels if that doesn't help. A monitor that doesn't run on the local probe isn't checked again from here: its channels are notified once that same wait has passed.
_Avoid_: Stuck monitor, dead monitor, overdue

**Probe**:
A place Peekaping runs checks from. A monitor runs on one or more probes, the local probe unless it's assigned to remote ones.
_Avoid_: Agent, location, worker

**Local probe**:
The probe that is Peekaping itself: its own process checks the monitors on it. Every install has it, and it can't be deleted.
_Avoid_: Built-in probe, default probe

**Remote probe**:
A probe that runs on another machine. It connects out to Peekaping with its token, gets the monitors assigned to it, and sends back their results.
_Avoid_: Private location, remote agent

**Probe instance**:
One running copy of a probe's program. Every copy started with the probe's token is an instance of that probe, with a random id of its own, and the probe's monitors are dealt among its instances.
_Avoid_: Agent, worker, node; "instance ID" alone, which is the usage ping's

**Assignment**:
The monitors a probe instance checks, with what each check needs, as the server deals them. The instance keeps checking its last assignment while it can't reach the server.
_Avoid_: Job list, schedule

### Status

**Status**:
A monitor's current state: Up, Down, Pending or Maintenance. Each heartbeat records one.
_Avoid_: State, health

**Up**:
The monitor's latest check succeeded.
_Avoid_: Online, healthy

**Down**:
The monitor's checks are failing and its max retries are used up.
_Avoid_: Offline, failed

**Pending**:
The monitor's latest check failed, but it still has retries left before it counts as Down. Pending is also the status of a new monitor before its first check.
_Avoid_: Degraded, warning, retrying

**Maintenance**:
The status of a monitor inside an active maintenance window. Its checks count as neither Up nor Down.
_Avoid_: Paused (that's a monitor switched off)

**Important heartbeat**:
A heartbeat that moves its monitor to Up or Down from a different status. A monitor's event history and notifications come from its important heartbeats.
_Avoid_: Event, incident, status change

**Max retries**:
How many consecutive failed checks a monitor spends in Pending before it becomes Down. Zero means the first failure is Down.
_Avoid_: Retries, retry count

**Retry interval**:
The time between checks while a monitor is Pending.
_Avoid_: Heartbeat retry interval

### Notifications

**Notification channel**:
A configured destination for notifications, such as an email address, a Slack webhook or a Telegram chat. A monitor sends to the channels attached to it.
_Avoid_: Notifier, notification provider, integration

**Channel type**:
The kind of destination a notification channel sends to, such as Slack, Telegram or SMTP. Each has its own config fields and form (`channel-types/<type>/`).
_Avoid_: Integration, provider, notifier type

**Saved secret**:
A secret field of a notification channel's config, such as a bot token, an SMTP password or a webhook URL that works as a credential. The API never returns it: it shows `********` instead, and a save or test that sends `********` back keeps the stored value.
_Avoid_: Credential, masked field, hidden value

**Endpoint field**:
A config field that decides where a notification channel sends, such as a server URL, an SMTP host and port, or the webhook URL. Saved secrets are kept only while the endpoint fields stay as they are; changing one means entering the saved secrets again, so a secret is never sent somewhere new.
_Avoid_: Target field, destination, address

**Notification**:
A message sent to a monitor's notification channels when the monitor goes Down or comes back Up. Its certificate expiry notifications, and a stale monitor's "not checked since" and "checking again" messages, go to the same channels.
_Avoid_: Alert

**Resend interval**:
How many consecutive Down heartbeats a monitor waits before sending its Down notification again. It counts heartbeats, not seconds; zero turns resending off.
_Avoid_: Reminder interval, repeat alert

**Certificate expiry notification**:
A notification sent when a monitor's TLS certificate is close to expiring, once for each configured threshold (for example 21, 14 and 7 days before).
_Avoid_: TLS alert, cert warning

### Maintenance

**Maintenance window**:
A scheduled period during which its affected monitors report Maintenance, so planned downtime raises no notifications.
_Avoid_: Maintenance (that's the status), downtime, scheduled outage

**Maintenance strategy**:
How a maintenance window's timing is defined: switched on and off by hand, a single period, recurring (every N days, on chosen weekdays, or on chosen days of the month), or a cron expression.
_Avoid_: Schedule type

### Sharing

**Status page**:
A public page showing the status and uptime of a chosen set of monitors. Once published, it's served at its slug and at any custom domains.
_Avoid_: Dashboard, public page

**Custom domain**:
A domain name that serves a status page, such as `status.example.com`.
_Avoid_: Domain status page, CNAME

**Badge**:
An embeddable image showing one live figure about a monitor: its status, uptime, response time or certificate expiry.
_Avoid_: Shield, widget

### Organizing

**Tag**:
A named, colored label attached to monitors for filtering.
_Avoid_: Label, group, category

**Data retention period**:
How many days heartbeats are kept before they're deleted.
_Avoid_: Keep-data period, history limit

### Deployment

**Storage backend**:
The database an install keeps its data in: SQLite, PostgreSQL or MongoDB. It's set with `DB_TYPE`. MongoDB is deprecated and frozen under [ADR 0001](docs/adr/0001-deprecate-mongodb-storage.md); new installs use SQLite or PostgreSQL.
_Avoid_: Database type, DB driver

**MongoDB monitor type**:
A monitor type that checks someone else's MongoDB server. It has nothing to do with the storage backend and isn't deprecated.
_Avoid_: Mongo storage, MongoDB database

**Single mode**:
Peekaping running as one process (the `peekaping` binary) with every role inside it (api, producer, worker and ingester) and no Redis. The SQLite and PostgreSQL bundle images run it. It supports those two storage backends only. See [ADR 0002](docs/adr/0002-single-process-runtime.md).
_Avoid_: Monolithic mode, standalone, all-in-one

**Cluster mode**:
Peekaping running as separate api, producer, worker and ingester processes that talk through Redis. Helm, the compose stacks and the Mongo bundle run it.
_Avoid_: Microservice mode, distributed mode, HA mode

### Telemetry

**Usage ping**:
An anonymous summary of how an install is set up, sent at most once a day to Peekaping's telemetry Worker. It is on until an admin opts out, and the environment kill switch `TELEMETRY=off` turns it off whatever the admin chose (`DO_NOT_TRACK` is not read: it was dropped on 2026-10-22). It holds the version, mode, storage backend, OS, a bucket for the number of monitors, the monitor types and notification channel types in use (names of kinds, nothing about individual monitors), and which features are on. The UI calls the feature "usage statistics" (the setup checkbox "Send anonymous usage statistics" has its label fixed by the ticket, and it starts ticked). It is the same thing as the usage ping, not a second one. An install that upgrades shows its admin a one-time **upgrade notice** that the usage ping and error reports are on. See [ADR 0003](docs/adr/0003-telemetry-and-error-reporting.md).
_Avoid_: Analytics, phone-home, tracking, heartbeat (that's a monitor's result), ping (that's a monitor type)

**Instance ID**:
The random UUID an install creates the first time it is needed while the usage ping is on, and deletes when an admin opts out. It is the only identifier in a usage ping, and it comes from nothing about the install. Opting in again creates a new one.
_Avoid_: Install ID, machine ID, user ID

**Error report**:
A crash report sent to Sentry. It is on until an admin opts out, separately from the usage ping, and the environment kill switch `ERROR_REPORTING=off` turns it off whatever the admin chose. It holds the error type; the stack frames (function, package, file, line, and whether it is Peekaping's own code); the release; the level (`error` or `fatal`); the tags `mode`, `db`, `os` and `component` (where the panic was recovered); and the event ID, timestamp, platform `go` and the DSN (the project's address, in the envelope header) the Sentry protocol needs. It holds no message text, no IP address and nothing else. Only panics are reported for now, and one place reports at most once a minute. See [ADR 0003](docs/adr/0003-telemetry-and-error-reporting.md).
_Avoid_: Crash dump, error log, bug report (that's what a person files)
