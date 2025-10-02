# Producer Service Architecture

## Overview

The Producer service implements a **distributed, high-availability task scheduling system** for monitor health checks using leader election, cron-based scheduling, and Redis-backed queueing.

## Key Design Principles

### 1. Leader Election Pattern

The producer uses a leader election pattern to ensure:
- **Single Active Scheduler**: Only one producer instance schedules tasks at a time
- **High Availability**: Multiple standby instances ready to take over
- **Automatic Failover**: If leader fails, standby takes over within ~10 seconds
- **No Split-Brain**: Redis atomic operations prevent multiple leaders

### 2. Separation of Concerns

```
┌─────────────────────────────────────────────────────────┐
│                    Producer Service                      │
├─────────────────────────────────────────────────────────┤
│                                                           │
│  ┌──────────────────┐                                   │
│  │ Leader Election  │  ← Redis-based distributed lock   │
│  └────────┬─────────┘                                   │
│           │                                              │
│           ▼                                              │
│  ┌──────────────────┐                                   │
│  │     Producer     │  ← Orchestrator                   │
│  └────────┬─────────┘                                   │
│           │                                              │
│           ├──────────────────────┬──────────────────┐   │
│           ▼                      ▼                  ▼   │
│  ┌──────────────────┐   ┌──────────────┐  ┌──────────┐ │
│  │Monitor Scheduler │   │Event Listener│  │  Syncer  │ │
│  └────────┬─────────┘   └──────┬───────┘  └─────┬────┘ │
│           │                    │                 │      │
└───────────┼────────────────────┼─────────────────┼──────┘
            │                    │                 │
            ▼                    ▼                 ▼
     ┌──────────┐         ┌──────────┐     ┌──────────┐
     │  Cron    │         │EventBus  │     │ Database │
     │  Jobs    │         │(In-Mem)  │     │(Monitors)│
     └────┬─────┘         └──────────┘     └──────────┘
          │
          ▼
     ┌──────────┐
     │  Queue   │
     │ (Redis)  │
     └──────────┘
```

## Components Deep Dive

### Leader Election (`leader_election.go`)

**Responsibility**: Manage distributed leadership across multiple producer instances.

**Implementation**:
```go
// Pseudo-code flow
1. Try to acquire lock: SETNX(leader_key, node_id, TTL=10s)
2. If successful → Become leader
3. If failed → Check current leader
4. If we are current leader → Renew lock (EXPIRE)
5. Repeat every 5 seconds
```

**Key Features**:
- Uses Redis `SETNX` for atomic lock acquisition
- Lock TTL: 10 seconds (prevents deadlock if leader crashes)
- Renewal interval: 5 seconds (keeps lock fresh)
- Graceful release using Lua script (prevents race conditions)

**Failure Scenarios**:
- **Leader crashes**: Lock expires after 10s, standby becomes leader
- **Network partition**: Lock expires, new leader elected
- **Redis failure**: Service continues with last known state (degrades gracefully)

### Monitor Scheduler (`monitor_scheduler.go`)

**Responsibility**: Manage cron jobs for each monitor and enqueue tasks when they trigger.

**Data Flow**:
```
Monitor DB → Scheduler → Cron Jobs → Task Queue
```

**Key Operations**:

1. **Sync Monitors**:
   - Fetch all active monitors from database
   - Add new monitors as cron jobs
   - Remove jobs for deleted monitors
   - Update jobs for modified monitors

2. **Cron Job Creation**:
   ```go
   // Convert interval to cron expression
   interval := 60 // seconds
   cronExpr := "@every 60s"

   // Create job that enqueues task
   job := func() {
       enqueueHealthCheckTask(monitorID)
   }

   // Add to cron scheduler
   cronID := cron.AddFunc(cronExpr, job)
   ```

3. **Task Enqueuing**:
   ```go
   payload := {
       "monitor_id": "abc123",
       "scheduled_at": "2025-10-02T10:00:00Z"
   }

   queueService.Enqueue(
       taskType: "monitor:healthcheck",
       payload: payload,
       options: {
           queue: "default",
           max_retry: 3,
           timeout: "5m"
       }
   )
   ```

**Concurrency Control**:
- Uses `sync.RWMutex` to protect job map
- All operations are thread-safe
- Cron library handles concurrent job execution

### Event Listener (`event_listener.go`)

**Responsibility**: Keep scheduler in sync with real-time monitor changes.

**Event Flow**:
```
Monitor CRUD → EventBus → Event Listener → Scheduler
```

**Subscribed Events**:
- `monitor.created` → Add monitor to scheduler
- `monitor.updated` → Update monitor schedule
- `monitor.deleted` → Remove monitor from scheduler

**Why Events?**
- **Real-time updates**: No need to wait for periodic sync
- **Efficiency**: Only sync changed monitors, not all
- **Decoupling**: Producer doesn't need to know about CRUD operations

### Producer (`producer.go`)

**Responsibility**: Orchestrate all components and manage leadership lifecycle.

**Lifecycle**:
```
Start → Leader Election → Wait for Leadership → Sync Monitors → Monitor Loop
                              ↓
                        (If not leader, wait)
                              ↓
                        (If become leader, sync)
                              ↓
                        (Periodic sync every 5min)
```

**Key Features**:
- Monitors leadership status continuously
- Syncs monitors when becoming leader
- Periodic sync every 5 minutes (safety net)
- Logs statistics for observability

