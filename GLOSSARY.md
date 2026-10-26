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
The share of a monitor's heartbeats in a period that were Up. A probe's uptime is the share of a range it was online (see Probe availability).
_Avoid_: Availability (that's a probe's online and offline history), SLA

**Paused**:
A monitor that's switched off. It runs no checks until it's resumed.
_Avoid_: Inactive, disabled, stopped

**Stale monitor**:
An active monitor Peekaping has stopped checking: its latest heartbeat is older than max(3 × check interval, check interval + timeout + 60s). The watchdog looks for them every minute, gets each one checked again right away, and notifies the monitor's channels if that doesn't help. A monitor that doesn't run on the local probe isn't checked again from here, and neither is any monitor in cluster mode on SQLite or PostgreSQL, where probe instances do the checks: its channels are notified once that same wait has passed. A monitor whose probe is offline isn't stale: the probe's own notification says so.
_Avoid_: Stuck monitor, dead monitor, overdue

**Probe**:
A place Peekaping runs checks from. A monitor runs on one or more probes, the local probe unless it's assigned to remote ones. Probes belong to the install, not to a workspace: once workspaces are built, any workspace's monitors can run on them, and only instance admins manage them.
_Avoid_: Agent, worker

**Local probe**:
The probe that is Peekaping itself. Every install has it, and it can't be deleted. In single mode the `peekaping` process checks its monitors and the local probe takes no probe instances: `peekaping` refuses a `peekaping-probe` that uses its token, and ignores `PROBE_BOOTSTRAP_TOKEN`. In cluster mode on SQLite and PostgreSQL its probe instances do: `peekaping-probe` copies started with its token (`PROBE_BOOTSTRAP_TOKEN` on the api, the same value as `PROBE_TOKEN` on the probe), so it is offline while none is connected, as a remote probe is.
_Avoid_: Built-in probe, default probe

**Remote probe**:
A probe that runs on another machine. It connects out to Peekaping with its token, gets the monitors assigned to it, and sends back their results.
_Avoid_: Private location, remote agent

**Probe instance**:
One running copy of a probe's program. Every copy started with the probe's token is an instance of that probe, with a random id of its own, and the probe's monitors are dealt among its instances. Each instance lists the monitor types its build runs (the full image and the slim one differ), a monitor goes only to instances that run its type, and the probe runs the types of its online instances together.
_Avoid_: Agent, worker, node; "instance ID" alone, which is the usage ping's

**Probe availability**:
When a probe was online and offline over the last 24 hours, 7 days or 30 days, and its uptime over that range. It comes from the probe's status changes, each dated when the status really changed: a probe that went offline did so when its instances were last seen, not when Peekaping noticed. It counts from when the probe's history begins, its creation or the upgrade that started recording it, and changes older than the data retention period are deleted, except the one that says what the probe was at the cutoff. The local probe in single mode has none: the `peekaping` process checks its monitors.
_Avoid_: Probe uptime chart, probe SLA

**Instance connection**:
A stretch of time a probe instance was connected: from its hello, or its first request after more than a minute of quiet, to its bye or its last request. An instance's connections are kept as long as the instance is, a day after it was last seen.
_Avoid_: Session

