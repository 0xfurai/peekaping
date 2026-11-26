# Database Migration Setup

This project includes a separate migration container that gives you full control over when database migrations run.

Before the SQL stats upgrade, read [Upgrading stats storage](apps/docs/docs/self-hosting/upgrading-stats.md). Stats are rebuilt from retained heartbeats, the mixed legacy table is dropped, and rollback does not recover its history. The guide covers backups and optional SQLite disk-space reclamation.

## How it works

1. **Migration Container**: The project uses a dedicated migration container built from `Dockerfile.migrate`
2. **Migration Tool**: Uses the bun migration tool located in `apps/server/cmd/bun/`
3. **Separate Control**: Migrations run in their own container, separate from the application server
4. **Server Dependency**: The server waits for migrations to complete successfully before starting


## Manual Migration Commands

If you need to run migrations manually outside of Docker:

1. **Build the bun migration tool**:
   ```bash
   cd apps/server
   go build -o bun ./cmd/bun
   ```

2. **Run migrations**:
   ```bash
   ./bun db migrate
   ```

   On PostgreSQL, a migrate that starts while another one runs waits for it, up to 10 minutes. Change that with `./bun db migrate --lock-timeout 30m` or `MIGRATE_LOCK_TIMEOUT=30m`. A migrate that is killed doesn't block the next one. See [Migrate](apps/docs/docs/architecture/migrate.md#migration-locking).

3. **Check migration status**:
   ```bash
   ./bun db status
   ```

4. **Rollback last migration**:
   ```bash
   ./bun db rollback
   ```

5. **Import a MongoDB install** (MongoDB storage was removed) into a SQLite or PostgreSQL database that holds no data of its own, which `DB_TYPE` and the other `DB_*` variables name:
   ```bash
   ./bun db import-mongo --mongo-uri mongodb://localhost:27017 --mongo-db peekaping
   ```
   It applies the migrations first, then copies the data in one transaction, and refuses a database that holds users, monitors or history. It replaces the default settings of a first start with the install's. See [Move to SQLite or PostgreSQL](apps/docs/docs/self-hosting/docker-with-mongo.md#move-to-sqlite-or-postgresql) for the steps and what is copied.

### Creating New Migrations

For creating new migrations, use the bun tool:

1. **Build the bun tool**:
   ```bash
   cd apps/server
   go build -o bun ./cmd/bun
   ```

2. **Create new migration**:
   ```bash
   ./bun db create_tx_sql migration_name
   ```

## Migration Files

Migrations are stored in `apps/server/cmd/bun/migrations/` and follow the naming convention:
- `YYYYMMDDHHMMSS_description.tx.up.sql` - Migration to apply
- `YYYYMMDDHHMMSS_description.tx.down.sql` - Migration to rollback
- `YYYYMMDDHHMMSS_description.go` - Go migration (`./bun db create_go`), for data changes that should behave the same on every database, such as rebuilding stats from heartbeats

## Migration Container Details

- **Image**: Built from `apps/server/infra/Dockerfile.migrate`
- **Tool**: Uses bun migration tool (`./bun db migrate`)
- **Restart Policy**: `no` (runs once and exits)
- **Dependencies**: Waits for database to be healthy before running
- **Server Dependency**: Server waits for migration container to complete successfully

## Database Support

The migration system supports:
- PostgreSQL (recommended)
- MySQL
- SQLite

The startup script automatically detects the database type and waits appropriately.
