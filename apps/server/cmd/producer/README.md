# Producer Service

The Producer service is responsible for scheduling monitor health checks using a distributed, high-availability architecture with leader election.

## Overview

The Producer service:
- **Implements Leader Election**: Uses Redis-based distributed locking to ensure only one producer instance is active at a time
- **Manages Monitor Schedules**: Converts monitor intervals to cron jobs and manages them dynamically
- **Enqueues Tasks**: When a cron job triggers, it creates a health check task in the Redis queue
- **Listens to Events**: Automatically syncs when monitors are created, updated, or deleted

## Architecture

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│  Producer 1  │────▶│     Redis    │◀────│  Producer 2  │
│   (Leader)   │     │ (Leader Lock)│     │  (Standby)   │
└──────┬───────┘     └──────────────┘     └──────────────┘
       │
       │ Schedules
       ▼
┌──────────────────┐
│   Cron Scheduler │
│  (Per Monitor)   │
└──────┬───────────┘
       │
       │ Enqueues
       ▼
┌──────────────────┐
│   Redis Queue    │
│    (Asynq)       │
└──────┬───────────┘
       │
       │ Consumes
       ▼
┌──────────────────┐
│ Worker Processes │
│ (Health Checks)  │
└──────────────────┘
```

## Components

### 1. Leader Election (`leader_election.go`)

- Uses Redis `SETNX` command for distributed locking
- Leader key: `peekaping:producer:leader`
- TTL: 10 seconds
- Renewal interval: 5 seconds
- Automatically releases leadership on graceful shutdown

### 2. Monitor Scheduler (`monitor_scheduler.go`)

- Uses `robfig/cron/v3` for job scheduling
- Converts monitor intervals (seconds) to cron expressions: `@every {interval}s`
- Maintains a map of monitor ID to cron entry ID
- Supports add, update, and remove operations

### 3. Event Listener (`event_listener.go`)

- Subscribes to monitor lifecycle events:
  - `monitor.created` - Adds new monitor to scheduler
  - `monitor.updated` - Updates monitor schedule
  - `monitor.deleted` - Removes monitor from scheduler

### 4. Producer (`producer.go`)

- Orchestrates all components
- Monitors leadership status
- Syncs monitors every 5 minutes when leader
- Provides health check and status information

## Task Format

When a cron job triggers, it enqueues a task to the queue:

**Task Type**: `monitor:healthcheck`

**Payload**:
```json
{
  "monitor_id": "abc123",
  "scheduled_at": "2025-10-02T10:00:00Z"
}
```

**Queue Options**:
- Queue: `default`
- Max Retry: 3
- Timeout: 5 minutes
- Retention: 1 hour

## Running the Producer

### Prerequisites

1. Redis instance running (for leader election and queue)
2. Database (PostgreSQL, MySQL, SQLite, or MongoDB)
3. Environment variables configured (see Configuration)

### Start the Producer

```bash
cd apps/server/cmd/producer
go run main.go
```

### Build and Run

```bash
# Build
go build -o producer cmd/producer/main.go

# Run
./producer
```

## Configuration

The producer uses the same configuration as the main server. Key environment variables:

```bash
# Database
DB_TYPE=postgres
DB_HOST=localhost
DB_PORT=5432
DB_NAME=peekaping
DB_USER=postgres
DB_PASS=password

# Redis
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# Application
MODE=dev
LOG_LEVEL=info
TZ=UTC
```

## High Availability

To run multiple producers for high availability:

1. Start multiple producer instances on different nodes:
   ```bash
   # Node 1
   ./producer

   # Node 2
   ./producer

   # Node 3
   ./producer
   ```

2. Only one instance will be the leader at any time
3. If the leader fails, another instance will automatically take over within ~10 seconds
4. The standby instances will detect leadership changes and activate when they become leader

## Monitoring

### Logs

The producer logs important events:

```
[INFO] Starting producer
[INFO] Starting leader election for node: abc-123-def
[INFO] Became leader node_id=abc-123-def
[INFO] Syncing monitors with scheduler
[INFO] Added/updated monitor job monitor_id=mon-1 interval=60 cron_expr=@every 60s
[INFO] Synced 5 active monitors
[DEBUG] Enqueuing health check task monitor_id=mon-1
```

### Health Check

Check if producer is running and its status:
- Check logs for leader status
- Monitor Redis key `peekaping:producer:leader` to see current leader

### Metrics

The scheduler provides statistics:
```go
stats := scheduler.GetStats()
// Returns: {"total_jobs": 10, "cron_entries": 10}
```

## Development

### Adding New Features

1. **New task types**: Add to `monitor_scheduler.go` with new task type constants
2. **Custom scheduling logic**: Modify `addOrUpdateMonitorJob` in `monitor_scheduler.go`
3. **Additional event handlers**: Add new handlers in `event_listener.go`

### Testing

```bash
# Run tests
go test ./internal/modules/producer/...

# Test leader election manually
# Start two producers and kill the leader to see failover
```

## Troubleshooting

### Producer not becoming leader

- Check Redis connectivity
- Verify no other producer is holding the lock
- Check Redis key: `redis-cli GET peekaping:producer:leader`

### Monitors not being scheduled

- Verify monitors are active: `active = true`
- Check logs for sync errors
- Ensure producer is the leader

### Tasks not being enqueued

- Verify Redis queue is accessible
- Check queue service is properly initialized
- Look for errors in logs during task enqueue

### Multiple leaders at once

- This should never happen with proper Redis configuration
- If it does, check for network partitions or Redis cluster issues

## Future Enhancements

Potential improvements:
- [ ] Metrics endpoint for Prometheus
- [ ] Admin API for manual sync/status
- [ ] Graceful degradation if Redis is unavailable
- [ ] Support for time zones in scheduling
- [ ] Priority-based scheduling
- [ ] Batch task enqueuing for efficiency

