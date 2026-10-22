---
sidebar_position: 5
---

# Ingester

The Ingester is the data persistence and event processing component of Peekaping. It consumes health check results from workers, stores them in the database, detects status changes, triggers notifications, and maintains statistics.

## Role & Responsibilities

The Ingester handles:

- **Result Processing**: Consumes health check results from the ingester queue
- **Heartbeat Storage**: Stores health check results (heartbeats) in the database
- **Status Change Detection**: Detects when a monitor's status changes (up ↔ down)
- **Notification Triggering**: Publishes notification events when status changes
- **TLS Certificate Storage**: Stores TLS certificate information for HTTPS monitors
- **Statistics Updates**: Publishes statistics events for real-time dashboard updates
- **Retry Logic**: Manages retry counting before marking monitors as down
- **Maintenance Awareness**: Respects maintenance windows

## Architecture

### Queue-Based Processing

The ingester consumes from a dedicated queue.

### Concurrency Model

Ingesters can run multiple tasks concurrently based on `QUEUE_CONCURRENCY`:
- Multiple ingester instances can run simultaneously
- Task distribution is automatic via the queue
- Each ingester independently processes results

## Environment Variables

### Database Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `DB_TYPE` | string | Yes | - | Storage backend: `postgres`, `mysql`, `sqlite`, `mongo`, `mongodb` (MongoDB storage is [deprecated](../self-hosting/docker-with-mongo.md)) |
| `DB_HOST` | string | Conditional | - | Database host (not required for SQLite) |
| `DB_PORT` | string | Conditional | - | Database port (not required for SQLite) |
| `DB_NAME` | string | Yes | - | Database name or SQLite file path |
| `DB_USER` | string | Conditional | - | Database username (not required for SQLite) |
| `DB_PASS` | string | Conditional | - | Database password (not required for SQLite) |

### Redis Configuration

:::note Single mode
These are required in cluster mode. In single mode, the `peekaping` binary stores the results as heartbeats in the same process as the API, with no Redis, so it doesn't need `REDIS_HOST`.
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
| `TZ` | string | Yes | `UTC` | Timezone for the ingester |
| `SERVICE_NAME` | string | Yes | `peekaping:ingester` | Service identifier for logging |


## Scaling

### Vertical Scaling

Increase `QUEUE_CONCURRENCY` to process more results concurrently on a single instance.


### Horizontal Scaling

Run multiple ingester instances for increased throughput.

**Benefits:**
- Linear scaling of ingestion capacity
- Fault tolerance
- Zero-downtime deployments

**Considerations:**
- Each ingester needs database access
- More instances = more database connections
- Consider database connection pool limits

## Graceful Shutdown

On receiving `SIGTERM` or `SIGINT`, the ingester stops in this order and exits 0 within 30 seconds:

1. Stop taking tasks from the queue. The ingests running finish storing their heartbeats, for up to 20 seconds. An ingest still running after that goes back to the queue.
2. Close the event bus. The handlers of the events already queued finish, such as the ones that update the stats. They get 10 seconds.
3. Close the database. SQLite checkpoints its write-ahead log first.

The ingester logs each step once it is done:
```
INFO Shutdown signal received, shutting down
INFO Stopping ingester
INFO Ingester stopped
INFO Stopped the ingester
INFO Closed the event bus
INFO Closed the database
INFO Ingester shut down
```

If a step doesn't finish in time, the ingester logs an error, leaves the database open and exits 1. A second signal makes it exit 1 at once.

Give the container 35 seconds to stop. The cluster Compose files and the Helm chart do. See the [producer's shutdown](./producer.md#graceful-shutdown) for the details.


## Related Components

- [Worker](./worker.md) - Enqueues health check results for ingester
- [API Server](./api-server.md) - Provides notification channel configuration
- [Producer](./producer.md) - Schedules monitors that generate heartbeats
