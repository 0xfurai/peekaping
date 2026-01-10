---
sidebar_position: 2
---

# API Server

The API Server is the main entry point for all client interactions with Vigi. It provides a RESTful API, WebSocket connections for real-time updates, and serves as the authentication gateway.

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
| `SERVICE_NAME` | string | Yes | `vigi:api` | Service identifier for logging and monitoring |

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

## Related Components

- [Producer](./producer.md) - Schedules monitor health checks
- [Worker](./worker.md) - Executes health checks
- [Ingester](./ingester.md) - Processes health check results

