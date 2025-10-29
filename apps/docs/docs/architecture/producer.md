---
sidebar_position: 3
---

# Producer

The Producer is the scheduling component of Peekaping, responsible for determining when monitors should be checked and enqueueing health check tasks for workers to execute.

## Role & Responsibilities

The Producer handles:

- **Monitor Scheduling**: Schedules health checks based on each monitor's interval
- **Task Enqueueing**: Creates and enqueues health check tasks to the Redis queue
- **Leader Election**: Implements distributed leader election for high availability
- **Monitor Syncing**: Synchronizes monitor configurations from the database (leader only)
- **Lease Management**: Uses Redis-based distributed locks to prevent duplicate checks
- **Task Reclaiming**: Reclaims expired task leases to handle producer failures
- **Event Listening**: Responds to monitor lifecycle events (created, updated, deleted)

## Architecture

### High Availability via Leader Election

Multiple producer instances can run simultaneously with automatic leader election.

**Leader Responsibilities:**
- Monitor syncing (keeping redis monitor list updated)
- Handle monitor events

**All Instances:**
- Process and enqueue tasks
- Reclaim expired leases

### Task Scheduling Strategy

The producer uses a **distributed lease-based scheduling** approach:

1. Each monitor has a `next_run_time` stored in Redis
2. Producers claim a batch of monitors whose `next_run_time` has passed
3. A lease is acquired for each monitor to prevent duplicate processing
4. The producer enqueues the health check task
5. The lease expires after processing or times out
6. A reclaimer goroutine periodically reclaims expired leases

### Concurrency Model

The producer runs multiple concurrent goroutines:
- **N Producer Workers** (configurable via `PRODUCER_CONCURRENCY`)
  - Each worker independently claims and processes batches of monitors
  - Workers sleep briefly between batches to prevent CPU spinning
- **1 Reclaimer Worker**
  - Periodically scans for and reclaims expired leases
- **1 Leadership Monitor**
  - Monitors leadership status and starts/stops monitor syncing

## Environment Variables

### Database Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `DB_TYPE` | string | Yes | - | Database type: `postgres`, `mysql`, `sqlite`, `mongo`, `mongodb` |
| `DB_HOST` | string | Conditional | - | Database host (not required for SQLite) |
| `DB_PORT` | string | Conditional | - | Database port (not required for SQLite) |
| `DB_NAME` | string | Yes | - | Database name or SQLite file path |
| `DB_USER` | string | Conditional | - | Database username (not required for SQLite) |
| `DB_PASS` | string | Conditional | - | Database password (not required for SQLite) |

### Redis Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `REDIS_HOST` | string | Yes | `redis` | Redis server hostname |
| `REDIS_PORT` | string | Yes | `6379` | Redis server port |
| `REDIS_PASSWORD` | string | No | `""` | Redis password (if authentication enabled) |
| `REDIS_DB` | int | No | `0` | Redis database number (0-15) |

### Producer Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `PRODUCER_CONCURRENCY` | int | No | `10` | Number of concurrent producer workers (1-128) |
| `MODE` | string | Yes | `dev` | Runtime mode: `dev`, `prod`, or `test` |
| `LOG_LEVEL` | string | No | `debug` | Logging level: `debug`, `info`, `warn`, `error` |
| `TZ` | string | Yes | `UTC` | Timezone for the producer |
| `SERVICE_NAME` | string | Yes | `peekaping:producer` | Service identifier for logging |


## Leader Election

### Election Process

Leader election uses Redis with a heartbeat mechanism:

1. Each producer instance has a unique node ID (hostname + PID)
2. Instances compete for leadership using Redis SET NX (set if not exists)
3. The leader maintains its lease by refreshing a TTL key
4. If the leader crashes, its lease expires and a new election occurs
5. Other instances detect the leadership change and promote themselves

### Leadership Monitoring

The leadership monitor runs every second to:
- Check current leadership status
- Start monitor syncing if elected leader
- Stop monitor syncing if leadership is lost


## Monitor Syncing

The leader periodically syncs monitor configurations from the database:

1. Fetch all active monitors from the database
2. Update in-memory monitor intervals map
3. Initialize `next_run_time` for new monitors in Redis
4. Remove deleted monitors from Redis


## Event Handling

The producer subscribes to monitor lifecycle events via Redis pub/sub:

### Monitor Created Event
- Adds monitor to the schedule
- Sets initial `next_run_time`

### Monitor Updated Event
- Updates monitor interval
- Adjusts `next_run_time` if interval changed

### Monitor Deleted Event
- Removes monitor from schedule
- Cleans up Redis keys

## Graceful Shutdown

On receiving `SIGTERM` or `SIGINT`:

1. Cancel all goroutines via context cancellation
2. Wait for all workers to finish current tasks
3. Close Redis event bus
4. Close database connections

The producer logs shutdown progress:
```
INFO Shutdown signal received, stopping producer...
INFO Producer stopped gracefully
```

## Scaling Considerations

### Vertical Scaling
Increase `PRODUCER_CONCURRENCY` to process more monitors concurrently on a single instance.

### Horizontal Scaling
Run multiple producer instances for high availability and load distribution.

**Benefits:**
- Automatic failover if one instance crashes
- Load distribution across instances
- Zero-downtime deployments

**Considerations:**
- Only one instance is leader (syncs monitors)
- All instances process tasks from the queue
- More instances = more database/Redis connections

## Related Components

- [API Server](./api-server.md) - Manages monitor configurations
- [Worker](./worker.md) - Executes health checks enqueued by producer
- [Ingester](./ingester.md) - Processes health check results

