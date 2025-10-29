---
sidebar_position: 6
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
- **Migration Locking**: Prevents concurrent migrations

## Architecture

### CLI Tool

The migrate component is a CLI tool that runs as a one-off process:
- Runs before other services start (via Docker depends_on)
- Exits after migrations are complete
- No persistent service or daemon

**Note**: MongoDB migrations are not used as MongoDB is schema-less.

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

## Key Commands

### Apply Migrations

Applies all pending migrations:

```bash
go run cmd/bun/main.go db migrate
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
- Not applicable (schema-less database)
- Schema evolution handled through application code

## Related Components

- [API Server](./api-server.md) - Requires migrated database schema
- [Producer](./producer.md) - Requires migrated database schema
- [Ingester](./ingester.md) - Requires migrated database schema
