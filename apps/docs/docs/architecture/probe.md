---
sidebar_position: 3
---

# Probe

The probe, `peekaping-probe`, is the program that runs the checks. It connects out to the [API](./api-server.md), gets the monitors it should check, runs each one on its interval and sends the results back. It has no database and no Redis, and it keeps nothing on disk.

A probe is a place Peekaping runs checks from. The same program runs the **local probe**, which every install has, and any [remote probe](../self-hosting/remote-probes.md) you add.

## Who runs the local probe's checks

| Layout | What checks the local probe's monitors |
| --- | --- |
| [Single mode](../self-hosting/deployment-modes.md#single-mode) | The `peekaping` process itself, in memory. A `peekaping-probe` can connect only as a [remote probe](../self-hosting/remote-probes.md): `peekaping` refuses one that uses the local probe's token. |
| [Cluster mode](../self-hosting/deployment-modes.md#cluster-mode) | `peekaping-probe` instances that hold the local probe's token. This page describes them. |

In cluster mode, the api stores the local probe's token at startup from `PROBE_BOOTSTRAP_TOKEN` (32 characters or more). Every probe instance gets the same value in `PROBE_TOKEN` and the api's address in `PEEKAPING_URL`. The compose files and the Helm chart set all three for you. See [Upgrading to probes](../self-hosting/upgrading-to-probes.md) if you run an older layout.

## Role & Responsibilities

The probe handles:

- **Assignment**: asks the API for its monitors with a long poll. The API deals the local probe's active monitors (push monitors excepted) among its online instances that run the monitor's type, so each monitor is checked by one instance at a time, and deals them again when an instance joins or leaves. Each instance lists the monitor types its build runs when it connects: the full and the [slim](../self-hosting/remote-probes.md#full-and-slim-instances-under-one-token) image can share a token.
- **Scheduling**: runs each monitor's check on its interval, in the probe's own process.
- **Health check execution**: runs the check for the monitor's type (HTTP, TCP, Ping, DNS and so on).
- **Proxy support and TLS validation**: routes checks through the monitor's proxy, and reads the TLS certificate.
- **Timeout management**: enforces each monitor's timeout.
- **Result delivery**: sends the results to the API in gzipped batches, oldest first. When it can't reach the API it keeps checking and holds up to 10,000 results in memory, then drops the oldest.

A push monitor is never dealt to a probe, because pushes arrive at the API. The API replica that holds the leader lock judges it: a push that doesn't arrive in time counts as Down.

### Health check executors

The probe runs the same executors as single mode:

| Monitor Type | Executor | Description |
|--------------|----------|-------------|
| `http` / `https` | HTTP Executor | HTTP/HTTPS requests with various methods |
| `tcp` | TCP Executor | TCP port connectivity checks |
| `ping` / `icmp` | Ping Executor | ICMP ping checks |
| `dns` | DNS Executor | DNS query resolution |
| `push` | N/A | The API judges it, not a probe |
| `docker` | Docker Executor | Docker container status checks |
| `grpc` | gRPC Executor | gRPC health checks |
| `websocket` | WebSocket Executor | WebSocket connection checks |
| And more... | | Extensible executor registry |

A monitor of a type the probe has no executor for is Down with `monitor type <type> not supported by this probe`.

### Ping Monitors and ICMP Permissions

Ping monitors send ICMP echo requests from the probe (or from the `peekaping` process in single mode). IPv4 and IPv6 targets are supported; a hostname with both kinds of address is pinged over IPv4. The probe opens one of two kinds of socket:

1. **Unprivileged ICMP socket** (tried first). Linux allows it for processes whose group, or one of their other groups, is in the `net.ipv4.ping_group_range` sysctl, for IPv4 and IPv6 alike. It needs no capability, and allows ICMP echo only.
2. **Raw ICMP socket**. Needs the `NET_RAW` capability in the process's effective set. A root process in a container has it when the runtime grants it. A non-root process has it only when the runtime or the image sets it as effective for that process, for example through ambient or file capabilities, which the Peekaping images don't.

If neither socket can be opened, the probe runs the system `ping` command. The `peekaping-probe` image and the SQLite bundle have BusyBox `ping`, which needs the same permissions. The PostgreSQL bundle has no `ping`. Either way the monitor reports DOWN with `cannot open an ICMP socket` and a hint on how to fix it.

None of the images runs Peekaping as root: the `peekaping-probe` image runs as group 65532, and the bundle images as group 1000 (`PGID`). So ping needs `net.ipv4.ping_group_range` to include that group. What this means for each deployment:

- **Docker and Docker Compose**: works by default. Docker sets `net.ipv4.ping_group_range` to all groups in every container with its own network namespace. It doesn't with rootless Docker or `userns-remap`: there, give the `probe` service `sysctls: ["net.ipv4.ping_group_range=65532 65532"]`. With `network_mode: host` the container shares the host's network namespace, Docker refuses `net.*` sysctls, and the service won't start with them. Set `net.ipv4.ping_group_range` on the host instead (`sysctl -w`, and a file in `/etc/sysctl.d/` to keep it). For the bundle images, see [Bundle user and permissions](../self-hosting/bundle-user.md#ping-monitors).
- **Podman**: allows only group 0 by default. Add `--sysctl net.ipv4.ping_group_range="65532 65532"` for the `peekaping-probe` image, or `"1000 1000"` (your `PGID`) for a bundle image. Rootless Podman rejects ranges that include groups outside its user namespace, such as `0 2147483647`.
- **Kubernetes**: the Helm chart sets `net.ipv4.ping_group_range` on probe pods through `probe.sysctls`. It is a safe sysctl, allowed under the baseline and restricted Pod Security Standards. If you deploy without the chart, set it in the pod's `securityContext.sysctls`.

Adding `NET_RAW` (for example `cap_add: [NET_RAW]` in Compose) usually doesn't help the `peekaping-probe` image or the bundle images. It adds the capability to the container, but most runtimes don't make it effective for a non-root process, and these images run as non-root and set no ambient or file capabilities. It helps a probe that runs as root. If your runtime does make `NET_RAW` effective for a non-root process, the raw socket works as a fallback when `ping_group_range` is unavailable, but `net.ipv4.ping_group_range` is the supported way.

## Environment Variables

The probe reads its settings from the environment: `PEEKAPING_URL`, `PROBE_TOKEN`, `PROBE_ALLOW_INSECURE`, `PROBE_CONCURRENCY`, `PROBE_READY_ADDR` and `LOG_LEVEL`. [Remote probes](../self-hosting/remote-probes.md#settings) lists each one with its default. It has no `DB_*` or `REDIS_*` variables.

The API reads `PROBE_BOOTSTRAP_TOKEN` (the local probe's token) and `PROBE_OFFLINE_AFTER` (how long a silent probe may stay quiet before it is offline). See [API Server](./api-server.md#local-probe-configuration).

## Scaling

### Vertical Scaling

Raise `PROBE_CONCURRENCY` (128 by default) to run more checks at the same time in one instance.

### Horizontal Scaling

Run more probe instances to add capacity. In Docker Compose, `docker compose up -d --scale probe=3`. With Helm, set `probe.replicaCount` (3 by default).

**Benefits:**
- Each monitor is checked by one instance at a time, and the API deals the monitors again when an instance joins or leaves
- A stopped or failed instance doesn't stop the checks: the others take over its monitors
- Zero-downtime deployments

**Considerations:**
- Instances need no coordination, no database and no Redis
- Each instance connects to the API, so it needs only an outbound route to it
- The API limits each instance's calls to the probe protocol. See [Request limit](../self-hosting/remote-probes.md#request-limit)

## Graceful Shutdown

On `SIGTERM` or `SIGINT`, the probe stops its timers, lets the running checks finish, sends the results it still holds and says goodbye to the API. That takes up to 25 seconds. The Compose files set `stop_grace_period: 30s` for the `probe` service, and the Helm chart sets `terminationGracePeriodSeconds: 35`.

## Health

The image has a Docker health check, and the Helm chart a readiness probe, on `/ready`. It answers 503 until the probe has its first assignment, and 200 from then on, also while the API is unreachable. A probe with a refused token never becomes ready. There is no liveness probe, because `/ready` stays 200 by design.

## Related Components

- [API Server](./api-server.md) - Deals the monitors, stores the results and runs the leader jobs
- [Remote probes](../self-hosting/remote-probes.md) - Add probes on other machines
- [Deployment modes](../self-hosting/deployment-modes.md) - Who runs the checks in each mode
