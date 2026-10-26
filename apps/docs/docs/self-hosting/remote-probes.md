---
sidebar_position: 4
---

# Remote probes

By default Peekaping checks every monitor from the machine it runs on. A remote probe lets a monitor be checked from another machine too, for example from another region, or from inside a network Peekaping can't reach.

## Probes and locations

A **probe** is a place Peekaping runs checks from. Every install has the **local probe**: Peekaping itself, which checks the monitors on it. It can't be deleted. In single mode the `peekaping` process checks them, so `peekaping-probe` connects to single mode only as a remote probe, with its own token: `peekaping` refuses a probe instance that uses the local probe's token. In cluster mode on SQLite and PostgreSQL, `peekaping-probe` instances started with the local probe's token do, as [the cluster layout](../architecture/probe.md#who-runs-the-local-probes-checks) describes: the Compose files and the Helm chart deploy them for you. A **remote probe** is a small program, `peekaping-probe`, that you run on another machine. It connects out to Peekaping with its token, gets the monitors assigned to it, runs their checks and sends the results back. Peekaping records them as heartbeats, like any other result.

A monitor runs on one or more probes. Each probe's results for the monitor are a **location** of that monitor. See [several probes per monitor](#several-probes-per-monitor).

A remote probe runs the monitor types it has a check for. A push monitor runs on the local probe only, because pushes arrive at Peekaping.

## Run a probe

