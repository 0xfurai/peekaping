---
sidebar_position: 2
---

# API Server

The API Server is the main entry point for all client interactions with Peekaping. It provides a RESTful API, WebSocket connections for real-time updates, and serves as the authentication gateway.

## Role & Responsibilities

The API Server handles:

- **HTTP API**: RESTful endpoints for all CRUD operations
- **Authentication**: User login, registration, JWT token management, and 2FA
- **Authorization**: Role-based access control and API key validation
- **WebSocket Server**: Real-time bidirectional communication with web clients
- **API Documentation**: Auto-generated Swagger/OpenAPI documentation
- **Security**: Brute force protection, rate limiting, CORS handling
- **Push Endpoint**: Receives heartbeats from push-based monitors
- **Cleanup Tasks**: Scheduled cleanup of old data
- **Prometheus Metrics**: Exposes per-monitor metrics for scraping

## Architecture

### Dependency Injection
The API server uses Uber's Dig for dependency injection, registering all modules and their dependencies at startup.

### Event-Driven Communication
The API server subscribes to Redis pub/sub events and broadcasts them to connected WebSocket clients for real-time updates.

## Environment Variables

### Server Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `SERVER_PORT` | string | Yes | `8034` | Port the API server listens on |
| `CLIENT_URL` | string | Yes | `http://localhost:3000` | Frontend URL for CORS configuration |
| `MODE` | string | Yes | `dev` | Runtime mode: `dev`, `prod`, or `test` |
| `LOG_LEVEL` | string | No | `info` | Logging level: `debug`, `info`, `warn`, `error` |
| `TZ` | string | Yes | `UTC` | Timezone for the server |
| `SERVICE_NAME` | string | Yes | `peekaping:api` | Service identifier for logging and monitoring |

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

### Queue Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `QUEUE_CONCURRENCY` | int | No | `128` | Maximum concurrent queue operations |
| `PRODUCER_CONCURRENCY` | int | No | `10` | Concurrent producers for push endpoint (1-128) |

### Security Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `BRUTEFORCE_MAX_ATTEMPTS` | int | No | `20` | Maximum login attempts before lockout |
| `BRUTEFORCE_WINDOW` | duration | No | `1m` | Time window for counting failed attempts |
| `BRUTEFORCE_LOCKOUT` | duration | No | `1m` | Lockout duration after max attempts |

### Metrics Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `METRICS_ENABLED` | bool | No | `false` | Expose the Prometheus `/metrics` endpoint |

## API Endpoints

### Core Resources

The API server exposes the following endpoint groups:

- `/api/v1/auth` - Authentication (login, register, logout, 2FA)
- `/api/v1/monitors` - Monitor management
- `/api/v1/heartbeats` - Heartbeat data retrieval
- `/api/v1/notification-channels` - Notification channel configuration
- `/api/v1/status-pages` - Status page management
- `/api/v1/proxies` - Proxy configuration
- `/api/v1/settings` - System settings
- `/api/v1/stats` - Statistics and analytics
- `/api/v1/api-keys` - API key management
- `/api/v1/tags` - Monitor tagging
- `/api/v1/maintenances` - Maintenance window management
- `/api/v1/health` - Health check endpoint
- `/api/v1/push/:id` - Push monitor heartbeat receiver

### Swagger Documentation

API documentation is automatically generated and available at:
```
http://localhost:8034/swagger/index.html
```

## WebSocket Connection

The API server provides WebSocket endpoints for real-time updates:

```
ws://localhost:8034/api/v1/ws
```


## Authentication Methods

### JWT Authentication
Header: `Authorization: Bearer <token>`

Used for web application authentication.

### API Key Authentication
Header: `X-API-Key: pk_<key>`

Used for programmatic API access.

## Health Check

The API server exposes a health check endpoint:

```http
GET /api/v1/health
```

Response:
```json
{
  "status": "ok",
  "timestamp": "2024-01-01T00:00:00Z"
}
```

## Prometheus Metrics

The API server can expose a Prometheus-compatible metrics endpoint for direct scraping.
It is disabled by default; set `METRICS_ENABLED=true` to turn it on:

```http
GET /metrics
```

The collector subscribes to heartbeat and monitor events on the Redis event bus and
keeps a set of in-memory gauges up to date. Because the metrics are event-driven,
scraping costs nothing at scrape time: there are no database round-trips and no
polling of the worker. Metrics are seeded from the current state of all active
monitors at startup, so they are populated immediately rather than only after the
first heartbeat of each monitor.

The endpoint is unauthenticated (like `/health` and `/version`) and exposes monitor
names and statuses, so it is off by default. Enable it with `METRICS_ENABLED=true`
once you are happy for that information to be available on `/metrics`.

### Exposed Metrics

All per-monitor metrics are labelled with `monitor_id`, `monitor_name`, and `monitor_type`.

| Metric | Type | Description |
|--------|------|-------------|
| `peekaping_monitor_status` | gauge | Current status: `0`=down, `1`=up, `2`=pending, `3`=maintenance |
| `peekaping_monitor_up` | gauge | `1` if the monitor is up, otherwise `0` |
| `peekaping_monitor_response_seconds` | gauge | Response time of the most recent heartbeat, in seconds |
| `peekaping_monitor_last_heartbeat_timestamp_seconds` | gauge | Unix timestamp of the most recent heartbeat (useful for staleness alerts) |
| `peekaping_build_info` | gauge | Always `1`; the server version is carried in the `version` label |

Standard Go runtime and process collectors (`go_*`, `process_*`) are also exposed.

When a monitor is deleted its series are removed; when a monitor is renamed the
stale series under the old name is dropped.

### Example Prometheus scrape config

```yaml
scrape_configs:
  - job_name: peekaping
    static_configs:
      - targets: ['peekaping:8034']
```

## Related Components

- [Producer](./producer.md) - Schedules monitor health checks
- [Worker](./worker.md) - Executes health checks
- [Ingester](./ingester.md) - Processes health check results