**Location**:
A monitor's results from one of its probes: that probe's latest status, response time and heartbeats for the monitor. A monitor on three probes has three locations. A monitor's status follows its locations: it is Down while its location rule is met, otherwise Maintenance while any location is in Maintenance, otherwise Up while any is Up, otherwise Pending. A location whose probe is offline, or that hasn't reported yet, doesn't count. Only a result can change the monitor's status, and only toward its own: an Up result never takes the monitor Down, a Down result never takes it Up, and a Down monitor stays Down while another location retries, until an Up result arrives.
_Avoid_: Region, probe (that's the place the checks run from)

**Location rule**:
How many of a monitor's locations must be Down for the monitor to be Down: any of them (the default), all of them, or at least a given number. Locations whose probe is offline or that haven't reported don't count, neither as Down nor in the number "all" is measured against, so "all" needs at least one location that counts, and a number higher than the locations that can report never takes the monitor Down.
_Avoid_: Alert rule, quorum

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
A configured destination for notifications, such as an email address, a Slack webhook or a Telegram chat. A monitor sends to the channels attached to it. A probe sends its offline and online notifications to its own channels, or, with none, to the channels of its active monitors.
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
A message sent to a monitor's notification channels when the monitor goes Down or comes back Up. Its certificate expiry notifications, and a stale monitor's "not checked since" and "checking again" messages, go to the same channels. A probe's offline and online notifications go to the probe's own channels, or to its active monitors' channels when it has none.
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
How many days heartbeats are kept before they're deleted. Once workspaces are built, each workspace sets its own, and its plan may cap it.
_Avoid_: Keep-data period, history limit

### Workspaces

Decided in [ADR 0005](docs/adr/0005-workspaces-and-data-isolation.md) and built in Peekaping Cloud's milestone 2. Until that ships, an install has no workspaces: every signed-in user sees everything.

**Workspace**:
What owns monitors and everything around them: notification channels, status pages, maintenance windows, tags, proxies, API keys, its settings and its plan. People work in it as its members, and it sees nothing of another workspace. A user can be a member of several, and a request runs in one of them: the one it names, or else the user's default workspace, the one they joined first. Probes and install settings belong to the install, not to a workspace. See [ADR 0005](docs/adr/0005-workspaces-and-data-isolation.md).
_Avoid_: Organization, team (kept for a level inside a workspace, which Peekaping doesn't have), account (that's a user's login), tenant, project

**Member**:
A user's place in a workspace, with one role. A user in two workspaces is a member twice, with a role in each.
_Avoid_: Seat (that's billing), user (that's the account itself)

**Role**:
What a member may do in their workspace. There are three. The member role manages monitors, notification channels, status pages, maintenance windows, tags and proxies. The admin role adds the workspace's people, API keys and settings, and sees its plan. The owner role adds changing the plan, deleting the workspace and making other owners; every workspace has at least one owner. An API key acts with the admin role in its workspace.
_Avoid_: Permission, access level

**Instance admin**:
A user who manages what belongs to the install rather than to a workspace: probes, the usage ping and error reports. It's a flag on the user, not a role, and gives no access to any workspace's data. The first user to sign up gets it, and an install upgraded to workspaces gives it to its existing users.
_Avoid_: Superuser, site admin, root

**Plan**:
The limits a workspace is held to, such as how many monitors it may have and its shortest check interval. A workspace without a plan has no limits, which is every workspace on a self-hosted install.
_Avoid_: Tier, subscription (that's the billing record), package

**Signup mode**:
Who may create an account on an install, set with `SIGNUP_MODE`. In `invite`, the default, the first user signs up and everyone after joins a workspace through an invite. In `open` anyone may sign up, and each new user gets a workspace of their own, unless they sign up through an invite: then they join the inviting workspace instead. The cloud runs `open`.
_Avoid_: Registration mode, signup policy

### Deployment

**Storage backend**:
The database an install keeps its data in: SQLite or PostgreSQL. It's set with `DB_TYPE`. MongoDB storage was removed ([ADR 0001](docs/adr/0001-deprecate-mongodb-storage.md)): `DB_TYPE=mongo` and `mongodb` stop every binary, and `bun db import-mongo` moves a MongoDB install to SQLite or PostgreSQL.
_Avoid_: Database type, DB driver

**MongoDB monitor type**:
A monitor type that checks someone else's MongoDB server. It has nothing to do with the storage backend and isn't deprecated.
_Avoid_: Mongo storage, MongoDB database

**Single mode**:
Peekaping running as one process (the `peekaping` binary) with everything inside it (the api, the scheduler and its check pool, ingest and the watchdog) and no Redis. The SQLite and PostgreSQL bundle images run it. It supports those two storage backends only. See [ADR 0002](docs/adr/0002-single-process-runtime.md).
_Avoid_: Monolithic mode, standalone, all-in-one

**Cluster mode**:
Peekaping running as separate processes: api replicas that share the database and talk to each other through Redis, and probe instances that connect to the api and run the checks. The api stores the results, and the api replica that holds the leader lock runs the watchdog. Helm and the SQLite and PostgreSQL compose stacks run it. See [ADR 0002](docs/adr/0002-single-process-runtime.md).
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
