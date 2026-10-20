---
sidebar_position: 4
---

# Worker

The Worker is the execution engine of Peekaping, responsible for performing actual health checks on monitored services and applications. It consumes tasks from the Redis queue, executes the appropriate health check, and enqueues results for the ingester to process.

## Role & Responsibilities

The Worker handles:

- **Task Consumption**: Pulls health check tasks from the Redis queue
- **Health Check Execution**: Performs various types of health checks (HTTP, TCP, Ping, DNS, etc.)
- **Proxy Support**: Routes checks through configured proxy servers
- **TLS Validation**: Validates and extracts TLS certificate information
- **Timeout Management**: Enforces timeouts for all health checks
- **Result Enqueueing**: Sends health check results to the ingester queue
- **Stale Task Detection**: Skips outdated tasks to prevent backlog processing

## Architecture

### Queue-Based Processing

The worker uses Asynq (Redis-based task queue) to consume tasks.


### Health Check Executors

The worker uses a registry of executors for different monitor types:

| Monitor Type | Executor | Description |
|--------------|----------|-------------|
| `http` / `https` | HTTP Executor | HTTP/HTTPS requests with various methods |
| `tcp` | TCP Executor | TCP port connectivity checks |
| `ping` / `icmp` | Ping Executor | ICMP ping checks |
| `dns` | DNS Executor | DNS query resolution |
| `push` | N/A | Passive monitoring (no active checks) |
| `docker` | Docker Executor | Docker container status checks |
| `grpc` | gRPC Executor | gRPC health checks |
| `websocket` | WebSocket Executor | WebSocket connection checks |
| And more... | | Extensible executor registry |

### Ping Monitors and ICMP Permissions

Ping monitors send ICMP echo requests from the worker, or from the `peekaping` process in single mode. IPv4 and IPv6 targets are supported; a hostname with both kinds of address is pinged over IPv4. The worker opens one of two kinds of socket:

1. **Unprivileged ICMP socket** (tried first). Linux allows it for processes whose group, or one of their other groups, is in the `net.ipv4.ping_group_range` sysctl, for IPv4 and IPv6 alike. It needs no capability, and allows ICMP echo only.
2. **Raw ICMP socket**. Needs root with the `NET_RAW` capability.

If neither socket can be opened, the worker runs the system `ping` command. The `peekaping-worker` image and the SQLite bundle have BusyBox `ping`, which needs the same permissions. The PostgreSQL and MongoDB bundles have no `ping`. Either way the monitor reports DOWN with `cannot open an ICMP socket` and a hint on how to fix it.

None of the images runs Peekaping as root: the `peekaping-worker` image runs as group 65532, and the bundle images as group 1000 (`PGID`). So ping needs `net.ipv4.ping_group_range` to include that group. What this means for each deployment:

- **Docker and Docker Compose**: works by default. Docker sets `net.ipv4.ping_group_range` to all groups in every container with its own network namespace. It doesn't with rootless Docker or `userns-remap`: there, give the `peekaping-worker` service `sysctls: ["net.ipv4.ping_group_range=65532 65532"]`. With `network_mode: host` the container shares the host's network namespace, Docker refuses `net.*` sysctls, and the service won't start with them. Set `net.ipv4.ping_group_range` on the host instead (`sysctl -w`, and a file in `/etc/sysctl.d/` to keep it). For the bundle images, see [Bundle user and permissions](../self-hosting/bundle-user.md#ping-monitors).
- **Podman**: allows only group 0 by default. Add `--sysctl net.ipv4.ping_group_range="65532 65532"` for the `peekaping-worker` image, or `"1000 1000"` (your `PGID`) for a bundle image. Rootless Podman rejects ranges that include groups outside its user namespace, such as `0 2147483647`.
- **Kubernetes**: the Helm chart sets `net.ipv4.ping_group_range` on worker pods through `worker.sysctls`. It is a safe sysctl, allowed under the baseline and restricted Pod Security Standards. If you deploy without the chart, set it in the pod's `securityContext.sysctls`.

Adding `NET_RAW` (for example `cap_add: [NET_RAW]` in Compose) only helps a worker that runs as root. A non-root process doesn't get the container's capabilities, so it doesn't help the `peekaping-worker` image or the bundle images. Use `net.ipv4.ping_group_range` instead.

### Concurrency Model

Workers can run multiple tasks concurrently based on the `QUEUE_CONCURRENCY` setting:
- Each worker instance processes tasks from the queue
- Multiple worker instances can run simultaneously for horizontal scaling
- Task distribution is automatic (first available worker gets the task)
- No database connection required (stateless execution)

## Environment Variables

### Redis Configuration (Required)

:::note Single mode
These are required in cluster mode. In single mode, the `peekaping` binary runs the checks in the same process as the API, with no Redis, so it doesn't need `REDIS_HOST`.
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
| `QUEUE_CONCURRENCY` | int | No | `128` | Maximum concurrent task processing |

### General Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `MODE` | string | Yes | `dev` | Runtime mode: `dev`, `prod`, or `test` |
| `LOG_LEVEL` | string | No | `info` | Logging level: `debug`, `info`, `warn`, `error` |
| `TZ` | string | Yes | `UTC` | Timezone for the worker |
| `SERVICE_NAME` | string | Yes | `peekaping:worker` | Service identifier for logging |

## Task Processing Flow

1. The worker receives a health check task with payload
2. The worker checks if the task is stale
3. Health Check Execution
4. The worker enqueues the result to the ingester queue:


## Scaling

### Vertical Scaling

Increase `QUEUE_CONCURRENCY` to process more tasks concurrently on a single instance.

### Horizontal Scaling

Run multiple worker instances for increased throughput.

**Benefits:**
- Linear scaling of execution capacity
- Fault tolerance (if one worker crashes, others continue)
- Zero-downtime deployments

**Considerations:**
- No coordination needed between workers
- Workers are stateless (no database connection)
- Each worker consumes memory and CPU

### Queue Configuration

Asynq server configuration:
- **Queue**: `default` (priority 10)
- **Concurrency**: Controlled by `QUEUE_CONCURRENCY`
- **Strict Priority**: Disabled (single queue)

## Related Components

- [Producer](./producer.md) - Enqueues health check tasks for workers
- [Ingester](./ingester.md) - Processes health check results from workers
- [API Server](./api-server.md) - Manages monitor configurations