## Data Flow: Task Creation

Let's trace a complete flow from monitor creation to task execution:

```
┌──────────────────────────────────────────────────────────────┐
│ 1. User creates monitor via API                              │
│    POST /api/monitors                                         │
│    { "name": "My API", "interval": 60, "active": true }     │
└────────────────────┬─────────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────────┐
│ 2. Server saves monitor to database                          │
│    INSERT INTO monitors (...)                                 │
└────────────────────┬─────────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────────┐
│ 3. Server publishes event                                     │
│    eventBus.Publish(MonitorCreated, monitorID)               │
└────────────────────┬─────────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────────┐
│ 4. Producer Event Listener receives event                    │
│    handleMonitorCreated(monitorID)                            │
└────────────────────┬─────────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────────┐
│ 5. Scheduler adds cron job                                   │
│    cron.AddFunc("@every 60s", enqueueTask)                   │
└────────────────────┬─────────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────────┐
│ 6. Cron triggers (every 60 seconds)                          │
│    enqueueTask() called                                       │
└────────────────────┬─────────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────────┐
│ 7. Task enqueued to Redis                                    │
│    RPUSH queue:default {task}                                │
└────────────────────┬─────────────────────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────────────────────┐
│ 8. Worker picks up task                                      │
│    [This would be implemented in a separate worker service]  │
└──────────────────────────────────────────────────────────────┘
```

## High Availability Scenario

### Scenario: Leader Failure

```
Time: 10:00:00
┌─────────────┐              ┌─────────────┐              ┌─────────────┐
│ Producer A  │              │ Producer B  │              │ Producer C  │
│  (Leader)   │              │ (Standby)   │              │ (Standby)   │
└──────┬──────┘              └──────┬──────┘              └──────┬──────┘
       │                            │                            │
       │ Schedules tasks            │ Waiting...                 │ Waiting...
       │                            │                            │
Time: 10:00:05
       │                            │                            │
       │ Renews lock                │ Checks lock                │ Checks lock
       │ (successful)               │ (sees A is leader)         │ (sees A is leader)
       │                            │                            │
Time: 10:00:07
       ✗ CRASHES!                   │                            │
                                    │                            │
Time: 10:00:08                      │                            │
                                    │ Checks lock                │ Checks lock
                                    │ (A still leader)           │ (A still leader)
                                    │                            │
Time: 10:00:12 (lock expires)       │                            │
                                    │ Tries to acquire           │ Tries to acquire
                                    │ SUCCESS! → Leader          │ FAIL → Standby
                                    │                            │
Time: 10:00:13                      │                            │
                                    │ Syncs monitors             │ Waiting...
                                    │ Starts scheduling          │
                                    │                            │
Total downtime: ~7 seconds          │                            │
```

## Configuration

### Environment Variables

The producer uses the same configuration as the main server:

```bash
# Database (required)
DB_TYPE=postgres|mysql|sqlite|mongo
DB_HOST=localhost
DB_PORT=5432
DB_NAME=peekaping
DB_USER=postgres
DB_PASS=password

# Redis (required)
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# Optional
MODE=dev|prod
LOG_LEVEL=debug|info|warn|error
TZ=UTC
```

### Tunable Parameters

In the code, you can adjust:

```go
// leader_election.go
LeaderTTL = 10 * time.Second          // Lock lifetime
LeaderRenewalInterval = 5 * time.Second // Renewal frequency

// producer.go
syncInterval = 5 * time.Minute        // Periodic sync interval

// monitor_scheduler.go
MaxRetry = 3                          // Task retry attempts
Timeout = 5 * time.Minute             // Task timeout
Retention = 1 * time.Hour             // Keep completed tasks
```

## Performance Considerations

### Scalability

**Monitors**: Can handle thousands of monitors per producer instance
- Cron library is efficient with many jobs
- Each job is a lightweight goroutine

**Task Enqueuing**: Redis can handle high throughput
- Asynq uses pipelining for efficiency
- Tasks are enqueued asynchronously

**Memory**: Minimal memory footprint
- Only stores monitor ID → cron entry ID map
- No monitor state stored in memory

### Bottlenecks

1. **Redis**: Single point of failure for both locking and queueing
   - Solution: Use Redis Sentinel or Cluster for HA

2. **Database**: Periodic sync queries all monitors
   - Solution: Add indexes on `active` column
   - Solution: Consider caching active monitor list

3. **Leader Election**: 5-10 second failover time
   - Solution: Reduce TTL/renewal interval (trade-off: more Redis ops)

## Future Enhancements

### Monitoring & Observability
- [ ] Prometheus metrics endpoint
- [ ] Health check endpoint
- [ ] Distributed tracing support

### Functionality
- [ ] Priority-based scheduling (critical monitors first)
- [ ] Batch task enqueuing (reduce Redis roundtrips)
- [ ] Time zone support for scheduling
- [ ] Scheduled maintenance windows

### Reliability
- [ ] Circuit breaker for database failures
- [ ] Graceful degradation if Redis unavailable
- [ ] Backup scheduling mechanism
- [ ] Task deduplication for overlapping schedules

### Operations
- [ ] Admin API for manual operations
- [ ] Dynamic configuration updates
- [ ] Monitor schedule preview
- [ ] Task statistics and analytics

