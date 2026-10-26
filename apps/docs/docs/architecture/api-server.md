---
sidebar_position: 2
---

# API Server

The API Server is the main entry point for all client interactions with Peekaping. It provides a RESTful API, WebSocket connections for real-time updates, and serves as the authentication gateway.

## Role & Responsibilities

The API Server handles:

- **HTTP API**: RESTful endpoints for all CRUD operations
- **Authentication**: User login, registration, JWT token management, and 2FA
- **Authorization**: Role-based access control and API key validation
- **WebSocket Server**: Real-time bidirectional communication with web clients
- **API Documentation**: Auto-generated OpenAPI documentation
- **Security**: Brute force protection, rate limiting, CORS handling
- **Push Endpoint**: Receives heartbeats from push-based monitors
- **Probe Protocol**: Deals each probe's monitors among its instances and takes their results (`/probe/v1/`). See [Remote probes](../self-hosting/remote-probes.md)
- **Ingest**: Stores the results that probes and push monitors send, as heartbeats (cluster mode on SQLite and PostgreSQL, and single mode)
- **Leader Jobs**: The stale-monitor watchdog, the probe offline sweep and the push monitors' checks, on the replica that holds the leader lock (cluster mode on SQLite and PostgreSQL)
- **Cleanup Tasks**: Scheduled cleanup of old data
- **Prometheus Metrics**: Exposes per-monitor metrics for scraping

## Architecture

### Dependency Injection
The API server uses Uber's Dig for dependency injection, registering all modules and their dependencies at startup.

### Event-Driven Communication
The API server subscribes to Redis pub/sub events and broadcasts them to connected WebSocket clients for real-time updates.

### Notifications
Redis pub/sub gives every API server a copy of an event, so notifications don't travel that way, or each replica would send them all. The API server that stores a heartbeat puts a monitor going Down or Up, or a certificate close to expiring, on the `notification` queue. The leader's watchdog does the same for a stale monitor and for the same monitor checked again. Each API server takes tasks from that queue, and a task goes to one of them, so a notification is sent once to each of the monitor's notification channels however many replicas run. A notification queued while no API server is running is sent when the first one starts.

In single mode the `peekaping` binary is the only process and sends them itself, with no queue.

### Ingest
In cluster mode on SQLite and PostgreSQL, the API server stores what it receives itself: a push to a push monitor's URL, and the results a probe sends. Each replica stores a monitor's results one at a time, in the order it got them, so a push and a check of the same monitor that reach one replica never overtake each other. The replicas don't order results among themselves. A probe instance sends its batches in order and to one replica at a time, and the database's lock serializes the monitor's location updates, so two replicas never change the same monitor's locations at once. The replica that took the request judges the monitor's status, publishes the heartbeat and counts it in the stats. Nothing goes through a queue.

- **Stats** are aggregated by the replica that stored the heartbeat. The heartbeat reaches the other replicas' websockets and metrics through Redis pub/sub, but only the replica that stored it counts it in the stats.
- **Retried batches.** A probe that doesn't get an answer sends its batch again, possibly to another replica. Each replica records the ids of the results it took in a Redis key that lasts an hour, so a batch retried within the hour is answered as a duplicate and stored once. A result's id is claimed for 2 minutes before it is stored and kept for the hour once it is. A retry that finds an id that is claimed and not kept (the first replica may still be storing it, or may have died) is not answered as a duplicate, which the probe drops for good: the replica stores the rest of the batch and answers 503 with a `Retry-After` of 5 seconds, and the probe sends the batch again. A request stores for 90 seconds at the most, so that it is over before its claims run out: past that it keeps the ids of what it stored, gives back the rest and answers the same 503. If a replica can't reach Redis for that record, it answers 503, and the probe sends the batch again.
- **Speed.** One API server stores about 2,000 probe results a second on PostgreSQL 17 and on SQLite. The ingester queue it replaced stored about 1,350 a second on PostgreSQL.


### Leader Jobs
Three jobs must run on one replica at a time. In cluster mode on SQLite and PostgreSQL they run on the API replica that holds the leader lock, the Redis key `peekaping:producer:leader`. The lock lasts 10 seconds and its holder renews it every 5.

