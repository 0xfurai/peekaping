---
sidebar_position: 6
---

# Upgrading stats storage

The SQL stats upgrade separates minute, hour and day stats into three tables and rebuilds them from retained heartbeats. This fixes mixed time scales in response time charts and uptime percentages on SQLite and PostgreSQL.

## Before upgrading

Schedule a maintenance window and stop every Peekaping process that uses the database, including every replica. For a bundle image, stop the container. In cluster mode, stop every API replica and, if your install still runs them, the producer, worker and ingester, and make sure no migration is running. The probes use no database, so they can keep running, but only for a while. A probe holds at most 10,000 results in memory and drops the oldest when that is full, and the API refuses results from checks that started more than one hour ago. A longer maintenance window leaves a gap in the heartbeats, and a probe that restarts meanwhile loses what it holds.

Back up the complete database before starting the new version or its migration container. For PostgreSQL, use your usual tested database backup and restore procedure. For SQLite, use the host's `sqlite3` CLI and the actual host path of the mounted database file. For example, `/app/data/peekaping.db` inside the container maps to `.data/sqlite/peekaping.db` with the volume in the [SQLite deployment guide](./docker-with-sqlite.md).

Replace the example paths below. Use a new backup filename in an existing directory with enough free space, and check that the command succeeds:

```bash
sqlite3 'file:/path/to/peekaping.db?mode=rw' ".backup /backup/peekaping-before-upgrade.db"
sqlite3 'file:/backup/peekaping-before-upgrade.db?mode=rw' 'PRAGMA integrity_check;'
```

The integrity check must print `ok`. The `mode=rw` URI requires an existing database and allows SQLite to initialize its write-ahead log files; run these commands as the file owner with write access to its directory. The `.backup` command includes committed data in SQLite's write-ahead log; copying just the `.db` file while it is in use can miss that data.

### How much history survives

The migration rebuilds stats from the heartbeats still in the database. History whose raw heartbeats have already expired cannot be reconstructed. The data retention period (`KEEP_DATA_PERIOD_DAYS`) controls heartbeat retention; an existing install without a saved setting uses 365 days.

After a successful rebuild, the migration drops the old `stats` table. Its rows mixed minute, hour and day counts, so they cannot provide accurate replacement buckets. Keep the pre-upgrade backup if you need to retain those original rows.

The rebuild can take minutes on a large installation. Let migrations finish successfully before restarting the remaining processes. Rolling back the stats migration recreates an empty legacy table; it does not restore the previous chart history. To return to the exact pre-upgrade database, stop Peekaping and restore the pre-upgrade backup with the matching application version. Changes made after that backup will be lost.

### Retention after the upgrade

The hourly cleanup keeps:

| Stats | Retention |
| --- | --- |
| Minute | 48 hours |
| Hour | 90 days |
| Day | Indefinitely |

Minute and hour row counts become bounded once cleanup catches up. Day stats continue to add about 365 rows per monitor each year. Uptime and badges read minute stats for windows up to 24 hours, hour stats up to 90 days, and day stats for longer windows. The first cleanup can take several minutes on an old installation.

## Reclaiming SQLite disk space

Deleting old rows frees pages for SQLite to reuse, but the database file does not shrink automatically. Reclaiming disk space is optional; future writes can reuse those pages. This procedure also reclaims space freed by heartbeat cleanup or dropped indexes.

1. After upgrading, let the hourly cleanup finish. Check the API logs for a successful `Deleted expired stats` entry. Running `VACUUM` before cleanup finishes will leave the expired rows in the file.
2. Stop every Peekaping process and replica that accesses this database again, including any migration process. For the bundle example, use `docker stop peekaping`. For Compose, stop the entire Peekaping deployment with its own Compose file. Keep it stopped until verification finishes. Checks and the API are unavailable during this maintenance.
3. Install the `sqlite3` CLI on the host if needed. Run it as the database file's owner, with write access to the containing directory. Check free disk space: SQLite may need up to twice the current database file's size in additional free space for `VACUUM`, plus space for the backup if it is on the same disk. See [SQLite's VACUUM documentation](https://www.sqlite.org/lang_vacuum.html).
4. Replace the paths below with the existing database and a new backup filename. Inspect the database and make a separate maintenance backup:

   ```bash
   sqlite3 'file:/path/to/peekaping.db?mode=rw' 'PRAGMA integrity_check; PRAGMA page_count; PRAGMA freelist_count;'
   sqlite3 'file:/path/to/peekaping.db?mode=rw' ".backup /backup/peekaping-before-vacuum.db"
   sqlite3 'file:/backup/peekaping-before-vacuum.db?mode=rw' 'PRAGMA integrity_check;'
   ```

   Both integrity checks must print `ok`. The other two numbers are the total page count and free page count. A large free page count indicates reclaimable space. Do not proceed if an integrity check or backup fails.
5. Compact the database and checkpoint its write-ahead log:

   ```bash
   sqlite3 -bail 'file:/path/to/peekaping.db?mode=rw' 'VACUUM; PRAGMA wal_checkpoint(TRUNCATE); PRAGMA integrity_check; PRAGMA page_count; PRAGMA freelist_count;'
   ```

   This can take minutes for a multi-GB file. Let the command finish. The integrity check must print `ok`, the checkpoint's first value must be `0` (no busy connection), and the free page count should be near zero. Check the database file size after the command. If a command fails, keep Peekaping stopped and resolve the error before retrying; retain the backup.
6. Start the same deployment again (`docker start peekaping` for the bundle example), then check the API, recent heartbeats and a monitor's chart. Keep the backup until those checks pass.

`VACUUM` does not restore deleted history. It rewrites the live database into fewer pages. Do not add it to the hourly cleanup: it requires a deliberate maintenance window.
