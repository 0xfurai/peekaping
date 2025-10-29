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

### Concurrency Model

Workers can run multiple tasks concurrently based on the `QUEUE_CONCURRENCY` setting:
- Each worker instance processes tasks from the queue
- Multiple worker instances can run simultaneously for horizontal scaling
- Task distribution is automatic (first available worker gets the task)
- No database connection required (stateless execution)

## Environment Variables

### Redis Configuration (Required)

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `REDIS_HOST` | string | Yes | `redis` | Redis server hostname |
| `REDIS_PORT` | string | Yes | `6379` | Redis server port |
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

