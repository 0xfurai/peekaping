# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

### Changed

- refactor: SQL databases write a stats row with one `INSERT ... ON CONFLICT` statement instead of an UPDATE followed by an INSERT. What is stored doesn't change (#260) (thanks @brewle)

### Fixed

- fix: heartbeats for the same monitor that the ingester handles at the same time no longer overwrite each other's counts in the stats behind the response time chart and uptime %, and no longer fail with a unique-key error on SQLite. On MongoDB they no longer create a second stats document for the same minute, hour or day. Each heartbeat now updates its stats with 3 statements instead of 6 to 9
- fix: the ingester logs an error with the monitor ID when it can't aggregate a heartbeat into the stats behind the response time chart and uptime %, instead of dropping the error (#260)
- fix: the API, producer, worker and ingester binaries embed the time zone database, so `TZ=Europe/Kyiv` and maintenance windows with their own time zone work in any image instead of falling back to UTC, and a time zone that fails to load logs a warning. "Same as Server Timezone" is the zone the process runs in. Upgrade note: the separate API, producer, worker and ingester images (Docker Compose, Helm) used to ignore `TZ` and run in UTC; after upgrading they run in the `TZ` you set (the example `.env` files set `America/New_York`), so "Same as Server Timezone" windows, log timestamps and times in notification messages move to that zone. Set `TZ=UTC` to keep the old behavior. The bundle images already honored `TZ` (#261, #262) (thanks @brewle)
- fix: the monitor page header shows the target of TCP, DNS, ping, Docker, gRPC, SNMP, MQTT, Kafka and RabbitMQ monitors, such as `1.1.1.1:443` or `example.com (A @ 8.8.8.8:53)`, instead of only the monitor type. Monitors whose target holds credentials or a token (database, Redis and push monitors) still show only the type. Only `http(s)` URLs are links (#269, #270) (thanks @HartD92)
- fix: a status page that isn't published returns 404 on its public slug, custom domain and monitor routes, instead of serving its title and monitors. The View button on a draft status page therefore shows a 404 until the page is published (#305)
- fix: badges are served only for monitors on a published status page. A monitor that is only on a draft page gets no badge, and a failed status page lookup returns 500 instead of serving the badge (#305)
- fix: updating, pausing, resuming or deleting a monitor, maintenance window, notification channel, proxy, tag, status page or API key that doesn't exist returns 404 instead of reporting success, and so do the heartbeat, stats and TLS routes of a monitor that doesn't exist (#305)
- fix: the certificate expiry badge and the monitor TLS endpoint no longer fail with 500 on SQL databases for a monitor without certificate info, and the response time badge shows "N/A" instead of "N/Ams" before the first heartbeat (#305)
- fix: invalid updates return 400: a proxy's port or protocol in a partial update, a monitor's config in a partial update (checked against its type), and a maintenance window's partial update. A maintenance window's start and end time must be a valid HH:MM time; they weren't checked before (#305)
- fix: the settings API serves only `KEEP_DATA_PERIOD_DAYS` and returns 404 for every other key, so callers can no longer read, change or delete the JWT signing secrets. Upgrading generates new signing secrets, which signs everyone out once. With more than one API replica, old and new pods reject each other's tokens until the rolling update finishes, so users can be signed out again during it; to avoid that, run `kubectl scale deployment <release>-api --replicas=0` before `helm upgrade`, which scales it back up (#302)
- fix: reject page sizes above 1,000 on paginated list endpoints (#295) (thanks @0xfurai)
- fix: monitors no longer stop checking after a check times out; existing stuck monitors recover on upgrade (#237, #254, #265, #266)
- fix: editing, pausing or deleting a monitor drops its queued check and cancels a running one, so no result of the old config is recorded and the next check uses the current config, right away after an edit (#266)
- fix: close idle HTTP connections after each check, so HTTP monitors no longer leak one socket per check (#287) (thanks @jonfriesen)
- fix: ping monitors report an unreachable host as DOWN instead of "ping: executable file not found", no longer pick up other monitors' replies, work without root where unprivileged ICMP is allowed, and support IPv6 targets (#226)

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

