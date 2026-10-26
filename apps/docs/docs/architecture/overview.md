---
sidebar_position: 1
---

# Architecture Overview

Peekaping's server has two kinds of process you run: the API and the probe. A third component, migrate, sets up the database. They run in one of two modes:

- **Single mode**: one process, the `peekaping` binary, is the API and the local probe. It passes work and events between its parts in memory, with no Redis. It supports SQLite and PostgreSQL. The SQLite and PostgreSQL bundle images run it.
- **Cluster mode**: the API and the probe are separate processes. The API replicas share the database and talk to each other through Redis, and the probe instances connect to the API and run the checks. You can run several of each for scale and high availability. Helm and the SQLite and PostgreSQL Docker Compose stacks run it.

See [Deployment modes](../self-hosting/deployment-modes.md) to choose between them. The rest of this page describes cluster mode. See [ADR 0002](https://github.com/0xfurai/peekaping/blob/main/docs/adr/0002-single-process-runtime.md) for how single mode replaces Redis, and how cluster mode came to need only the API and the probe.

## System Architecture

```text
 browser ──► web / gateway ──► API replicas ◄─── Redis ───► API replicas
                                  │    ▲        (events, notifications,
                                  │    │         leader lock, duplicate window)
                       database ◄─┘    │
                                       │ probe protocol: /probe/v1/ over HTTP(S)
                                       │
              probe instances (the local probe) ── run the checks of its monitors
              probe instances (remote probes)   ── run the checks assigned to them
```

The probe instances connect out to the API. They need no database, no Redis and no inbound port.

## Component Overview

### 1. **API Server**
The HTTP API server that handles all client requests, authentication, and WebSocket connections. In cluster mode it also stores the results the probes send, and runs the jobs that only one replica may run.

**Key Responsibilities:**
- RESTful API endpoints
- User authentication and authorization
- WebSocket real-time updates
- The probe protocol: deals each probe's monitors among its instances, and takes their results
- Storing heartbeats, status change detection, notification triggering, statistics
- The push URL of push monitors
- Leader jobs: the stale-monitor watchdog, the probe offline sweep and the push monitors' checks
- OpenAPI documentation
- Brute force protection

### 2. **Probe**
The program that runs the checks. In cluster mode, probe instances check the local probe's monitors, and remote probes on other machines run the same program.

**Key Responsibilities:**
- Getting its monitors from the API
- Executing health checks (HTTP, TCP, Ping, DNS, etc.)
- Proxy support
- TLS certificate validation
- Timeout handling
- Sending the results to the API

### 3. **Migrate**
The database migration tool based on Bun.

**Key Responsibilities:**
- Schema initialization
- Schema migrations (up/down)
- Database versioning
- Multi-database support

## Communication Flow

### Health Check Flow
1. A **probe instance** asks the API for its monitors and gets its share of them. The API deals the local probe's monitors among the instances that are online.
2. The probe runs each monitor's check on the monitor's interval.
3. The probe sends the results to the API in batches.
4. The **API** replica that took the request stores each result as a heartbeat, one monitor's results at a time and in the order it got them, judges the monitor's status, and publishes events. That replica aggregates the stats of the heartbeats it stored.
5. Every API replica receives the events through Redis pub/sub and pushes updates to its connected clients via WebSocket.

A push monitor takes the same path from its push URL: the replica that took the request stores the heartbeat.

### Event-Driven Architecture
The system uses Redis pub/sub for real-time event communication:
- Monitor status changes
- Heartbeat events
- WebSocket updates

Notification events go through the `notification` queue instead, so one API server sends each notification however many replicas run.

### Leader Jobs
Some work must run once, not once for each replica. The API replica that holds the leader lock runs it:

- **The stale-monitor watchdog**, which looks for monitors that stopped being checked every minute.
- **The probe offline sweep**, which marks a probe offline when none of its instances has been heard from for `PROBE_OFFLINE_AFTER`, and reports a local probe that has monitors and no instance.
- **The push monitors' checks**, which find a push that didn't arrive in time and store it as Down.

The lock is the Redis key `peekaping:producer:leader`, valid for 10 seconds and renewed every 5. When the leader stops, it releases the lock and another replica takes over at once. When it is killed, another replica takes over within about 15 seconds.

## Data Stores

### Database (PostgreSQL / SQLite)

- **User accounts** and authentication
- **Monitor configurations**
- **Heartbeat history**
- **Notification channels**
- **Status pages**
- **Settings and preferences**
- **Probes**, their tokens (hashed) and their instances

### Redis

Redis connects the API replicas to each other. The probes don't use it.

- **Event bus** (pub/sub)
- **Notification queue** (via Asynq)
- **Leader election** (the leader lock)
- **Stale-monitor records**
- **Duplicate window** of the probe protocol: a batch of results that a probe sends again to another replica within the hour is answered as a duplicate
- **Distributed locks**, such as the claim on the hourly cleanup

## High Availability Features

### Horizontal Scaling
- Multiple **API** replicas can run behind a load balancer:
       - The replica that holds the leader lock runs the leader jobs
       - Any replica takes the probes' requests and stores the results
       - Automatic failover if the leader stops or crashes
- Multiple **probe** instances can run for the local probe, and for each remote probe:
       - The API deals a probe's monitors among its online instances
       - The others take over the monitors of an instance that stops
       - Add instances to add capacity for checks

## Supported Databases

Peekaping supports two database backends:

| Database   | Use Case                     | Features                           |
|------------|------------------------------|------------------------------------|
| PostgreSQL | Production deployments       | Full feature support, best performance |
| SQLite     | Development & small setups   | Single file, no server required    |

MongoDB storage was removed. [Move to SQLite or PostgreSQL](../self-hosting/docker-with-mongo.md#move-to-sqlite-or-postgresql) with `bun db import-mongo`.

## Technology Stack

- **Language**: Go 1.26+
- **HTTP Server and Router**: Go `net/http` and `http.ServeMux`
- **Database**: Bun ORM (SQL)
- **Queue**: Asynq (Redis-based), for notifications
- **Event Bus**: Redis Pub/Sub
- **Logging**: Zap
- **Dependency Injection**: Uber Dig
- **Validation**: Go Playground Validator

## Next Steps

Learn more about each component:
- [API Server](./api-server.md)
- [Probe](./probe.md)
- [Migrate](./migrate.md)
