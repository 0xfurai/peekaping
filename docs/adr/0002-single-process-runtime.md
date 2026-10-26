# 0002: Single-process runtime and cluster mode

Status: Accepted
Date: 2026-10-20
Ticket: PP-24

## Context

Peekaping runs as four processes that talk through Redis. The api serves the HTTP API and the websocket. The producer schedules checks. The worker runs them. The ingester stores their results as heartbeats. A bundle image (the all-in-one Docker image for one storage backend) puts the four in one container, together with Redis, Caddy (a web server that serves the web app and proxies the API) and supervisord (a process manager). The SQLite bundle runs six programs under supervisord.

Redis does four jobs between the processes:

- **Events.** Every event (monitor created, heartbeat, stale monitor and so on) crosses processes over Redis pub/sub. Delivery is fire-and-forget, handlers run concurrently, and nothing relies on order.
- **Job queue.** asynq (a Go job queue on Redis) carries exactly two hops: `monitor:healthcheck` from the producer to the worker, and `monitor:ingest` from the worker to the ingester. The push endpoint in the api also enqueues `monitor:ingest`.
- **Schedule and leadership.** The producer keeps its schedule in two Redis sorted sets and picks a leader with a Redis lock (`SET NX`, 10 second TTL). Only the leader syncs the schedule and runs the watchdog.
- **Stale state.** The watchdog writes the stale monitors to a Redis hash. The api's health endpoint reads it.

ADR 0001 decided that single-container mode covers SQLite and PostgreSQL only. The ticket asked seven questions. This ADR answers them in order.

## Decision

We add **single mode** beside the existing **cluster mode** (both are in the glossary). Both modes run the same modules. They differ only in the five seams in section 2.

### 1. Binary layout

A new `cmd/peekaping` is always single mode. It runs the api, producer, worker and ingester roles in one process. `cmd/api`, `cmd/producer`, `cmd/worker` and `cmd/ingester` stay unchanged and are the cluster-mode binaries, so the existing images, the Helm chart and the compose stacks keep working.

We rejected subcommands (`peekaping api|worker|…`). Nobody needs them while the cluster images exist, and they can be added later without breaking anything.

### 2. In-memory replacements

| Seam | Cluster mode (unchanged) | Single mode |
| --- | --- | --- |
| Event bus | `events.EventBus` over Redis pub/sub | in-memory `events.EventBus` (PP-25) |
| Check dispatch | the producer enqueues `monitor:healthcheck`, one task ID per monitor; the worker's asynq server runs it | the scheduler hands the check to an in-process pool |
| Ingest dispatch | the worker and the push endpoint enqueue `monitor:ingest`; the ingester's asynq server stores it | a direct call into the ingest logic |
| Stale state | `stale.Store` on a Redis hash, written by the watchdog, read by the api | in-memory `stale.Store` |
| Leadership | Redis `SET NX` lock | none: the one process is always the leader |

- **Event bus.** In-memory is a 1:1 replacement, because today's bus already gives no more than that. Since PP-83 the bus has typed topics (`events.Topic[T]`) and one dispatcher shared by every transport, so a handler gets the same `T` from Redis or from memory, in the same order. One difference for PP-25: handlers share one payload value instead of decoding their own copy, so they must not change it.
- **Queue.** Single mode uses no asynq. Two narrow interfaces replace the two hops: "run a check" and "ingest a result". The cluster implementations enqueue exactly as today. The single implementations call in process. Today the worker, the ingester and the push endpoint each declare their own copy of the result payload and agree only through JSON; a direct call needs one shared type. We rejected an in-memory `queue.Service`. It has 13 methods and would have to fake task ID dedupe, unique TTLs and task cancelling to serve two hops.
- **Leadership.** Single mode has no election. The one process is always the leader. Cluster mode keeps the Redis election.
- **Stale state.** The watchdog runs in the process and writes the in-memory store, and the health endpoint reads it. To get a stale monitor checked it calls `RunNow(monitorID)` on the scheduler. Today it calls the producer's `scheduleMonitor` with run-now set.
- **Websocket.** No change. The socket.io server in the api already listens on the event bus, and in single mode the bus delivers in process.