1. In Peekaping, open **Probes** and choose **Create probe**. Give it a name. The location text is optional and is only a label, such as `Frankfurt, Germany`.
2. Copy the token. It is shown once. [What the token can read](#what-the-token-can-read) says why to guard it.
3. Start the probe where you want the checks to run, with the Docker line that Peekaping shows next to the token:

   ```bash
   docker run -d --name peekaping-probe -e PEEKAPING_URL=https://peekaping.example.com -e PROBE_TOKEN=<token> 0xfurai/peekaping-probe:latest
   ```

   `PEEKAPING_URL` is the address you open Peekaping at, without a path. The same line works from the other machine only if that address is reachable from it. See [What the probe needs to reach](#what-the-probe-needs-to-reach).

   With Docker Compose, keep the token in a `.env` file next to the Compose file:

   ```yaml
   services:
     peekaping-probe:
       image: 0xfurai/peekaping-probe:latest
       restart: unless-stopped
       environment:
         PEEKAPING_URL: https://peekaping.example.com
         PROBE_TOKEN: ${PROBE_TOKEN}
   ```

4. Once the probe has connected, the Probes page shows it as **Online**, with the instance that connected.
5. Assign monitors to it: choose the probe in the monitor's form.

The image is `0xfurai/peekaping-probe` on Docker Hub and `ghcr.io/0xfurai/peekaping-probe` on GHCR, for amd64 and arm64. It runs as the non-root user `65532`, and keeps nothing on disk, so it needs no volume. Ping monitors need `net.ipv4.ping_group_range` to include the probe's group. Rootful Docker with a private network namespace allows that by default. Rootless Docker, `userns-remap`, `network_mode: host` and Podman don't, so set the sysctl there: see [Ping Monitors and ICMP Permissions](../architecture/probe.md#ping-monitors-and-icmp-permissions).

The image has a Docker health check on the probe's `/ready` endpoint. It answers 200 once the probe has its first assignment, and keeps answering 200 while the server is unreachable.

## The slim image

The image above runs every monitor type but push. The **slim** image is the same probe without the checks for five types whose drivers make up much of its size: **Docker**, **gRPC keyword**, **Kafka producer**, **MongoDB** and **SQL Server**. The image is about 40% smaller, so use it on a machine that doesn't need those checks.

| | Image |
| --- | --- |
| Full | `0xfurai/peekaping-probe:latest`, `0xfurai/peekaping-probe:<version>` |
| Slim | `0xfurai/peekaping-probe:slim`, `0xfurai/peekaping-probe:<version>-slim` |

Both are also on GHCR as `ghcr.io/0xfurai/peekaping-probe`, for amd64 and arm64. Start the slim image the way you start the full one, with its tag:

```bash
docker run -d --name peekaping-probe -e PEEKAPING_URL=https://peekaping.example.com -e PROBE_TOKEN=<token> 0xfurai/peekaping-probe:slim
```

The probe tells Peekaping which monitor types it runs when it connects, so Peekaping won't let you assign a Docker, gRPC keyword, Kafka producer, MongoDB or SQL Server monitor to a probe that runs the slim image. Every other type works as it does on the full image.

### Full and slim instances under one token

You can start the full and the slim image with the same token. Each instance tells Peekaping which types it runs when it connects, and Peekaping keeps the list of each instance:

- **A monitor goes only to an instance that runs its type.** A Docker monitor is dealt to a full instance and never to a slim one. Every other monitor is dealt among all the online instances, as it is when they run one image. A monitor that no online instance runs is dealt to nobody, so it records no heartbeats.
- **The probe runs a type while one of its online instances does.** The types the monitor form checks, and the types the API shows for the probe, are those of all its online instances together. With a full instance online you can assign Docker monitors to the probe. When the last full instance stops or goes quiet for a minute, Docker leaves the probe's types: its Docker monitors stay assigned and are not checked, and once they go stale their notification says `probe <name> doesn't run docker monitors`. They are checked again as soon as a full instance connects. A probe with no instance online keeps the types it last had.
- **An instance can't send results for a type it doesn't run.** Peekaping answers such a result with `not_assigned`.

The Probes API lists the types of each instance as `instances[].supported_types`, next to the probe's own `supported_types`. An empty list means every type, which is what an instance of an older probe reports. If you rely on Docker, Kafka, SQL Server, MongoDB or gRPC keyword monitors, keep a full instance online for them.

## What the probe needs to reach

The probe connects out to Peekaping over HTTP or HTTPS, to paths under `/probe/v1/` on the API. It never listens for connections from outside. Its only listener is `/ready`, on loopback by default. So the probe's machine needs no inbound port, and it works behind NAT and a firewall that allows outbound traffic.

`PEEKAPING_URL` has to reach the API's `/probe/` path. Where that path goes depends on how you installed Peekaping:

- **Docker Compose in cluster mode** (the `prod` files): the nginx gateway on port 8383 sends `/probe/` to the API.
- **The single-mode bundles** (SQLite and PostgreSQL): port 8383 serves `/probe/`, like everything else.
- **Helm**: the Ingress and the Istio VirtualService send `/probe` to the API Service. With ingress-nginx, which refuses request bodies over 1 MiB, also set the annotation `nginx.ingress.kubernetes.io/proxy-body-size: "2m"` in `ingress.annotations`, because a batch of results can be a little over 1 MiB.
- **Your own reverse proxy** in front of port 8383 (see [Reverse proxy and HTTPS](./docker-with-sqlite.md#reverse-proxy-and-https)): it has to pass `/probe/` too. If it limits the size of request bodies, allow 2 MiB. In nginx that is `client_max_body_size 2m;`.
- **The dev stack**: point `PEEKAPING_URL` at the API port. A probe run on the host uses `http://localhost:8034`. A probe in a container can't reach the host's `localhost`: join it to the stack's network and use `http://api:8034`. The network is `<project>_appnet`, where the project is the checkout's folder name unless `COMPOSE_PROJECT_NAME` sets it (`peekaping_appnet` for a checkout named `peekaping`; `docker network ls` lists it). Or use the host's address from the container (`http://host.docker.internal:8034` on Docker Desktop). Either way, set `PROBE_ALLOW_INSECURE=true` for the plain `http://` URL. The token then crosses that network unencrypted, so the dev stack's network and your machine must be trusted.

Use `https://` for any probe that isn't on the same machine as Peekaping, and for any probe that reaches Peekaping through a network you don't trust. The probe sends its token with every call, and it refuses a plain `http://` address to a host that isn't this machine, unless you set `PROBE_ALLOW_INSECURE=true`. Set it only where the network is trusted and isolated, such as the `appnet` network of the Compose stack or the cluster network of the Helm chart. Anywhere else, use an `https://` address and leave `PROBE_ALLOW_INSECURE` unset.

## Settings

The probe reads its settings from the environment:

| Variable | Default | What it does |
| --- | --- | --- |
| `PEEKAPING_URL` | | Required. The address of your Peekaping server, such as `https://peekaping.example.com`. |
| `PROBE_TOKEN` | | Required. The probe's token. |
| `PROBE_ALLOW_INSECURE` | `false` | Allows a plain `http://` address to a host other than this machine (`localhost`, `127.0.0.1` and `::1`). The token then crosses the network unencrypted, so use it only on a network you trust. |
| `PROBE_CONCURRENCY` | `128` | How many checks run at the same time. |
| `PROBE_READY_ADDR` | `127.0.0.1:8081` | Where `/ready` listens. The image's health check reads it. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |

A probe that has a wrong setting logs which variable is wrong and stops.

How long Peekaping waits before it calls a silent probe offline is a setting of the server, not of the probe. See [When a probe is offline](#when-a-probe-is-offline).

## What the token can read

:::warning The token reads your monitors' configs
The token lets whoever holds it read the configs of the monitors assigned to its probe. That includes the credentials in them, such as passwords and tokens set in a monitor's config, and the user names and passwords of proxies. Keep it like a password: don't commit it, and run a probe only on machines you trust.
:::

The token is shown once, when you create the probe or rotate its token. Peekaping keeps only a hash, so it can't show the token again.

- **Rotate** the token on the Probes page if it leaked or was lost. The old token stops working at once, and the probe's instances must restart with the new one.
- **Disable** a probe to stop its token working until you enable it again.
- **Delete** a probe to remove it. A probe that still has monitors can't be deleted: move them to another probe or delete them first. Once the probe is deleted, its token stops working.

## Several instances

Every container started with the same token is an **instance** of that probe, with an id of its own. You can run several, for example two on different hosts in one region, so a restart or a failed host doesn't stop the checks. Peekaping deals the probe's monitors among its online instances, so each monitor is checked by one instance at a time, and it deals them again when an instance joins or leaves. The Probes page lists the instances of each probe.

If an instance loses its connection, it keeps checking its last assignment until it can reach Peekaping again.

A probe has at most 64 instances. A container that starts gets a new id, and Peekaping keeps an instance that stopped for a day before it forgets it. When a new instance connects and the probe already has 64, Peekaping removes the oldest one that hasn't been in contact for a minute. If all 64 are running, the new instance is refused with `probe <name> already has 64 running instances, the most one probe may have`, and it tries again after a minute.

## Request limit

Peekaping limits how often an instance may call each route under `/probe/v1/`. The limit is 1 request a second, after a burst of 5. A request over the limit gets a 429 with a `Retry-After` header, and the probe waits and sends it again. The limit is not a setting.

Each API process counts for itself. In single mode there is one process, so the limit is exact. In cluster mode with several API replicas behind a load balancer, an instance can send up to as many times the limit as there are replicas, because each replica keeps its own count. With 3 replicas, an instance can send up to 3 requests a second. Hellos have a second limit on top, for all the instances of a probe together: 1 a second, after a burst of 10. A hello counts against both, so one instance still gets at most 5 at once. This limit is also counted by each replica for itself.

A batch that a probe sends again, for example after a timeout, can reach another replica. The replicas share a record in Redis of the results they took, kept for an hour, so the second replica answers the batch as a duplicate and each result is stored once. A result that the first replica is still storing, or died storing, is not a duplicate yet: the second replica stores the rest of the batch and answers 503 with a `Retry-After` of 5 seconds, and the probe sends the batch again, until the first replica has stored the result or its claim has run out after 2 minutes. If a replica can't reach Redis, it answers 503 and the probe sends the batch again. In single mode the record is in memory.

## When a probe is offline

A probe is online while one of its instances has been in contact with Peekaping. When the probe can't reach Peekaping, it keeps checking and holds the results in memory, up to 10,000. When that is full, the oldest are dropped first, and the probe logs it. It sends what it holds as soon as it can reach Peekaping again. Peekaping refuses results more than one hour old, so a longer gap leaves a gap in the heartbeats. Because the results are in memory, a probe that restarts while it is cut off loses them.

When Peekaping has heard nothing from any instance of a probe for `PROBE_OFFLINE_AFTER`, a sweep that runs every 30 seconds marks the probe offline. The setting is on the server (`peekaping`, or the API in cluster mode, where the replica that holds the leader lock runs the sweep, so set it on every API replica). It is `3m` by default, and it can be 2 minutes to 24 hours.

- **Notifications.** The probe's own notification channels get "Probe `<name>` has been offline since `<time>`", and "Probe `<name>` is online" when it is back. Set them in **Edit probe**, for example to send probe notifications to an ops channel. A probe with no channels of its own tells the channels of its active monitors instead, each channel once. A paused monitor's channels get nothing.
- **Monitors.** Peekaping doesn't mark the probe's monitors Down. A location whose probe is offline doesn't count towards the monitor's status. The stale-monitor watchdog also skips a monitor whose every probe is offline, so you get the probe's notification, not one for each monitor.

**The local probe.** In single mode the local probe is the `peekaping` process, so it is never offline and takes no probe instances: `peekaping` answers 401 to a `peekaping-probe` that uses its token, and the probe logs "this install runs in single mode, where the peekaping process checks the local probe's monitors; use a remote probe's token". In cluster mode on SQLite and PostgreSQL it is its instances, so it is online while one of them is connected, and offline when they all stop: a stopped probe deployment raises the probe offline notification, and its monitors are skipped by the watchdog, as for a remote probe. If a cluster runs with no instance at all, such as one upgraded without deploying the probes, the leader reports the local probe offline once, after `PROBE_OFFLINE_AFTER`, and logs `the local probe has N active monitors and no instance: deploy peekaping-probe with PROBE_TOKEN set to PROBE_BOOTSTRAP_TOKEN`. See [Upgrading to probes](./upgrading-to-probes.md). A push monitor needs no instance, because its pushes arrive at the API.

## Several probes per monitor

A monitor can run on several probes. The monitor's page then shows each probe as a location: its own status, response time and heartbeat bar, and a selector to look at one location or all of them.

The monitor's own status follows its locations and its **location rule**, which you choose in the monitor's form once it runs on more than one probe:

- **Any location is Down** (the default): the monitor is Down while one location is Down.
- **All locations are Down**: the monitor is Down only while every location that counts is Down.
- **At least N locations are Down**: the monitor is Down while N or more locations are Down. N can be from 1 up to the number of the monitor's probes.

A location whose probe is offline, or that hasn't reported yet, doesn't count, either as Down or in the number "all" is measured against. So with "at least 2" and only one probe online, the monitor can't go Down; the probe offline notification has told you about the others. "All", or "at least" with N of 2 or more, keeps one probe's network trouble from raising an alarm while the other probes report. "At least 1" behaves like "any", and "all" counts only the probes that report: with every other probe offline, one Down location is enough.

A Down or Up notification for a monitor on several probes names its locations, such as "Down at Frankfurt, Tokyo (2 of 3 locations)". A custom message template can use `{{locations_down}}` (the names) and `{{locations_total}}`.

On a status page, switch on **Show each location** to list each location's status and history for the monitors that run on several probes.

## Upgrading

The probe and the server speak a protocol with a version, currently 1. A probe older than the server's minimum version gets the answer 426 and logs "upgrade the probe". It tries again after an hour. So when a release raises the minimum, upgrade the server first, then the probes. A new server accepts older probes until a release says otherwise.

Upgrade a probe by pulling the new image and starting the container again with the same token. With several instances, restart them one at a time.
