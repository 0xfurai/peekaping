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
| `CLIENT_URL` | string | Yes | `http://localhost:3000` | The address people open. Notifications and account emails link to it, and its origin is the one `CORS_ORIGINS` allows when it is unset. It can also be set in Admin → Settings as the public URL (see [Install settings](#install-settings)); the origin `CORS_ORIGINS` allows stays the environment's (or the default) |
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

### Install settings

The signup mode (`SIGNUP_MODE`), the sender of account emails (`ACCOUNT_SMTP_HOST`, `ACCOUNT_SMTP_PORT`, `ACCOUNT_SMTP_USER`, `ACCOUNT_SMTP_PASSWORD`, `ACCOUNT_SMTP_FROM`, `ACCOUNT_SMTP_TLS`) and the public URL (`CLIENT_URL`) are install settings: an instance admin can change each of them in the web app, under Admin → Settings, with no restart. Each value comes from the environment first, then from the value saved there, then from the default:

- **A variable that is set wins**, from the system environment or the `.env` file. A variable set to an empty string (`ACCOUNT_SMTP_HOST=`) counts as unset: it doesn't lock its setting. The setting shows "set by the server's environment" and can't be changed in the web app, and `PUT /api/v1/admin/settings` refuses it with 409 `SETTING_LOCKED`. A shared install that wants its settings pinned sets the variables. The sender is one setting here: when any `ACCOUNT_SMTP_*` variable is set, the environment sets the whole sender, so every field of it is locked and a field whose variable is unset takes its default, never a saved value. A password from the environment is never sent to a saved host.
- **An install that sets nothing and saves nothing** behaves as before: invite signup, no sender, and `http://localhost:3000`.
- **A saved change** takes effect at once on the server that saved it, and on the other api replicas within 5 seconds, how long each keeps the saved values.
- **The SMTP password is write-only.** `GET /api/v1/admin/settings` says only whether one is set. A change that leaves it out keeps it; an empty one clears it. The saved password is kept only while the host, port, TLS mode and user stay as they are: a change of any of them that doesn't send a password in the same request clears it, and the admin enters it again, so that it is never sent to another server.
- **Open signup needs a sender.** Saving `open` with no sender, and removing the sender while signup is open, are refused (400 `OPEN_SIGNUP_NEEDS_SENDER`, `SENDER_NEEDED_FOR_OPEN_SIGNUP`). The startup checks below still apply to the variables that are set: `SIGNUP_MODE=open` in the environment needs `ACCOUNT_SMTP_HOST` in it too.
- **Send a test email** (`POST /api/v1/admin/settings/test-email`) sends a fixed message through the sender as it is now.

The saved values are in the `settings` table, under `install.signup_mode`, `install.public_url` and `install.account_smtp.*`. The SMTP password is stored as entered, as it is in an SMTP notification channel. Every other variable (Turnstile, the JWT and token settings, the database and the rest) stays in the environment only.

### Account Emails Configuration

One sender sends the account emails of the whole install: the password reset link, the email verification link and the invite emails. It is separate from the SMTP settings of an email notification channel, which belong to the channel. Leave `ACCOUNT_SMTP_HOST` unset, and save no host in Admin → Settings, to switch account emails off: the sign-in page shows no "Forgot password" link, no one is asked to verify an address and invites are copied, not emailed. `GET /api/v1/auth/config` tells the web app which it is. They apply to the api and to `peekaping`.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `ACCOUNT_SMTP_HOST` | string | No | empty | The SMTP server. Setting it turns account emails on. Each `ACCOUNT_SMTP_*` setting can also be set in Admin → Settings (see [Install settings](#install-settings)) |
| `ACCOUNT_SMTP_PORT` | int | No | `587` | The SMTP port, from 1 to 65535. Use `465` with `ACCOUNT_SMTP_TLS=tls` |
| `ACCOUNT_SMTP_USER` | string | No | empty | The login. Empty means the server needs none |
| `ACCOUNT_SMTP_PASSWORD` | string | No | empty | The password for `ACCOUNT_SMTP_USER`. It is masked in the startup log |
| `ACCOUNT_SMTP_FROM` | string | When the host is set | empty | The address the emails come from, such as `no-reply@example.com` or `Peekaping <no-reply@example.com>`. A host without it stops startup |
| `ACCOUNT_SMTP_TLS` | string | No | `starttls` | `starttls` upgrades the connection and refuses to send when the server doesn't offer it, `tls` wraps the connection in TLS from the first byte (port 465), and `none` sends in the clear, for a relay on the same host or network. With `ACCOUNT_SMTP_USER` and `ACCOUNT_SMTP_PASSWORD` set it is meant only for a relay on loopback (`localhost`, `127.0.0.1`): Go's `net/smtp` refuses to send credentials over an unencrypted connection to any other host, so the send fails. A remote server with a login needs `starttls` or `tls` |

Each link is a single-use token with a lifetime: a password reset link expires after one hour, a verification link after three days, and an invite after seven. The server stores only a hash of a token. Using a reset link sets the new password and ends every session of the user: refresh tokens and access tokens issued before are refused. Asking for a reset link or a new verification link is limited to 5 emails an hour per address and 20 requests an hour per client address (see `TRUSTED_PROXIES`), counted in the memory of each API replica. A request for a reset link answers the same whether or not the address has an account. The emails are in English, and the links open the web app at the public URL (`CLIENT_URL`, or the one saved in Admin → Settings), so set it to the address people use.

An install that upgrades counts its existing users as verified.

### Signup Configuration

`SIGNUP_MODE` says who may create an account. They apply to the api and to `peekaping`. `SIGNUP_MODE` can also be set in Admin → Settings (see [Install settings](#install-settings)); the Turnstile keys are read from the environment only.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `SIGNUP_MODE` | string | No | `invite` | `invite`: the first user signs up and gets the first workspace, as its owner and an instance admin, and everyone after joins a workspace through an invite. `open`: anyone may sign up and gets a workspace of their own, as its owner; only the install's very first user is an instance admin. Someone who signs up through an invite joins the inviting workspace instead, in both modes. Any other value stops startup. `open` needs [account emails](#account-emails-configuration), because each new account must verify its email address: without `ACCOUNT_SMTP_HOST` startup stops and says so. Behind a reverse proxy, `open` also needs `TRUSTED_PROXIES` (see below) |
| `SIGNUP_TURNSTILE_SITE_KEY` | string | With the secret | empty | The public key of a [Cloudflare Turnstile](https://developers.cloudflare.com/turnstile/) widget. `GET /api/v1/auth/config` returns it as `turnstileSiteKey` in `open` mode, for the web app to show the widget |
| `SIGNUP_TURNSTILE_SECRET` | string | With the site key | empty | The widget's secret. With both set, `POST /api/v1/auth/register` in `open` mode needs a `turnstileToken` that the server checks with Cloudflare (5-second timeout). A missing or refused token, and a Cloudflare that cannot be reached, answer 400 and create no account. Set both or neither: one alone stops startup. The secret is masked in the startup log |

In `open` mode, the workspace of a new account is suspended until its owner has verified their email address by following the link, or by resetting the password with the emailed link. A suspended workspace keeps its monitors, and its members can create, edit and read them, but none runs: no checks, no missed-push detection, no notifications, and a probe is dealt none of them. `GET /api/v1/me` marks the workspace `suspended` with `suspendedReason` `unverified`. Its public status pages and badges answer 503 "unavailable", as for a [suspended workspace](#suspended-workspaces). A push to a suspended workspace's push URL answers 403 and is not stored. A result that was on its way when the workspace was suspended is dropped, and sends no notification. Verifying lifts the suspension, and so does making a user with a verified address an owner of the workspace (by a role change, or an invite for the owner role); the monitors start running. A workspace of an `invite` install, or of one upgraded to workspaces, is never suspended.

Signups without an invite are limited to 5 an hour per client address (see `TRUSTED_PROXIES`) in `open` mode, counted in the memory of each API replica; more answer 429. Behind a reverse proxy, set `TRUSTED_PROXIES` to the proxy's address or range (such as `127.0.0.1,10.0.0.0/8`) when you use `open` mode: without it every signup comes from the proxy's address and all visitors share one limit of 5 an hour, so the sixth signup of any visitor is refused. The server logs a warning at startup, and when an instance admin saves `open`, when signup is open and `TRUSTED_PROXIES` is empty. A signup through an invite is not limited and needs no Turnstile answer: the invite is the proof that the visitor is wanted. In `open` mode, signing up with an email that already has an account answers 409, which tells the visitor that the address is registered; the limit of 5 signups an hour per client address and the Turnstile check (when set) are what hold back someone who tries addresses one by one, and "Forgot password" stays silent about which addresses have accounts. `GET /api/v1/auth/config` also returns `signupMode` and `signupOpen`: true in `open` mode, and in `invite` mode while the install has no user.

### Suspended workspaces

An instance admin lists the install's workspaces with `GET /api/v1/admin/workspaces` and suspends or resumes one with `PATCH /api/v1/admin/workspaces/{id}` and `{"suspended": true}` or `false`. Both are for an instance admin signed in as a user: anyone else, and any API key, gets 403. The list shows counts and metadata only (name, owners' emails, members, active and paused monitors, status pages, custom domains, and whether, why and since when the workspace is suspended), never a monitor's target, a channel or anything else inside a workspace. Each suspend and resume is logged with who did it.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `SUPPORT_CONTACT` | string | No | empty | How a member of a suspended workspace gets in touch with whoever runs the install: free text, an email address or a URL. `GET /api/v1/me` returns it as `supportContact` (`null` when unset), for the banner the web app shows |

It applies to the api and to `peekaping`.

- While a workspace is suspended by an admin its monitors are kept and listed, but none runs: no checks, no missed-push detection, no notifications, and a push answers 403 (naming the install's administrator) and is not stored. `GET /api/v1/me` marks the workspace `suspended` with `suspendedReason` `admin`. Nothing is deleted.
- Its public status pages (by slug and by custom domain, with their monitors) and the badges of its monitors answer 503 with `{"error":{"code":"STATUS_PAGE_UNAVAILABLE"}}` and the message "This status page is unavailable", and say nothing of why. A page that is not published is still 404. A new account's workspace answers the same while it waits for its owner's email verification.
- Resuming lifts only the admin's hold: the monitors run again in the state they had, and a workspace that still waits for its owner's email verification keeps waiting (`suspendedReason` `unverified`).

### Deleting a workspace

An instance admin deletes a workspace with `DELETE /api/v1/admin/workspaces/{id}`, which answers 204 (404 for a workspace that does not exist; anyone else, and any API key, gets 403). It is for good: the workspace's monitors with their heartbeats and stats, notification channels, proxies, tags, maintenance windows, status pages and custom domains, API keys, invites and settings all go with it, by cascade. Its monitors stop at once, its public status pages and badges are gone (404), and its members' open sockets leave its rooms. Its members keep their accounts and their other workspaces; a member who had only this workspace signs in to an empty account (`GET /api/v1/me` answers an empty `workspaces` list). An admin may delete a workspace they are a member of. The delete is logged with who did it.

### Monitor Types Configuration

`DISABLED_MONITOR_TYPES` turns monitor types off for the whole install. It applies to the api and to `peekaping`. Use it where a type would reach something it should not: the Docker monitor reads the Docker socket of the machine the server runs on, which on a shared install is the host's own containers.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `DISABLED_MONITOR_TYPES` | string | No | empty | Comma-separated monitor types to turn off, such as `docker` or `docker,snmp`. Spaces are trimmed and case is ignored. The types are the ones in the monitor type list: `dns`, `docker`, `http`, `http-json-query`, `http-keyword`, `grpc-keyword`, `kafka-producer`, `mongodb`, `mqtt`, `mysql`, `ping`, `postgres`, `push`, `rabbitmq`, `redis`, `smtp`, `snmp`, `sqlserver` and `tcp`. A type the server does not know stops startup and lists the known ones. Empty (the default) turns nothing off |

With a type turned off:

- `POST /api/v1/monitors` for that type answers 400 with the code `MONITOR_TYPE_DISABLED` and a message that names the type, and so does a `PUT` or `PATCH` that changes a monitor's type to it. Editing a monitor that already has the type, without changing it, still works, and so does moving it to another type.
- Monitors that already have the type are kept and listed with `type_disabled: true`, but none runs: no checks by the scheduler or by remote probes, no missed-push detection, no notifications, and a push to one of them answers 403. Nothing is changed in the monitor itself (`active`, its probes, its history), so removing the type from the list makes it run again. A result that was on its way when the type was turned off is dropped.
- `GET /api/v1/me` returns `disabledMonitorTypes`, and the web app leaves those types out of the monitor type list and shows a monitor of one as paused, with the reason.

### Blocking private targets

`BLOCK_PRIVATE_TARGETS` keeps monitors from reaching the internal network and the cloud's metadata service. It applies to the api, to `peekaping` and to `peekaping-probe`. A self-hosted install usually monitors its own network and leaves it off; turn it on where the people who create monitors should not be able to point the server at the machine it runs on.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `BLOCK_PRIVATE_TARGETS` | bool | No | `false` | `true` blocks the addresses listed below. Anything else leaves checks unrestricted |

With it on, these addresses are blocked: loopback (`127.0.0.0/8`, `::1`), the private ranges `10.0.0.0/8`, `172.16.0.0/12` and `192.168.0.0/16`, carrier-grade NAT (`100.64.0.0/10`), `0.0.0.0/8` and the unspecified address, link-local addresses (`169.254.0.0/16`, which holds the metadata service at `169.254.169.254`, and `fe80::/10`), IPv6 unique-local addresses (`fc00::/7`), multicast, and the broadcast address. An IPv4-mapped IPv6 address counts as its IPv4 address. Unix sockets are blocked too.

- The address a check connects to is what is checked, after the name is resolved. A hostname that resolves only to blocked addresses, an HTTP redirect to one, and a DNS answer that changes between lookups are all refused, and the check is Down with "target address is not allowed". A hostname with both blocked and allowed answers is not refused as a whole: the check refuses each blocked address and may connect to an allowed one, and ping drops the blocked answers and fails only when none is left. Such a check can then be Down with a connection error, from an allowed address that didn't answer, rather than "target address is not allowed". The target and redirect checks apply to the connections a check makes itself: behind a proxy, the check connects only to the proxy, so the proxy's address is what is checked, and the proxy dials the target and any redirect.
- This covers every monitor type that connects out: HTTP, keyword and JSON query (including the OAuth token request, mTLS, NTLM and the proxy), TCP, ping, DNS, SMTP, SNMP, PostgreSQL, MySQL, SQL Server, MongoDB, Redis, MQTT, RabbitMQ, Kafka, gRPC and Docker. For an HTTP monitor behind a proxy, the address checked is the proxy's: the proxy connects to the target. A Kafka monitor's brokers are checked too when the cluster announces them, an MQTT monitor ignores the `all_proxy` of the environment, a gRPC target that names a DNS server (`dns://server/host:port`) is refused, and a Docker monitor over a Unix socket (the `socket` connection type) is Down.
- Saving a monitor whose target is an IP address that is blocked answers 400 with the code `TARGET_NOT_ALLOWED`, on create, `PUT` and `PATCH`. So does saving a proxy whose host is one. A hostname is saved, since what it resolves to can change and is checked at each check. A monitor or proxy stored before the setting was turned on is kept, and its checks are refused.
- Notification channels connect to addresses users type, so they are covered too: the webhook, Slack, Discord, Teams, Google Chat, Mattermost, WeCom, ntfy, Gotify, Matrix, Signal, Telegram, WhatsApp, PagerDuty, PagerTree and Grafana OnCall channels send through the same check, and so does the SMTP channel's server. A webhook, a redirect from one, or a host name that resolves to a blocked address sends nothing. Saving or testing a channel whose URL or SMTP host is a blocked IP address answers 400 with the code `TARGET_NOT_ALLOWED`; a name is saved, and a test that dials one that resolves to a blocked address answers 400 with "target address is not allowed". The account emails (`ACCOUNT_SMTP_*`) and the install email channel, which sends through them, are the install's own setting and are not checked. A proxy set in the server's environment (`HTTPS_PROXY`, `HTTP_PROXY`) is the install's too, so the channel connects to it even at a private address. What the proxy would dial is checked before the request goes to it instead: a URL or a redirect whose host is a blocked address, or a name that resolves to one, sends nothing, and the proxy never sees it. The proxy resolves the name again when it dials, though, so split DNS (the proxy's resolver answering differently from the server's) or DNS rebinding can still reach a private address through it. An install that turns `BLOCK_PRIVATE_TARGETS` on and sends notifications through a proxy must enforce the destination rules at the proxy, or leave `HTTPS_PROXY` (and `HTTP_PROXY`) unset for the server.
- A remote probe blocks private targets when its own `BLOCK_PRIVATE_TARGETS` is on, or when the server it connects to has it on: the server states its setting when the probe says hello, and the probe follows it at each hello. A server from before the setting leaves it out, which means off. A probe from before the setting ignores it, so its hello doesn't say that it blocks private targets (`enforces_private_target_block`): a server with `BLOCK_PRIVATE_TARGETS` on deals such an instance no monitors, logs a warning once for each, and lists it in the probe API with `enforces_private_target_block: false`. Upgrade the probes (in cluster mode, the local probe's `peekaping-probe` instances too) before turning the setting on, or they get no work.

### API rate limits

`API_RATE_LIMIT_PER_WORKSPACE` and `API_RATE_LIMIT_PER_API_KEY` cap how many requests a workspace, and one of its API keys, may make. They apply to the api and to `peekaping`. Both are off by default, so a self-hosted install is unchanged; turn them on where a workspace's scripts should not be able to take the server from the others.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `API_RATE_LIMIT_PER_WORKSPACE` | int | No | `0` | Requests per minute one workspace may make, all of its users and API keys together. `0` is off |
| `API_RATE_LIMIT_PER_API_KEY` | int | No | `0` | Requests per minute one API key may make. `0` is off. Every request of a key also counts toward its workspace's limit |

A negative value stops startup with a configuration error.

- A request over either limit answers 429 with the body `{"message": "Too many requests, try again later", "data": null}` and a `Retry-After` header: the seconds until the oldest request leaves the minute, rounded up, and at least 1. The same header comes with the 429 of an invite, a password reset or verification email, and an open signup.
- The count is a sliding minute: the limit is how many requests were made in the last 60 seconds. A request that is refused is not counted, so a client that keeps trying gets in as soon as the wait is over. A request that its API key's limit refuses is not counted for the workspace, so one key that keeps trying uses up none of the requests the workspace's other keys and users need. A request that the workspace's limit refuses has already counted toward its key's.
- Only a request that runs in a workspace is counted, once it is signed in or its API key is accepted. The routes that run in no workspace are not: `GET /api/v1/me`, `/api/v1/auth/*`, `/api/v1/admin/*`, `POST /api/v1/workspaces`, and viewing or accepting an invite. A request with no or bad credentials is not counted either; the login lockout and the limits on the account routes guard those.
- Each api replica keeps its own count, in memory, and forgets it when it restarts. An install with several replicas therefore allows each replica's share: with 3 replicas and a limit of 600, a workspace may make up to 1,800 requests a minute if the load balancer spreads them evenly. The limit guards against a script that runs away, and is not an exact quota.

### Spacing checks to one host

`CHECKS_PER_HOST_PER_MINUTE` caps how many checks to one target host run per minute, across all monitors and workspaces. It applies to the api, to `peekaping` and to `peekaping-probe`. It is off by default, so a self-hosted install is unchanged; turn it on where many workspaces may point monitors at the same host, so that together they can't hammer it.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `CHECKS_PER_HOST_PER_MINUTE` | int | No | `0` | The most checks to one target host that one checking process starts per minute. `0` is off |

A negative value stops startup with a configuration error.

- The host is the one the check dials: the host of an HTTP URL, a TCP, ping, database or broker host, a DNS monitor's resolver. Host names are compared in lower case. A monitor that dials several hosts takes a turn on each and waits for the latest. A push monitor dials nothing and is never limited, and neither is a monitor under maintenance.
- Checks to one host are spaced evenly: at 60 a minute, they start a second apart. A check whose turn is at most a few seconds away (a quarter of its check interval, at most 5 seconds) waits for it, before its own timeout starts. A check whose turn is further away skips that tick: it makes no heartbeat, no Down and no notification, takes no check slot, and logs at debug level with the host. The monitor is checked again on its next tick. The first skip for a host, and then at most one a minute, is logged at info with how many checks were skipped.
- The cap is per checking process: the scheduler of `peekaping`, the push checks of the api, and each `peekaping-probe` instance count for themselves. With 3 probe instances and a cap of 60, a host may see up to 180 checks a minute.
- A remote probe uses the stricter of its own `CHECKS_PER_HOST_PER_MINUTE` and the server's (the smaller one that is not 0, or whichever is set). The server states its value when the probe says hello, and the probe follows it at each hello. A server or probe from before the setting left it out, which means off.
- If more monitors want a host than the cap allows, some skip every tick: the cap and the number of monitors on one host have to fit the check intervals. Raise the cap, or give those monitors longer intervals.

### Minimum check interval

`MIN_CHECK_INTERVAL` is the smallest check interval and retry interval a monitor can be saved with. It applies to the api and to `peekaping`. The default is the smallest a monitor can have, so a self-hosted install is unchanged; raise it where many workspaces share the install and short intervals would cost too much.

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `MIN_CHECK_INTERVAL` | int | No | `20` | The smallest `interval` and `retry_interval`, in seconds |

A value below 20 stops startup with a configuration error: the timeout minimum (16 seconds) and the rule that a timeout is at most 0.8 times the interval rely on it.

- Creating a monitor, or updating one with `PUT` or `PATCH`, with an `interval` or `retry_interval` below the minimum answers 400 with the code `INTERVAL_TOO_SHORT`, the `field` (`interval` or `retry_interval`) and the `minimum`, and a message that names the minimum in seconds. A `PATCH` that sends neither field is not refused because of a lower stored value, so a monitor stored with less can still be renamed or paused.
- A monitor stored with less, because it was saved before the minimum was raised or imported, is checked at the minimum: the scheduler of `peekaping`, the stale-monitor watchdog and the assignments a remote probe is dealt use the minimum in its place. The API still shows the stored values, and the timeout is kept as stored; a longer interval only loosens the 0.8 rule.
- `GET /api/v1/me` returns the minimum as `minCheckInterval`. The monitor form uses it as the minimum and the default for the interval and the retry interval, and so does Get started when it creates the first monitor.

### Metrics Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `METRICS_ENABLED` | bool | No | `false` | Serve the unauthenticated Prometheus `/metrics` endpoint, see [Prometheus Metrics](#prometheus-metrics) |

### Cleanup Configuration

Every hour, at the top of the hour in UTC, the API server deletes the heartbeats older than the data retention period, the expired stats, the old notification history and the old TLS information. The data retention period is set per workspace, in Settings, by an admin or owner, and an upgrade keeps the install's value for its one workspace. The two variables below set how long the last two are kept.

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

- `/api/v1/auth` - Authentication (login, register, logout, 2FA), and with [account emails](#account-emails-configuration) the password reset (`forgot-password`, `reset-password`) and email verification (`verify-email`, `verify-email/resend`) routes. `GET /api/v1/auth/config` is public and says whether account emails are on
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

In single mode, the `peekaping` binary has one port. With `WEB_DIR` set and `METRICS_ENABLED=true`, it serves `/metrics` there beside the web app, so whoever can open the web app can read it. The single-mode image (`0xfurai/peekaping`) runs single mode, so with `METRICS_ENABLED=true` it serves `/metrics` on port 8383.
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