### 3. Module wiring

A shared registration (`internal/app`) builds the common modules once: config, logger, database, repositories, services, check executors and listeners. A mode-specific transport registration then adds either Redis, asynq and leader election, or the in-memory parts. Each background job runs in exactly one cluster process today (the cleanup cron and status repair in the api, stats aggregation in the ingester, and so on), so the single process starts each once.

We don't just merge the four `main` functions. The worker and the ingester each build their own `*asynq.Server` (different queues), and one container would provide it twice. Every `container.Provide` return value is checked. None of the 98 calls is today, so a failed provide is dropped and the first provider wins. Exact package and function names are for PP-27.

Shutdown follows PP-84: on SIGTERM, HTTP `Shutdown`, then the websocket, then the scheduler and pool, then the event bus, then the database, all under a deadline.

### 4. Scheduling in single mode

Detail is in PP-26. The decisions:

- One `time.AfterFunc` timer per active monitor. The monitor created, updated and deleted events reset it. Pausing is an update, so it stops the timer.
- A bounded goroutine pool runs the checks. Its size is configurable.
- At most one check per monitor at a time. If a monitor's check is still running when its timer fires, the new one is skipped, as the task ID does in cluster mode.
- A per-monitor lock ingests one monitor's heartbeats in order, checks and pushes alike. The ingest logic reads the previous heartbeat to decide Pending, Down and whether to notify, so two results ingested at once would both read the same one.
- An update, pause or delete cancels the monitor's running check and drops its result, as in cluster mode.
- No Redis means no uniqueness locks, so the stuck-monitor failure of the Redis scheduler (a task left holding a monitor's ID) can't happen in single mode.

### 5. Config compatibility

Single mode is selected by binary. There is no new environment variable, and `MODE` is untouched: it stays the dev/prod/test switch.

- `peekaping` ignores `REDIS_*` and logs once if any is set.
- `peekaping` refuses `DB_TYPE=mongo` and `DB_TYPE=mongodb` at startup, with a clear error (ADR 0001). The Mongo bundle keeps its multi-process layout.
- Every other variable keeps its name and meaning.
- The cluster binaries require Redis exactly as today. `REDIS_HOST` is `validate:"required" default:"redis"` in each `cmd/*/config.go` and in `internal/config/config.go`.

We rejected the ticket's idea that an unset `REDIS_HOST` means single mode. It can't be detected, because the default fills it in before anything can look. It would also be wrong. The compose stacks and the docs' example `.env` don't set it; they rely on the default `redis`, their Redis service's name. And a cluster install that lost the variable would silently split into independent single processes, each with its own schedule and no shared events.

### 6. Upgrade path from the 0.0.44–0.0.48 bundle

The single-mode bundle images (built in PP-28) keep the image names `peekaping-bundle-sqlite` and `peekaping-bundle-postgres`, port 8383, the data volume and the environment variables users set. Upgrading is pulling the new tag and restarting. Migrations run at start as today.

Redis and its data are dropped. No bundle documents a volume for it, and it held only transient state. The schedule and queue are rebuilt from the database at start, and a check in flight is simply run again. The watchdog's stale records are the exception: see Consequences.

`peekaping-bundle-mongo` is unchanged (ADR 0001). Helm and the compose stacks stay cluster mode. The PostgreSQL bundle still runs its bundled PostgreSQL beside `peekaping`.

One thing doesn't carry over: a custom Caddyfile mounted at `/etc/caddy/Caddyfile`, which the SQLite and PostgreSQL self-hosting pages document. There is no Caddy to read it. Those users put their own reverse proxy in front, and the release notes say so.

### 7. Web assets

The Go server serves the web build from a directory, `WEB_DIR` (`/app/web` in the bundle), only when it is set. Unset, it serves the API alone, as today. Caddy is dropped from the single-mode bundle.

Behaviours to keep from today's Caddyfile:

- **One port** (8383 in the bundle) for the UI, `/api/*` and `/socket.io/*`.
- **SPA fallback.** A path that isn't a file serves `index.html`.
- **`/env.js` is never cached** (`no-store`). Go generates it from config, with `API_URL` empty by default, meaning the API is on the same origin. The startup script stops writing it.
- **Long cache for static assets.** Files ending in `.js`, `.css`, `.mjs`, `.woff`, `.woff2`, `.svg`, `.png`, `.jpg`, `.jpeg`, `.gif` or `.ico` get `Cache-Control: public, max-age=31536000, immutable`. A missing one is a 404, not `index.html`.
- **Only `/api/*` and `/socket.io/*` reach the API.** Caddy sent every other path to the web build, so `/metrics` (unauthenticated, lists monitor names), `/docs`, `/swagger/*` and `/health` were never reachable on port 8383. On one port they would be. Where `/metrics` is served in single mode is decided in PP-27.

We rejected `go:embed`: it ties `go build` and the Go tests to a bun build of the web app first. We rejected keeping Caddy: it is a second process and a bigger image.

## Consequences

- Cluster-mode code paths stay and keep their tests. Cluster behaviour doesn't change.
- Two runtimes to maintain. Anything that changes check dispatch, ingest or the schedule has to be made on both sides of a seam. The seams are few and narrow to keep that cheap.
- Single mode doesn't scale horizontally: one process runs every check. Installs that need more run cluster mode.
- The stale records are lost on restart. A monitor already notified as stale gets no "checking again" message if it recovers after a restart, and one that is still stale is found again and notified again. We accept this: it only touches monitors that are stale across a restart.
- With Caddy gone, nothing replaces a client's `X-Forwarded-For` with its real address, as Caddy's proxy did. `httpx.ClientIP` trusts that header and the login lockout keys on it, so a client reaching port 8383 directly can dodge the lockout. PP-50 (a trusted-proxy setting, default none) closes this. It should land before the single-mode image ships.
- The self-hosting pages call the two modes "monolithic" and "microservice". They move to the glossary words with the image.

Waiting tickets:

- **PP-25** (in-memory event bus): builds the bus. Handlers don't block publishers, a panic in one doesn't stop the others, payloads are shared values. It implements the typed bus from PP-83.
- **PP-26** (scheduler and pool): the timers, the pool and its size setting, the per-monitor lock, `RunNow`, the in-memory `stale.Store`, and the single implementations of "run a check" and "ingest a result". It keeps today's timing: first check right away, later checks on the next multiple of the interval, and no immediate check after an edit for push monitors. It decides whether to keep the 30-second reconcile of the schedule against the database.
- **PP-27** (`peekaping` binary): `cmd/peekaping`, the shared registration and the cluster implementations of the two interfaces, checked `Provide` errors in every binary, the `REDIS_*` and `DB_TYPE` rules, `WEB_DIR` and `/env.js`, where `/metrics` is served, and a single-mode note in the architecture docs that call `REDIS_HOST` required.
- **PP-28** (image): keeps the names, port 8383, volume and variables, drops Redis and Caddy, and keeps the PostgreSQL bundle's PostgreSQL running. It updates the self-hosting pages and says in the release notes that a custom Caddyfile no longer works.
- **PP-84** (shutdown): the order in section 3, for every binary.
- **PP-50** (login lockout): see the `X-Forwarded-For` item above.

## Update

Date: 2026-10-20
Ticket: #29

Some statements above no longer hold for cluster mode:

- The Context says every event crosses processes over Redis pub/sub, and that asynq carries exactly two hops.
- Section 1 says `cmd/api` stays unchanged.
- Section 2's table marks cluster mode unchanged, with its event bus over Redis pub/sub.
- The Consequences say cluster behaviour doesn't change.

Redis pub/sub gives each event to every api replica, so with several replicas each one sent every notification. Cluster mode now sends the four notification events through an asynq queue named `notification` instead. They are an important heartbeat, a certificate expiry, a stale monitor and a stale monitor checking again. The ingester and the producer's watchdog enqueue them. The api takes them, so each one goes to a single replica. That replica hands it to its own event bus, and its notification listener sends the notification. Each notification is sent once to each notification channel, however many api replicas run. A notification raised while no api replica runs waits in the queue until one starts.

So `cmd/api` now runs an asynq server for the `notification` queue. Every other event stays on pub/sub, because every api replica needs it. The websocket in each replica pushes every heartbeat to its own clients.

Single mode is unchanged. It has one api and no queue, and its in-memory event bus already sends each notification once.

## Update

Date: 2026-10-25
Ticket: #254

Some statements above no longer hold for cluster mode on SQLite and PostgreSQL:

- The Context says Peekaping runs as four processes, and that the producer schedules checks, the worker runs them and the ingester stores their results.
- Section 2's table says check dispatch is the producer enqueueing `monitor:healthcheck` for the worker's asynq server, and ingest dispatch is the worker and the push endpoint enqueueing `monitor:ingest` for the ingester's asynq server. Its Leadership row says the Redis lock elects the producer that runs the watchdog.
- The Consequences say anything that changes check dispatch, ingest or the schedule has to be made on both sides of a seam, and that cluster behaviour doesn't change.
- The #29 update, which says the ingester and the producer's watchdog enqueue the notification events.

Remote probes (#31 to #36) gave every check a second route: a probe instance gets its monitors from the api, runs the checks and sends the results back over the probe protocol. On SQLite and PostgreSQL, cluster mode now uses that route for the local probe too, so the checks and the ingest no longer cross Redis:

- **Check dispatch is the probe protocol.** The local probe's monitors are checked by `peekaping-probe` instances that hold its token (`PROBE_BOOTSTRAP_TOKEN` on the api, the same value as `PROBE_TOKEN` on the probe). The api deals the monitors among the instances, as it does for any probe. Nothing schedules a check, and nothing enqueues `monitor:healthcheck`.
- **Ingest dispatch is a call in the api.** The api stores a push and a probe's results itself, one monitor's results at a time and in order, on the replica that took the request. It doesn't enqueue `monitor:ingest`. The heartbeats feed the stats on the replica that stored them, through a bus that stays in that process, so Redis pub/sub doesn't hand one heartbeat to every replica to count. The probe protocol's duplicate window moves to Redis (a key for each result, one hour), so a batch a probe retries on another replica is answered duplicate.
- **Leadership stays in the api.** The leader lock keeps its key, `peekaping:producer:leader`, so that a rolling upgrade has one leader while old producers and new api replicas run together. The api replica that holds it runs the stale-monitor watchdog, the probe offline sweep and the timeout checks of push monitors. Another replica takes over about 15 seconds after the leader is killed, and at once after it stops gracefully. The watchdog has no rechecker there: a stale monitor is recorded and its channels are notified once the wait has passed, as for a monitor on a remote probe.
- **The local probe is its instances.** In cluster mode on these backends it is online while an instance is connected, so a stopped probe deployment raises the probe offline notification. A cluster upgraded without any instance has a local probe that was never online, so the leader reports it offline once, after `PROBE_OFFLINE_AFTER`, and logs how to fix it. `PROBE_OFFLINE_AFTER` is now read by the api.

So cluster mode on SQLite and PostgreSQL is two processes plus Redis between the api replicas: the api and the probe. The producer and the ingester refuse to start there and point to the [upgrade page](../../apps/docs/docs/self-hosting/upgrading-to-probes.md), and the worker logs that it has no work. Helm and the Docker Compose stacks for those two backends no longer run them.

Two Redis seams stay in the api: the event bus and the `notification` queue from #29. The api now stores the heartbeats and runs the watchdog, so it enqueues the notifications that the ingester and the producer's watchdog enqueued before. Every other event stays on pub/sub, so each replica's websocket and metrics still see every heartbeat.

MongoDB has no probes (ADR 0001), so MongoDB installs keep the layout this ADR describes, with the producer, worker and ingester, until MongoDB storage is removed. The Mongo bundle and the Mongo compose stacks are unchanged, and the old images and binaries stay for them. Once MongoDB storage is removed, nothing uses the queue-based check and ingest dispatch any more, and it can go with the three processes.

Single mode is unchanged. Its one process is the local probe, as before.

Measured on the api alone: it stores about 2,000 probe results a second on PostgreSQL 17 and on SQLite. The ingester queue stored about 1,350 a second on PostgreSQL. A soak of 3 probe instances and 1,000 monitors at 20 seconds, with one instance stopped, missed no check and kept the api's heap at 14 to 17 MiB.

## Update

Date: 2026-10-25
Ticket: PP-277

The producer, worker and ingester are removed, as the #254 update said they would be once MongoDB storage was removed. That has happened (ADR 0001). Some statements above no longer hold:

- The Context says Peekaping runs as four processes, that the producer schedules checks, the worker runs them and the ingester stores their results, and that asynq carries exactly two hops, `monitor:healthcheck` and `monitor:ingest`.
- Section 1 says `cmd/api`, `cmd/producer`, `cmd/worker` and `cmd/ingester` stay unchanged and are the cluster-mode binaries.
- Section 2's table says cluster mode is unchanged, and gives its check dispatch, ingest dispatch and leadership as the producer, worker and ingester. The #254 update already replaced those rows for SQLite and PostgreSQL. Now no backend uses them.
- Section 5 says the Mongo bundle keeps its multi-process layout, and section 6 says `peekaping-bundle-mongo` is unchanged. The #254 update says MongoDB installs keep the producer, worker and ingester until MongoDB storage is removed, and that the Mongo bundle and compose stacks are unchanged. MongoDB storage is gone, and so are the Mongo bundle and the Mongo compose stacks. A Mongo `DB_TYPE` stops every binary.
- The #254 update says the producer and the ingester refuse to start on SQLite and PostgreSQL, and the worker logs that it has no work. They were removed in the release that was to ship that behaviour, so no published image does it. The programs are no longer in the repository.
- The Consequences say cluster behaviour doesn't change, and that anything that changes check dispatch or ingest has to be made on both sides of a seam.

Cluster mode is now the api plus probe instances, on every storage backend. SQLite and PostgreSQL are the only ones left, so there is no backend where it is anything else.

What went:

- `cmd/producer`, `cmd/worker` and `cmd/ingester`, and the producer and worker modules.
- The queue-based check and ingest dispatch: the `monitor:healthcheck` and `monitor:ingest` queues, `PRODUCER_CONCURRENCY` and the queue shutdown timeout. In cluster mode the probe protocol carries the checks and the api stores the results itself. In single mode the scheduler's pool runs the checks. The ingest logic stays as a package that the api and `peekaping` call in process.
- The Dockerfiles, the image builds and manifests in the release, release candidate and e2e workflows, the dev scripts, the `make` targets and the three architecture pages. The `peekaping-producer`, `peekaping-worker` and `peekaping-ingester` images get no new tags. The ones already published stay where they are.

What stays:

- The `notification` asynq queue from the #29 update. It is the only queue left, and the api serves it.
- The leader lock key `peekaping:producer:leader`. It keeps its name so that api replicas of an earlier release and of this one share one leader while a rolling upgrade runs.

The api used to copy the admin's error reporting choice to Redis so that the producer, worker and ingester could follow it (ADR 0003). The copy is gone with the three processes. The cluster api turns reporting on or off in its own process, as `peekaping` does. Each replica applies the stored choice again every minute, so a change made on another replica reaches it.