| Job | What it does |
| --- | --- |
| Stale-monitor watchdog | Every minute, finds active monitors that stopped being checked, records them for the health endpoint and notifies their channels. It skips a monitor whose probe is offline. It can't make a probe check again, so it notifies once the wait has passed. |
| Probe offline sweep | Every 30 seconds, marks a probe offline when none of its instances has been heard from for `PROBE_OFFLINE_AFTER`, and online again when one is. It tells the probe's notification channels, or its active monitors' channels when it has none. See [When a probe is offline](../self-hosting/remote-probes.md#when-a-probe-is-offline). |
| Push monitors' checks | Finds a push monitor that got no push within its interval and stores a Down heartbeat. |

The sweep also looks for a local probe that has active monitors and no instance, which is what a cluster upgraded without deploying probes looks like. It reports that probe offline once, after `PROBE_OFFLINE_AFTER` from the moment the replica took the lock, and logs `the local probe has N active monitors and no instance: deploy peekaping-probe with PROBE_TOKEN set to PROBE_BOOTSTRAP_TOKEN`. The API also logs it at startup. See [Upgrading to probes](../self-hosting/upgrading-to-probes.md).

When the leader stops, it releases the lock and another replica takes it at once, and starts the jobs. When the leader is killed, the lock runs out and another replica takes it within about 15 seconds. Each replica's log says when it starts or stops running the jobs.

In single mode the `peekaping` process is always the leader, and runs the jobs itself.

## Environment Variables

### Server Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `SERVER_PORT` | string | Yes | `8034` | Port the API server listens on |
| `CLIENT_URL` | string | Yes | `http://localhost:3000` | The address people open. Notifications link to it, and its origin is the one `CORS_ORIGINS` allows when it is unset |
| `MODE` | string | Yes | `dev` | Runtime mode: `dev`, `prod`, or `test` |
| `LOG_LEVEL` | string | No | `info` | Logging level: `debug`, `info`, `warn`, `error` |
| `TZ` | string | Yes | `UTC` | Timezone for the server |
| `SERVICE_NAME` | string | Yes | `peekaping:api` | Service identifier for logging and monitoring |

### Web App Configuration (single mode)

Only the `peekaping` binary (single mode) reads these. With `WEB_DIR` set, it serves the web app on `SERVER_PORT` beside the API, so one port serves the UI, `/api/*` and `/socket.io/*`.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `WEB_DIR` | string | No | `""` | Directory of the web app's build, with its `index.html`. Unset, `peekaping` serves the API alone. A directory without `index.html` stops it at startup |
| `API_URL` | string | No | `""` | Where the web app reaches the API, written to the `/env.js` that `peekaping` generates. Empty means the web app's own origin |

With `WEB_DIR` set, only `/api/*`, `/socket.io/*` and, when `METRICS_ENABLED` is `true`, `/metrics` reach the API. `/docs`, `/swagger/*` and `/health` get the web app instead, so check the health at `/api/v1/health`.

### Database Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `DB_TYPE` | string | Yes | - | Storage backend: `postgres`, `mysql` or `sqlite`. MongoDB storage was removed: `mongo` and `mongodb` [stop the API at startup](../self-hosting/docker-with-mongo.md) |
| `DB_HOST` | string | Conditional | - | Database host (not required for SQLite) |
| `DB_PORT` | string | Conditional | - | Database port (not required for SQLite) |
| `DB_NAME` | string | Yes | - | Database name or SQLite file path |
| `DB_USER` | string | Conditional | - | Database username (not required for SQLite) |
| `DB_PASS` | string | Conditional | - | Database password (not required for SQLite) |

### Redis Configuration

:::note Single mode
These are required in cluster mode, where the API replicas reach each other through Redis. The probes don't use Redis. In single mode, the `peekaping` binary runs the API and the checks in one process with no Redis, so it doesn't need `REDIS_HOST`. It ignores the `REDIS_*` variables and logs once when one is set.
:::

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `REDIS_HOST` | string | Yes | `redis` | Redis server hostname |
| `REDIS_PORT` | string | Yes | `6379` | Redis server port |
| `REDIS_USERNAME` | string | No | `""` | Redis ACL username (omit for password-only/default user auth) |
| `REDIS_PASSWORD` | string | No | `""` | Redis password (if authentication enabled) |
| `REDIS_DB` | int | No | `0` | Redis database number (0-15) |

### Queue Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `QUEUE_CONCURRENCY` | int | No | `128` | Maximum concurrent tasks of the `notification` queue |

### Local Probe Configuration

`PROBE_OFFLINE_AFTER` applies in both modes. `PROBE_BOOTSTRAP_TOKEN` is for cluster mode: in single mode `peekaping` doesn't store it, and logs a warning when it is set, because its own process checks the local probe's monitors and it refuses a `peekaping-probe` that uses the local probe's token.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `PROBE_BOOTSTRAP_TOKEN` | string | In cluster mode on SQLite and PostgreSQL | `""` | The token of the local probe, at least 32 characters, such as the output of `openssl rand -hex 32`. The API stores it at startup. The probe instances send the same value as `PROBE_TOKEN`. It gives access to the config of the monitors on the local probe, so keep it secret. The Compose files and the Helm chart require it and set it for the API and the probe together, but the API itself starts without it. Without it the API stores no token for the local probe, so unless an earlier start stored one, no probe instance can connect and the local probe's monitors are not checked. Remote probes use their own tokens |
| `PROBE_OFFLINE_AFTER` | duration | No | `3m` | How long an online probe's instances may all go unseen before the probe counts as offline, from `2m` to `24h`. The leader's sweep reads it. Any replica can become the leader, so set it on every replica |

### Security Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `BRUTEFORCE_MAX_ATTEMPTS` | int | No | `20` | Maximum failed logins for one client address and email before lockout |
| `BRUTEFORCE_WINDOW` | duration | No | `1m` | Time window for counting failed attempts |
| `BRUTEFORCE_LOCKOUT` | duration | No | `1m` | Lockout duration after max attempts |
| `BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS` | int | No | `100` | Failed logins for one account, from any addresses, that lock it. A successful login resets this count and the one above |
| `BRUTEFORCE_ACCOUNT_WINDOW` | duration | No | `10m` | How long the count of an account's failed logins is kept, from the first of them. Until it has passed, each further failure after a lock ends locks the account again |
| `BRUTEFORCE_ACCOUNT_LOCKOUT` | duration | No | `1m` | How long one lock lasts. After an attack stops, the owner is back in within `BRUTEFORCE_ACCOUNT_WINDOW` at most. An attacker who keeps going can keep the account locked |
| `TRUSTED_PROXIES` | string | No | empty | Comma-separated IP addresses and CIDR ranges of the proxies whose `X-Forwarded-For` and `X-Real-IP` headers are believed, such as `127.0.0.1,10.0.0.0/8`. The login lockout counts failures per client address. A request from any other address counts as coming from the address it connected from, so with the setting empty (its own default, and the single-mode bundles' setting) no header is believed. The cluster Compose files and the Helm chart set loopback and the private ranges when it is unset, and an explicitly empty value there trusts nobody. For a request from a listed proxy, the client is the last `X-Forwarded-For` address that isn't listed here (the leftmost when all are), or `X-Real-IP` when there is no `X-Forwarded-For`; an `X-Forwarded-For` it can't read makes the client the proxy's own address. An invalid entry stops startup |
| `CORS_ORIGINS` | string | No | empty | Comma-separated origins whose pages may read the API's responses, such as `https://status.example.com,http://localhost:8383`. Empty (the default) allows the origin of `CLIENT_URL`: its scheme, host and port, without a path. A default port (`:80` for `http`, `:443` for `https`) is left out, as browsers leave it out. A request from any other origin is still served, but its browser won't hand the response to the page. `*` alone allows any origin. Responses never allow credentials, since the API takes its token in the `Authorization` header. The web app calling the API on its own origin needs no entry. With `API_URL` set to another origin, list the web app's origin and those of your status page custom domains. `/metrics` is never readable from another origin. An entry that isn't an origin (`https://x.example/path`, `example.com`, or `*` next to other entries) stops startup |

### Metrics Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `METRICS_ENABLED` | bool | No | `false` | Serve the unauthenticated Prometheus `/metrics` endpoint, see [Prometheus Metrics](#prometheus-metrics) |

### Cleanup Configuration

Every hour, at the top of the hour in UTC, the API server deletes the heartbeats older than the data retention period (a setting in the web app), the expired stats, the old notification history and the old TLS information. The two variables below set how long the last two are kept.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `NOTIFICATION_HISTORY_RETENTION_DAYS` | int | No | `90` | How many days notification history is kept. At least `30`: the history stops a certificate expiry notification from being sent twice, and the default thresholds are 21, 14 and 7 days before expiry. If you set a threshold above this value, that notification can be sent again |
| `TLS_INFO_RETENTION_DAYS` | int | No | `30` | How many days monitors' TLS information is kept. At least `1` |

A value below its minimum stops startup with a configuration error. `0` counts as unset and gives the default.

In cluster mode every API replica schedules the cleanup, and the first one to claim an hour runs it, so each hour's cleanup runs once however many replicas run. Only that replica logs `Running the hourly cleanup` with the hour; the others log at debug level that another replica has it. The claim is a Redis key, `peekaping:cleanup:<hour>` (the time rounded to the nearest hour, in UTC), that expires after two hours, so replicas whose clocks are less than an hour apart still share one claim per hour. If a replica can't reach Redis when the hour starts, it logs an error and skips that hour's cleanup instead of running it unguarded. The next hour tries again.

In single mode the `peekaping` binary runs the cleanup itself every hour, with no Redis.

### Usage Statistics and Error Reporting Configuration

The anonymous usage ping and the error reports are both on until an admin turns them off, each with its own switch. These variables switch them off whatever the admin chose. See [Usage statistics and error reporting](../self-hosting/telemetry.md).

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `TELEMETRY` | string | No | `""` | `off` (also `false`, `0` or `no`) turns the usage ping off |
| `ERROR_REPORTING` | string | No | `""` | `off` (also `false`, `0` or `no`) turns error reports off. In cluster mode, set it on the API. The probe sends no error reports. |

## API Endpoints

### Core Resources

The API server exposes the following endpoint groups:

- `/api/v1/auth` - Authentication (login, register, logout, 2FA)
- `/api/v1/monitors` - Monitor management
- `/api/v1/heartbeats` - Heartbeat data retrieval
- `/api/v1/notification-channels` - Notification channel configuration
- `/api/v1/status-pages` - Status page management
- `/api/v1/proxies` - Proxy configuration
- `/api/v1/settings` - User-editable settings (only `KEEP_DATA_PERIOD_DAYS`; other keys return 404)
- `/api/v1/stats` - Statistics and analytics
- `/api/v1/api-keys` - API key management
- `/api/v1/tags` - Monitor tagging
- `/api/v1/maintenances` - Maintenance window management
- `/api/v1/health` - Health check endpoint
- `/api/v1/push/:id` - Push monitor heartbeat receiver
- `/probe/v1/*` - The probe protocol, which probes call with their token. See [Remote probes](../self-hosting/remote-probes.md)

### Pagination

The monitors, tags, proxies, notification channels, status pages, maintenances, and monitor heartbeat list endpoints accept a zero-based `page` and a `limit` from 1 to 1,000. Limits outside this range return HTTP 400. The default limit is 10 items, or 50 for monitor heartbeats.

### OpenAPI Documentation

REST controllers use typed Huma inputs and outputs, registered through the standard HTTP router. These registrations also define the OpenAPI contract. Export and regenerate the client with `bun run api:generate`; `bun run api:check` validates the spec and checks committed artifacts without databases or Redis.

The interactive reference is available at:
```
http://localhost:8034/docs
```

## WebSocket Connection

The API server provides WebSocket endpoints for real-time updates:

```
ws://localhost:8034/api/v1/ws
```


## Authentication Methods

### JWT Authentication
Header: `Authorization: Bearer <token>`

Used for web application authentication.

### API Key Authentication
Header: `X-API-Key: pk_<key>`

Used for programmatic API access.

## Health Check

The API server exposes a health check endpoint. It needs no credentials:

```http
GET /api/v1/health
```

Response:
```json
{
  "status": "success",
  "stale_monitors": 0,
  "watchdog_ran_at": "2026-10-19T12:00:42.318Z"
}
```

- `status` is always `success`. The endpoint answers `200` whenever the API server is up, so a Docker healthcheck on it only tells you that the API server is running.
- `stale_monitors` is how many active monitors Peekaping has stopped checking. It is `null` if the API server can't read the count, in cluster mode when it can't reach Redis.
- `watchdog_ran_at` is when the watchdog last ran. The watchdog looks for stale monitors every minute. It is `null` until the watchdog has run once, and also when the API server can't read it.

To be told when monitors stop being checked, alert on a `stale_monitors` above `0`, or on a `watchdog_ran_at` that is more than a few minutes old.

## Prometheus Metrics

The API server can serve the status of your monitors in the Prometheus text format, so a Prometheus and Grafana stack can scrape it without polling the REST API or wiring up notification channels. It is off by default. Set `METRICS_ENABLED=true` to turn it on:

```http
GET /metrics
```

:::warning
`/metrics` needs no credentials, and it lists the name and type of every active monitor. Scrape it over a private network, and leave `METRICS_ENABLED` off if that information shouldn't be readable by whoever can reach the API port.

In cluster mode, the gateway (nginx in front of the separate web image) doesn't forward `/metrics`, so it isn't served on the web address. Scrape the API port (`8034` by default) directly, for example `api:8034` on the Docker Compose network or `<release>-api:8034` in Kubernetes. The development Compose files publish that port; the production ones don't, so publish it or run Prometheus on the same network.

In single mode, the `peekaping` binary has one port. With `WEB_DIR` set and `METRICS_ENABLED=true`, it serves `/metrics` there beside the web app, so whoever can open the web app can read it. The SQLite and PostgreSQL bundle images run single mode, so with `METRICS_ENABLED=true` they serve `/metrics` on port 8383.
:::

### How it works

The API server keeps the latest heartbeat of each active monitor in memory, fed by the heartbeat and monitor events on the Redis event bus. It sees heartbeats whichever probe ran the check and whichever replica stored it, and a scrape reads that memory only: no database queries and no calls to a probe.

- After a restart, the metrics come back from each monitor's latest heartbeat without waiting for its next check.
- Every 5 minutes the API server re-checks the active monitors. A pause, rename or delete whose event was missed is corrected, and a monitor with no metrics gets its latest heartbeat loaded.
- A monitor that is paused or deleted has no metrics.
- A resumed monitor comes back with its next heartbeat. If the 5-minute re-check runs first, it comes back with its last heartbeat from before the pause, so its timestamp is old until the monitor is checked. An alert on that timestamp, like the second example below, can fire for a moment.
- A renamed monitor, or one whose type changed, moves to its new labels at once with the same values.
- A new monitor has no metrics until its first heartbeat. Resetting a monitor's data deletes its heartbeats but keeps its old metrics until its next check.

### Exposed metrics

Each monitor metric carries the labels `monitor_id`, `monitor_name` and `monitor_type`, and describes the monitor's latest heartbeat.

| Metric | Type | Description |
|--------|------|-------------|
| `peekaping_monitor_status` | gauge | Status: `0` Down, `1` Up, `2` Pending, `3` Maintenance |
| `peekaping_monitor_response_seconds` | gauge | Response time, in seconds. Only present while the status is Up |
| `peekaping_monitor_last_heartbeat_timestamp_seconds` | gauge | Unix time of the heartbeat. Use it to catch a stale monitor, one Peekaping has stopped checking |
| `peekaping_build_info` | gauge | Always `1`; the server version is in the `version` label |

The standard Go runtime and process metrics (`go_*`, `process_*`) are also exposed.

Pending means the latest check failed but the monitor still has retries left, and Maintenance counts as neither Up nor Down. To alert on a monitor that is down, use `peekaping_monitor_status == 0`, not "status is not `1`".

### Example Prometheus configuration

```yaml
scrape_configs:
  - job_name: peekaping
    static_configs:
      - targets: ['api:8034']
```

Example alert rules:

```yaml
groups:
  - name: peekaping
    rules:
      - alert: PeekapingMonitorDown
        expr: peekaping_monitor_status == 0
        for: 1m
      - alert: PeekapingMonitorStale
        expr: time() - peekaping_monitor_last_heartbeat_timestamp_seconds > 300
        for: 1m
```

The second rule fires for a monitor whose latest heartbeat is more than 5 minutes old. Raise `300` above the check interval of your slowest monitor.

## Related Components

- [Probe](./probe.md) - Executes health checks and sends the results
- [Migrate](./migrate.md) - Sets up the database

