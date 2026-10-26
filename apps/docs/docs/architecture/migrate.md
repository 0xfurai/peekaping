---
sidebar_position: 4
---

# Migrate

The Migrate component is a database migration tool built on top of [Bun](https://bun.uptrace.dev/), responsible for managing database schema versions and applying migrations across different database systems.

## Role & Responsibilities

The Migrate component handles:

- **Schema Initialization**: Creates initial database schema from scratch
- **Schema Migrations**: Applies incremental schema changes (up migrations)
- **Rollback Support**: Reverts schema changes (down migrations)
- **Multi-Database Support**: Works with PostgreSQL, MySQL, and SQLite
- **Migration Tracking**: Maintains migration history and status
- **Transactional Migrations**: Ensures atomic schema changes
- **Migration Locking**: Keeps two migrations from changing the database at once, without leaving a lock behind when a migration is killed

## Architecture

### CLI Tool

The migrate component is a CLI tool that runs as a one-off process:
- Runs before other services start (via Docker depends_on)
- Exits after migrations are complete
- No persistent service or daemon

**Note**: MongoDB storage was removed. With `DB_TYPE=mongo` or `DB_TYPE=mongodb`, the migrate image and every `bun db` command stop with an error that points to [the guide for moving to SQLite or PostgreSQL](../self-hosting/docker-with-mongo.md#move-to-sqlite-or-postgresql).

### Migration Locking

`migrate` and `rollback` take a lock while they change the database. What the lock is depends on the database:

| Database | Lock | When a migration is killed |
|----------|------|----------------------------|
| PostgreSQL | A session advisory lock, held on a connection of its own | PostgreSQL drops the lock when the connection closes, so the next migration starts at once |
| SQLite | None. One process migrates at startup | Nothing is left behind |
| MySQL | A row in the `bun_migration_locks` table | The row stays. `bun db unlock` clears it |

A migration that starts while another one holds the PostgreSQL lock waits for it, up to `--lock-timeout` (default 10 minutes), then applies what is left, usually nothing. If the lock is still held when the time is up, it fails with `another migration still holds the database lock`. A killed migration, whether by `docker stop`, an out-of-memory kill or a lost node, doesn't block the next one.

On PostgreSQL and SQLite, `bun db lock` no longer holds back `migrate` or `rollback`: they don't read `bun_migration_locks`, and they ignore a row that an older version left there. Don't use `bun db lock` to freeze migrations during maintenance on those databases; stop the migrate yourself instead (for example, don't start the migrate container or Job). `bun db lock` and `bun db unlock` still add and remove the row, and the row only matters for MySQL. On PostgreSQL and SQLite you no longer need `bun db unlock` to clear a leftover row.

### Migration Files

Migrations are stored in `apps/server/cmd/bun/migrations/` with naming convention:

```
YYYYMMDDHHMMSS_description.tx.{up|down}.sql
```

- **YYYYMMDDHHMMSS**: Timestamp (ensures ordered execution)
- **description**: Human-readable description (snake_case)
- **tx**: Transactional migration (wrapped in BEGIN/COMMIT)
- **up/down**: Migration direction

## Environment Variables

### Database Configuration

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `DB_TYPE` | string | Yes | - | Database type: `postgres`, `mysql`, `sqlite` |
| `DB_HOST` | string | Conditional | - | Database host (not required for SQLite) |
| `DB_PORT` | string | Conditional | - | Database port (not required for SQLite) |
| `DB_NAME` | string | Yes | - | Database name or SQLite file path |
| `DB_USER` | string | Conditional | - | Database username (not required for SQLite) |
| `DB_PASS` | string | Conditional | - | Database password (not required for SQLite) |

**Note**: No Redis or other services are required for migrations.

### Locking

| Variable | Type | Required | Default | Description |
|----------|------|----------|---------|-------------|
| `MIGRATE_LOCK_TIMEOUT` | duration | No | `10m` | How long `db migrate` and `db rollback` wait for another migration's lock on PostgreSQL, such as `30s` or `1h`. The `--lock-timeout` flag overrides it |

## Key Commands

### Apply Migrations

Applies all pending migrations:

```bash
go run cmd/bun/main.go db migrate
```

To wait longer or shorter than 10 minutes for another migration to finish on PostgreSQL:

```bash
go run cmd/bun/main.go db migrate --lock-timeout 30m
```

### Rollback Migrations

Rolls back the last migration group:

```bash
go run cmd/bun/main.go db rollback
```

### Check Migration Status

Shows current migration status:

```bash
go run cmd/bun/main.go db status
```

### Create Migration

Creates a new transactional SQL migration:

```bash
go run cmd/bun/main.go db create_tx_sql add_new_feature
```

## Docker Integration

In docker-compose files, the migrate service runs once at startup:

```yaml
migrate:
  build:
    context: ./apps/server
    dockerfile: infra/Dockerfile.migrate
  restart: "no"  # Run once and exit
  env_file:
    - .env
  depends_on:
    database:
      condition: service_healthy
  networks:
    - appnet
```

The container:
1. Builds the migrate binary
2. Copies migration files
3. Runs `db migrate` command
4. Exits after completion

## Database Support

### PostgreSQL
- Full support for all migration features
- Transactional DDL supported
- Best performance and reliability

### MySQL
- Full support for most migration features
- Some DDL statements are not transactional
- Use caution with transactional migrations

### SQLite
- Full support with some limitations
- Single writer limitation (migrations are serialized)
- WAL mode enabled for better concurrency

### MongoDB
- Removed as a storage backend. `bun db import-mongo` copies a MongoDB install into a new SQLite or PostgreSQL database: see [Move off MongoDB storage](../self-hosting/docker-with-mongo.md#move-to-sqlite-or-postgresql)

## Related Components

- [API Server](./api-server.md) - Requires migrated database schema
- [Probe](./probe.md) - Needs no database, so it doesn't wait for migrations
