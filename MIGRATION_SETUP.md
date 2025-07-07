# Database Migration Setup

This project includes a separate migration container that gives you full control over when database migrations run.

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

3. **Check migration status**:
   ```bash
   ./bun db status
   ```

4. **Rollback last migration**:
   ```bash
   ./bun db rollback
   ```

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

## Migration Container Details

- **Image**: Built from `apps/server/Dockerfile.migrate`
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
