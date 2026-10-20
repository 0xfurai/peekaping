# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- docs: pages for [Deployment modes](apps/docs/docs/self-hosting/deployment-modes.md), [Upgrading to single mode](apps/docs/docs/self-hosting/upgrading-to-single-mode.md) and [Kubernetes (Helm)](apps/docs/docs/self-hosting/kubernetes-helm.md). They say what single mode and cluster mode run, how to take a 0.0.44 to 0.0.47 bundle to the single-mode bundle and back, and how to install the chart from a repository checkout. The Docker + SQLite and Docker + PostgreSQL pages link to the upgrade page instead of each keeping its own copy of the upgrade notes
- feat: each release publishes the Helm chart at `oci://ghcr.io/0xfurai/charts/peekaping`, with the chart's `version` and `appVersion` set to the release, and every image tag defaults to the `appVersion`. So a chart version installs one consistent release, and you need no checkout and no `helm dependency build`: `helm install peekaping oci://ghcr.io/0xfurai/charts/peekaping --version <X.Y.Z>`. A component's `image.tag` still overrides the default. See [Kubernetes (Helm)](apps/docs/docs/self-hosting/kubernetes-helm.md) (#221)
  - Upgrade note: an install from a checkout still runs `latest`, and an `image.tag` you set still wins. A values file that pins all six tags can drop them when you install the published chart, because the chart version pins them. A tag left in stays on its release when you upgrade.

### Changed

- feat: the SQLite and PostgreSQL bundle images run Peekaping in single mode. One process, `peekaping`, serves the web app, the API and the websocket on port 8383, with no Redis, Caddy or supervisord. The SQLite bundle now runs on Alpine. The PostgreSQL bundle stays on Debian and runs its bundled PostgreSQL 17 beside `peekaping`, so existing databases keep working as they are. The image names, port 8383, the data volumes and your environment variables stay the same. `peekaping` runs as the non-root `peekaping` user, and `PUID`, `PGID`, the Docker socket's group and the `ping_group_range` warning work as before. The images can also start with `docker run --user`, and they have a Docker health check. The MongoDB bundle doesn't change. The SQLite bundle, measured against 0.0.47:

  | | 0.0.47 | Now |
  | --- | --- | --- |
  | Image size | 417.5 MB | 78.6 MB |
  | RAM after 2 minutes idle | 92.7 MiB | 19.2 MiB |
  | RAM with 100 HTTP monitors checked every 20 seconds, after 5 minutes | 131.2 MiB | 36.6 MiB |

  The PostgreSQL bundle image goes from 763.6 MB to 493.3 MB. See [Upgrading to single mode](apps/docs/docs/self-hosting/upgrading-to-single-mode.md), [Docker + SQLite](apps/docs/docs/self-hosting/docker-with-sqlite.md), [Docker + PostgreSQL](apps/docs/docs/self-hosting/docker-with-postgres.md) and [Bundle user and permissions](apps/docs/docs/self-hosting/bundle-user.md) (#163, #209, #376) (thanks @tafaust)
  - Upgrade note: pull the new tag and start it with the same volume. Migrations run at start, as before. Redis and its data are gone; Redis held only the schedule and queued checks, which `peekaping` rebuilds from the database at start.
  - Upgrade note: a custom Caddyfile mounted at `/etc/caddy/Caddyfile` no longer works, because the images have no Caddy. Put your own reverse proxy (Caddy, nginx, Traefik) in front of port 8383 instead. The login lockout reads the client's address from `X-Forwarded-For`, which the Caddy inside the image used to set. Have your proxy set that header to the client's address, replacing what the client sent (nginx: `proxy_set_header X-Forwarded-For $remote_addr;`), and don't let clients reach port 8383 past the proxy.
  - Upgrade note: port 8383 serves everything, and the images ignore `SERVER_PORT`. A `.env` that sets it, such as `SERVER_PORT=8034` from the examples, keeps working. To use another host port, change the published port, for example `-p 8080:8383`.
  - Upgrade note: `MODE` from your environment now applies, with `prod` as the default. Before, the bundles always ran with `prod`. With `MODE=dev`, `peekaping` logs in the development format and logs every request.
  - Upgrade note: `API_URL` from your environment now reaches the web app, in the `/env.js` that `peekaping` serves. Before, the bundles always gave the web app an empty `API_URL`, which means the API on the web app's own origin. If your environment or `.env` sets `API_URL`, unset it, unless the web app must call an API on another origin.
  - Upgrade note: `peekaping` takes up to 30 seconds to shut down cleanly, and the PostgreSQL bundle stops PostgreSQL after it. `docker stop` waits only 10 seconds by default, so use `docker stop -t 35`, or `stop_grace_period: 35s` in Docker Compose.
  - Upgrade note: with `METRICS_ENABLED=true`, port 8383 now serves `/metrics`, so anyone who can open the web app can read it. Before, Caddy didn't forward it.
  - Upgrade note: logs go to the container's output (`docker logs`). Nothing writes to `/var/log/supervisor` now, so a volume mounted there can go.
- chore: for development, `docker-compose.deps.yml` with `--profile postgres` or `--profile mongo` replaces `docker-compose.sqlite.yml`, `docker-compose.postgres.yml` and `docker-compose.mongo.yml`. `make docker-sqlite`, `make docker-postgres` and `make docker-mongo` keep working
- chore: the Helm chart's bundled PostgreSQL and Redis (off by default) run pinned images, PostgreSQL 18.6 and Redis 8.10.2, instead of Bitnami's floating `latest`, so a rescheduled pod can't start a new major version on old data. The Bitnami subcharts move to `postgresql` 18.12.14 and `redis` 28.3.1, and `scripts/update-chart-deps.sh` checks and updates both. The migrate Job now tries up to 6 times (`migrate.backoffLimit`), enough to outlast a restart of the bundled PostgreSQL during an upgrade
  - Upgrade note: with the bundled databases on, the upgrade restarts PostgreSQL and Redis on the pinned images. The data stays, since the major versions stay 18 and 8.

### Deprecated

- chore: MongoDB as a storage backend (the database Peekaping keeps its data in) is deprecated: `DB_TYPE=mongo`, the `peekaping-bundle-mongo` image and the Mongo Docker Compose setup. Existing Mongo installs keep working, and the API, producer and ingester log a warning at startup. Until removal, Mongo gets no new features and only fixes for data loss, startup failures and security problems. A command to move your data to SQLite or PostgreSQL will ship before removal, and Mongo is removed no earlier than the second release after the one that ships it. New installs should use SQLite or PostgreSQL. The MongoDB monitor type, which checks other MongoDB servers, is not affected. See [Docker + MongoDB](apps/docs/docs/self-hosting/docker-with-mongo.md) (PP-23)

### Fixed

- fix: the Helm chart's web pods copy the web app from the web image you set. Before, an init container always used `0xfurai/peekaping-web:latest`, so pinning `web.image.tag` still served the latest web app. The chart's install notes now say it runs cluster mode and link to the Kubernetes (Helm) docs page
- fix: on a fresh Helm install, the api, producer, worker and ingester pods no longer crash-loop until the migrate Job has created the database tables. They wait in an init container, `wait-for-migrations`, until the Job has applied every migration. On an upgrade, the new pods wait for the new release's migrations while the old pods keep serving
- fix: `helm upgrade` no longer fails while the last migrate Job still exists. Each install, upgrade and rollback runs its own Job, `<release>-migrate-<revision>`, and Helm removes the previous one, so the Kubernetes (Helm) page drops the step that deleted the Job first

## [0.0.47] - 2026-10-19

### Added

- feat: the API serves Prometheus metrics at `/metrics` on its own port when `METRICS_ENABLED=true` (off by default). Each active monitor has `peekaping_monitor_status` (0 Down, 1 Up, 2 Pending, 3 Maintenance), `peekaping_monitor_last_heartbeat_timestamp_seconds`, and `peekaping_monitor_response_seconds` while it is Up, labelled with the monitor's ID, name and type; a paused or deleted monitor has none. The endpoint needs no credentials and lists monitor names, so scrape it over a private network; the gateway doesn't forward it. See [Prometheus Metrics](apps/docs/docs/architecture/api-server.md#prometheus-metrics) (#267, #277, #350) (thanks @SiteRelEnby)
- feat: SMTP monitor type. It connects over plain SMTP, STARTTLS or direct TLS, can log in with PLAIN authentication, can test whether the server is an open relay, and tracks the TLS certificate's expiry (#250) (thanks @czimnick)
- feat: Microsoft Teams notification channel. Point it at a Workflows webhook to get alerts with the monitor's status, details and links (#245) (thanks @mtb77)
- feat: a Slack notification channel can post with a bot token and a channel name instead of a webhook URL. The webhook URL is optional now: a channel needs a webhook URL or a bot token, and a bot token needs a channel (#248)
- feat: a webhook notification channel can sign its requests. With a signing secret set, each request carries `X-Webhook-Timestamp` (Unix seconds) and `X-Webhook-Signature` (`sha256=` and the hex HMAC-SHA256 of `{timestamp}.{payload}`); without one, requests go out unsigned as before. Signed requests don't follow redirects. See [Webhook](apps/docs/docs/notifications/webhook.md) for how to verify the signature (#288) (thanks @jonfriesen)
- feat: `REDIS_USERNAME` sets the Redis ACL user for the API, producer, worker and ingester, for a shared Redis 6 or later where each application has its own user. Leave it empty to keep password-only authentication (#279, #280) (thanks @daftu)
- feat: a stale-monitor watchdog. Every minute the leader producer finds active monitors that haven't been checked for longer than 3 check intervals (or interval + timeout + 60 seconds, if longer), logs a warning and queues a check. If a monitor is still stale once that check has had time to finish, its notification channels get one message, and another when it is checked again. The monitor list and page show "Not checked since <time>", and `GET /api/v1/health` reports `stale_monitors` and `watchdog_ran_at` (#321, #322)

### Changed

- feat: new installs keep heartbeats for 180 days instead of 365. An install counts as new when the API starts with no admin and no heartbeats yet. Upgrading changes nothing: an install that never saved a data retention period keeps 365 days, and a saved period stays as it is. Charts and uptime % come from stats, so only the recent heartbeats list and a monitor's event history reach back less far on new installs (#337)
- refactor: SQL databases write a stats row with one `INSERT ... ON CONFLICT` statement instead of an UPDATE followed by an INSERT. What is stored doesn't change (#260, #311) (thanks @brewle)
- feat: Grafana OnCall alerts also carry the heartbeat's `status`, `ping_ms`, `retries` and `time`, and the monitor's `monitor_id`, `monitor_type` and `tags`, so OnCall can route and group them. HTTP monitors add `monitor_url` (without credentials, query or fragment); TCP, ping, DNS and gRPC keyword monitors add `monitor_host`; other types send no target (#263) (thanks @brewle)
- feat: the bundle images run the API, producer, worker and ingester as a non-root user, `peekaping` (UID/GID 1000); `PUID` and `PGID` change the IDs. Caddy, Redis, the database and supervisord keep their users. Upgrade note: on start, `/app/data` is chowned to that user, which changes the owner of files an older image wrote as root; where root can't change owners, such as NFS with `root_squash`, startup warns and carries on. A Docker socket at `/var/run/docker.sock` keeps working, and ping monitors need `net.ipv4.ping_group_range` to include the user's groups (Docker allows it by default; startup warns where it doesn't). See [Bundle user and permissions](apps/docs/docs/self-hosting/bundle-user.md) (#163, #209, #316) (thanks @tafaust)
- refactor: the API server runs on Go's standard HTTP router instead of Gin, and its 75 operations are typed Huma operations instead of Swag annotations. Paths, response formats, authentication and validation stay the same. The API reference is now OpenAPI 3.1 instead of Swagger 2, served as an interactive page at `/docs`; `/swagger/index.html` redirects there and `/swagger/doc.json` stays as an alias (#329, #332)

### Fixed

- fix: when the web app talks to the API on another URL (`API_URL`), a page load sends each API request once, to the API. Before, the first requests (the version check and the custom-domain lookup) went to the web app itself, failed, and ran again against the API (PP-210)
- fix: a data retention period of 0, a negative number or a very large number no longer makes the hourly cleanup delete every heartbeat. The settings API returns 400 unless `KEEP_DATA_PERIOD_DAYS` is a whole number of days from 1 to 36,500 with type `int`. If an invalid value was stored before, or the setting can't be read, the cleanup logs an error and deletes no heartbeats instead of falling back to 365 days (#337)
- fix: the hourly cleanup deletes expired heartbeats 5,000 at a time on SQL databases instead of in one statement, so a large backlog, such as the first run after lowering the data retention period, no longer blocks the API. On SQLite, deleting a million heartbeats used to hold the database for about 13 seconds; now no request waits more than about half a second. A migration replaces three heartbeat indexes that no query uses with one on the heartbeat time, which the cleanup deletes through. Each heartbeat gets about 15% smaller on SQLite, and loading a monitor's event history gets faster. The migration builds the new index, about 1 second per million heartbeats; on PostgreSQL new heartbeats wait while it does. On SQLite the database file stops growing but doesn't shrink on its own (#336)
- fix: a network reconnect (a laptop waking, say) no longer wipes a half-filled form. The app re-checks the custom-domain lookup in the background and keeps the current screen mounted, instead of unmounting every route for the length of the refetch (#161, #228, PP-196)
- fix: minute and hour stats have bounded retention. Every hour, minute stats older than 48 hours and hour stats older than 90 days are deleted; day stats are kept indefinitely and add about 365 rows per monitor each year. Uptime % and badges read minute stats for up to 24 hours and hour stats for up to 90 days, and charts read less. On SQL databases the rows are deleted 5,000 at a time, so the first run after upgrading clears a year of backlog without blocking the API. SQLite reuses freed pages, but its file does not shrink automatically (#315)
- docs: explain SQL stats upgrade history limits, backups and rollback, and provide a manual SQLite disk-space reclamation procedure. The migration drops the mixed legacy stats table and rebuilds history only from retained heartbeats; take a database backup before upgrading. See [Upgrading stats storage](apps/docs/docs/self-hosting/upgrading-stats.md) (PP-101)
- fix: heartbeats for the same monitor that the ingester handles at the same time no longer overwrite each other's counts in the stats behind the response time chart and uptime %, and no longer fail with a unique-key error. On MongoDB they no longer create a second stats document for the same minute, hour or day. Each heartbeat now updates its stats with 3 statements instead of 6 to 9 (#312)
- fix: the ingester logs an error with the monitor ID when it can't aggregate a heartbeat into the stats behind the response time chart and uptime %, instead of dropping the error (#260)
- fix: the API, producer, worker and ingester binaries embed the time zone database, so `TZ=Europe/Kyiv` and maintenance windows with their own time zone work in any image instead of falling back to UTC, and a time zone that fails to load logs a warning. "Same as Server Timezone" is the zone the process runs in. Upgrade note: the separate API, producer, worker and ingester images (Docker Compose, Helm) used to ignore `TZ` and run in UTC; after upgrading they run in the `TZ` you set (the example `.env` files set `America/New_York`), so "Same as Server Timezone" windows, log timestamps and times in notification messages move to that zone. Set `TZ=UTC` to keep the old behavior. The bundle images already honored `TZ` (#261, #262) (thanks @brewle)
- fix: the monitor page header shows the target of TCP, DNS, ping, Docker, gRPC, SNMP, MQTT, Kafka and RabbitMQ monitors, such as `1.1.1.1:443` or `example.com (A @ 8.8.8.8:53)`, instead of only the monitor type. Monitors whose target holds credentials or a token (database, Redis and push monitors) still show only the type. Only `http(s)` URLs are links (#269, #270, #308) (thanks @HartD92)
- fix: a status page that isn't published returns 404 on its public slug, custom domain and monitor routes, instead of serving its title and monitors. The View button on a draft status page therefore shows a 404 until the page is published (#305)
- fix: badges are served only for monitors on a published status page. A monitor that is only on a draft page gets no badge, and a failed status page lookup returns 500 instead of serving the badge (#305)
- fix: updating, pausing, resuming or deleting a monitor, maintenance window, notification channel, proxy, tag, status page or API key that doesn't exist returns 404 instead of reporting success, and so do the heartbeat, stats and TLS routes of a monitor that doesn't exist (#305)
- fix: the certificate expiry badge and the monitor TLS endpoint no longer fail with 500 on SQL databases for a monitor without certificate info, and the response time badge shows "N/A" instead of "N/Ams" before the first heartbeat (#305)
- fix: invalid updates return 400: a proxy's port or protocol in a partial update, a monitor's config in a partial update (checked against its type), and a maintenance window's partial update. A maintenance window's start and end time must be a valid HH:MM time; they weren't checked before (#305)
- fix: the settings API serves only `KEEP_DATA_PERIOD_DAYS` and returns 404 for every other key, so callers can no longer read, change or delete the JWT signing secrets. Upgrading generates new signing secrets, which signs everyone out once. With more than one API replica, old and new pods reject each other's tokens until the rolling update finishes, so users can be signed out again during it; to avoid that, run `kubectl scale deployment <release>-api --replicas=0` before `helm upgrade`, which scales it back up (#302)
- fix: reject page sizes above 1,000 on paginated list endpoints (#295) (thanks @0xfurai)
- fix: monitors no longer stop checking after a check times out; existing stuck monitors recover on upgrade (#237, #254, #265, #266, #294)
- fix: editing, pausing or deleting a monitor drops its queued check and cancels a running one, so no result of the old config is recorded and the next check uses the current config, right away after an edit (#266)
- fix: close idle HTTP connections after each check, so HTTP monitors no longer leak one socket per check (#287) (thanks @jonfriesen)
- fix: ping monitors report an unreachable host as DOWN instead of "ping: executable file not found", no longer pick up other monitors' replies, work without root where unprivileged ICMP is allowed, and support IPv6 targets (#226, #297)
- fix: editing a monitor, or creating one, no longer overwrites its stored status. The monitor list's status filter and the status badge read that stored status, so an edited Down monitor was listed under Up and its badge said Up. A new monitor starts as Pending, and on start the API repairs monitors whose stored status differs from their latest heartbeat's (#285, #352, #353)
- fix: a push monitor's heartbeat is recorded as Maintenance, with the maintenance message, while the monitor is in a maintenance window, instead of the status the push reported. If the maintenance check fails, the push's own status is used (#349) (thanks @dajiaohuang)
- fix: a push URL with `status=down` is recorded as Down, and its `ping` as the response time. Before, `down` failed to parse and counted as Up, and `ping` was ignored (#298) (thanks @sean0407)
- fix: a gRPC monitor whose target can't be reached is Down. Before, any failure produced a made-up `SERVING` response, so it showed Up; the default method `check` also didn't match the proto's `Check`. A DNS monitor stops at its timeout instead of waiting 10 to 20 seconds on a resolver that never answers (#303)
- fix: on SQLite, a cancelled request no longer makes every API write fail with `database is locked (517)` until the API restarts. The SQLite driver is upgraded to a version that closes the cancelled query's rows (#346)
- fix: the monitor list card showed Pending and Maintenance monitors as Up, and the monitor page showed Pending as Down. Both show Up, Down, Pending or Maintenance by name (#344)
- fix: after the socket reconnects, the monitor list keeps updating live instead of freezing until the page is reloaded (#343)
- fix: a certificate expiry notice goes out once per threshold, such as 21, 14 and 7 days, instead of stopping after the first. A certificate first seen inside several thresholds gets only the nearest one, not all at once (#324)
- fix: a recurring-interval maintenance window counts calendar days in its time zone. Before, it was active on the wrong days around midnight and daylight saving changes, so alerts fired during planned maintenance and were muted outside it. A single date-range window now includes its first minute (#323)
- fix: failed logins lock an account on SQLite and MongoDB, as they do on PostgreSQL. The lock never applied there before. After `BRUTEFORCE_MAX_ATTEMPTS` failures (20 by default) within `BRUTEFORCE_WINDOW` (1 minute by default), the account is locked for `BRUTEFORCE_LOCKOUT` (1 minute by default). On every database, `BRUTEFORCE_MAX_ATTEMPTS=1` now locks after one failure instead of two (#313)
- fix: setting up 2FA a second time before verifying the first showed a QR code whose codes never verified, and a failed save of the 2FA status still answered "2FA verification successful" (#314)
- fix: the production Docker Compose files and the self-hosting guides give `web` and `gateway` `restart: unless-stopped`, so the nginx gateway no longer stays stopped after a Docker restart. Add the two lines to a compose file you copied earlier (#33, #331)
- fix: the Telegram channel's template hint names `{{ monitor.name }}` instead of a variable that doesn't exist (#253, #281) (thanks @dimonl)
- fix: uptime % on the monitor page (24h, 7d, 30d and 365d) and the 24h, 30d and 90d badges count the right window. They read day stats, which count only when the day starts inside the window, so "24h" meant since midnight UTC and 7d and 30d dropped everything before the first midnight (#320)
- fix: SQL databases keep minute, hour and day stats in their own tables (`stat_minutely`, `stat_hourly`, `stat_daily`). Before, one table mixed the three scales, so the 14:00 minute row also held the whole 14:00 hour, and maintenance checks lowered the average response time. Upgrade note: a migration creates the tables, rebuilds them from the heartbeats you still have and drops the old `stats` table; take a backup first, as the docs entry above says (#306)

## [0.0.46] - 2026-04-10

### Added

### Changed

### Fixed

## [0.0.45] - 2025-12-21

### Added

### Changed

### Fixed

## [0.0.44] - 2025-10-29

### ⚠️ Breaking Changes
- Major architectural overhaul to make the system highly available and horizontally scalable (#207) (thanks @0xfurai)
- If you are running a microservices setup with Docker Compose, make sure to update your configuration — the app has now been split into multiple microservices, also update .env file from example
- The bundled version should continue to work as before without any changes

### Added

### Changed

### Fixed

## [0.0.42] - 2025-10-14

### Added

### Changed
> [!WARNING]
>Attention: Breaking change with the API Key !
>Change header from Authorization: pk_... to X-API-Key: pk_...

- feat!: refactor swagger API authentication, using X-API-Key header instead of Authorization for api key auth (#210) (Thanks @tafaust) 4b3adfa

- feat: automate version updates in release workflow (Thanks @0xfurai) 12cb83e

### Fixed

## [0.0.41] - 2025-10-11

### Added

- Feature: add api key auth (#204) (Thanks @tafaust) 0ad9871
- docs(README.md): add community terraform provider mention (#205) (Thanks @mail) 4c6a127

### Changed

- chore: update Makefile and add asdf configuration for Go environment (#202) (Thanks @tafaust) 1336d6c
- Update documentation headers for badges and intro pages by removing emojis for consistency (Thanks @0xfurai) e934fda

### Fixed

- fix(docs): update healthcheck command in Docker configurations to use wget with output option (#200) (Thanks @Shurco) a523953
- Fix/monitor partial update not update config (#194) (Thanks @0xfurai) a01404b
- Fix/update monitor 200 if monitor not found (#193) (Thanks @0xfurai) 3fab720
- refactor(monitor): update buildSetMapFromModel function to preserve created_at timestamp during updates (#192) (Thanks @0xfurai) a77456f
- fix(forms): preserve current name when resetting form in create mode across multiple components (#191) (Thanks @0xfurai) d0cc2ca

## [0.0.40] - 2025-09-25

### Added
- mask password field with toggle option for http monitor authentication block

### Changed

### Fixed
- update default data retention period to 365 days in ui and add settings navigation option

## [0.0.39] - 2025-09-08

### Added
- Chinese translation (thanks @MciG-ggg)
- General translations support (thanks @0xfurai)
-  LINE messaging channel for notifications (thanks @KarinaOliinyk)
- Keyword and JSON-over-HTTP support (thanks @0xfurai)

### Changed
- Domains are now unique across all status pages (thanks @sergeykobylchenko)

### Fixed
- Heartbeat retry interval labels now correctly show actual form values instead of the hardcoded 60/48 seconds (thanks @JustAnotherDevGuy)

## [0.0.38] - 2025-08-29

### Added
- feat: badges (#156) (Thanks @0xfurai)
- feat: enhance Playwright configuration and improve test coverage (Thanks @0xfurai)
- feat: pagertree (#152) (Thanks @KarinaOliinyk)
- feat: implement pushbullet notifications (#147) (Thanks @KarinaOliinyk)
- feat: add password visibility toggle to login form (#146) (Thanks @KarinaOliinyk and @AbhishekG-Codes)
- feat: improve landing page (Thanks @0xfurai)

### Changed
- refactor: remove unused proxy handling from Push Monitor (Thanks @0xfurai)
- refactor: remove unused Card components from Push Monitor (Thanks @0xfurai)

### Fixed

## [0.0.37] - 2025-08-18

### Added
- add twilio to notification chanel

### Changed

### Fixed
- add the server name in message for pushover
- fix broken FindAll with tags

- Fix custom domain issues

## [0.0.36] - 2025-07-28

### Added

- implement rendering certificate information for https monitors

### Changed

- enhance changelog generation script to extract GitHub usernames

### Fixed

## [0.0.35] - 2025-07-27

### Added

- Add ability to set custom domain for status pages
- Add ability to check certificate expiration and notify about it

### Changed

- Change api url for web client in dev mode - now it is proxied via Vite

### Fixed

- Fix push monitor url

