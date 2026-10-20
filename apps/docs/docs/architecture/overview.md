---
sidebar_position: 1
---

# Architecture Overview

Peekaping's server has four roles: the API, the producer, the worker and the ingester. A fifth component, migrate, sets up the database. The roles run in one of two modes:

- **Single mode**: one process, the `peekaping` binary, runs all four roles and passes work and events between them in memory, with no Redis. It supports SQLite and PostgreSQL. The SQLite and PostgreSQL bundle images run it.
- **Cluster mode**: each role is its own process, and they talk through Redis. You can run several of each for scale and high availability. Helm, the Docker Compose stacks and the MongoDB bundle image run it.

Both modes run the same modules for each role. The rest of this page describes cluster mode, where the parts are easiest to see. See [ADR 0002](https://github.com/0xfurai/peekaping/blob/main/docs/adr/0002-single-process-runtime.md) for how single mode replaces Redis.

## System Architecture

![Peekaping system design](/img/schema.png)

## Component Overview

### 1. **API Server**
The main HTTP API server that handles all client requests, authentication, and WebSocket connections.

**Key Responsibilities:**
- RESTful API endpoints
- User authentication and authorization
- WebSocket real-time updates
- OpenAPI documentation
- Brute force protection

### 2. **Producer**
The scheduling service that manages when monitors should be checked.

**Key Responsibilities:**
- Scheduling health checks based on monitor intervals
- Leader election for high availability
- Monitor lifecycle management

### 3. **Worker**
The execution engine that performs actual health checks.

**Key Responsibilities:**
- Executing health checks (HTTP, TCP, Ping, DNS, etc.)
- Proxy support
- TLS certificate validation
- Timeout handling

### 4. **Ingester**
The data persistence service that processes health check results.

**Key Responsibilities:**
- Storing health check results (heartbeats)
- Status change detection
- Notification triggering
- TLS certificate storage
- Statistics calculation

### 5. **Migrate**
The database migration tool based on Bun.

**Key Responsibilities:**
- Schema initialization
- Schema migrations (up/down)
- Database versioning
- Multi-database support

## Communication Flow

### Health Check Flow
1. **Producer** schedules a health check task and adds it to the queue
2. **Queue (Redis/Asynq)** holds the task until a worker is available
3. **Worker** picks up the task, executes the health check, and queues the result
4. **Ingester** processes the result, stores it in the database, and triggers events
5. **API** receives events via Redis pub/sub and pushes updates to connected clients via WebSocket

### Event-Driven Architecture
The system uses Redis pub/sub for real-time event communication:
- Monitor status changes
- Heartbeat events
- Notification events
- WebSocket updates

## Data Stores

### Database (PostgreSQL / SQLite)

MongoDB storage is [deprecated](../self-hosting/docker-with-mongo.md) but still works.

- **User accounts** and authentication
- **Monitor configurations**
- **Heartbeat history**
- **Notification channels**
- **Status pages**
- **Settings and preferences**

### Redis
- **Task queue** (via Asynq)
- **Event bus** (pub/sub)
- **Leader election** (for producer HA)
- **Distributed locks**
- **Monitors due time sorted set**

## High Availability Features

### Horizontal Scaling
- Multiple **producer** instances can run simultaneously with automatic leader election:
       - Leader handles monitor syncing and scheduling
       - All instances can process tasks
       - Automatic failover if leader crashes
- Multiple **Worker** instances can process tasks concurrently
- Multiple **Ingester** instances can process results concurrently
- Queue-based load distribution ensures even work distribution

## Supported Databases

Peekaping supports two database backends for new installs:

| Database   | Use Case                     | Features                           |
|------------|------------------------------|------------------------------------|
| PostgreSQL | Production deployments       | Full feature support, best performance |
| SQLite     | Development & small setups   | Single file, no server required    |

MongoDB storage is deprecated. Existing installs keep working, but it gets no new features and will be removed in a future release. See [Docker + MongoDB](../self-hosting/docker-with-mongo.md).

## Technology Stack

- **Language**: Go 1.26+
- **HTTP Server and Router**: Go `net/http` and `http.ServeMux`
- **Database**: Bun ORM (SQL) / MongoDB Driver (MongoDB storage is deprecated)
- **Queue**: Asynq (Redis-based)
- **Event Bus**: Redis Pub/Sub
- **Logging**: Zap
- **Dependency Injection**: Uber Dig
- **Validation**: Go Playground Validator

## Next Steps

Learn more about each component:
- [API Server](./api-server.md)
- [Producer](./producer.md)
- [Worker](./worker.md)
- [Ingester](./ingester.md)
- [Migrate](./migrate.md)

