---
sidebar_position: 7
---

# Recovering a corrupt SQLite database

A SQLite database is corrupt when its file, `peekaping.db`, is damaged. Causes include disk and filesystem errors, a copy of the file taken while Peekaping was writing to it, and a crash or power loss on a disk or filesystem that doesn't honor `fsync` (the call that forces a write onto the disk).

Peekaping checks the file each time it starts, unless you turn the check off. If the check finds corruption, Peekaping stops. It never repairs the file and never overwrites it. You restore or rebuild the database yourself, with the steps below.

This page is for the SQLite storage backend, the database Peekaping keeps its data in. For a PostgreSQL database, use PostgreSQL's own tools.

## The startup check

At start, Peekaping checks the database. `SQLITE_STARTUP_CHECK` chooses the check. In single mode, `peekaping` runs it. In cluster mode, each `api` container runs it. The `migrate` image doesn't.

| `SQLITE_STARTUP_CHECK` | What runs |
| --- | --- |
| `quick` (default) | `PRAGMA quick_check` |
| `full` | `PRAGMA integrity_check` |
| `off` | Nothing |

The values are lower case. Any other value fails the configuration check, and Peekaping doesn't start. [Startup check](./docker-with-sqlite.md#startup-check) shows how to set it.

The quick check walks the b-tree pages (the pages that hold tables and indexes) and takes about 2 seconds for a 1.5 GB database. It finds malformed pages, bad free page lists and wrong page counts. It doesn't find an index that disagrees with its table. The full check, `PRAGMA integrity_check`, finds that too, but it reads the whole file and takes about 30 seconds for 1.5 GB, so it isn't the default. Set `SQLITE_STARTUP_CHECK=full` to run it at every start, for example on a database you suspect. In cluster mode, all API replicas then read the file at once. To run it only when you choose, see [Running the full check by hand](#running-the-full-check-by-hand).

With `off`, Peekaping runs no check. It logs `SQLite startup check skipped: SQLITE_STARTUP_CHECK is off` and starts, even on a damaged file, and then writes to the file, which can make the damage worse. Use it only when you have checked the file another way, for example on a copy of it.

When the check reports anything but `ok`, Peekaping logs three kinds of lines at error level. The path is the database path inside the container:

```text
SQLite quick check found corruption in /app/data/peekaping.db. Peekaping stops here and doesn't change the file.
quick_check: *** in database main ***
Tree 2 page 4: btreeInitPage() returns error code 11
Stop every Peekaping process that uses this database, and its container's restarts. Keep the original database file together with its -wal and -shm files; don't repair it in place. Restore a tested backup. Without one, salvage the data into a new database with sqlite3 .recover and run PRAGMA integrity_check on the result; recovered data can be missing, altered or resurrected. See https://docs.peekaping.com/self-hosting/sqlite-recovery
```

With `full`, the first line says `SQLite full check` instead of `SQLite quick check`, and the finding lines start with `integrity_check:` instead of `quick_check:`.

- The first line says what stopped. Peekaping stops without changing the file, and leaves the `-wal` and `-shm` files as they were.
- Each finding gets a `quick_check:` line (`integrity_check:` with `full`), up to 100 of them. SQLite reports the first finding as two lines, `*** in database main ***` and the finding, so the first `quick_check:` line continues on a second line. The findings above are an example. Yours name other pages or tables.
- The last line is the procedure. The steps below are the same procedure in full.

After the log, the process exits. The container's restart policy (`restart: always` or `unless-stopped`) starts it again, the check fails again, and the lines repeat until you replace the database.

To read the log:

```bash
# Single mode, started with docker run
docker logs peekaping

# Single mode with Docker Compose
docker compose logs peekaping

# Cluster mode with Docker Compose
docker compose logs api
```

On Kubernetes, use `kubectl logs` on the pods.

If SQLite can't run the check at all, for example because another process holds a lock on the file, Peekaping logs `Failed to run the SQLite quick check (non-fatal)` (`full check` with `full`) with the error and starts. That doesn't confirm corruption. If you suspect it, run the full check by hand.

When the check passes, the log says `SQLite quick check passed` (`SQLite full check passed` with `full`).

## If the check fails

Follow the steps in order.

### 1. Stop every Peekaping process

Stop every process that uses the database, and stop its restarts. Don't start Peekaping again until the database is replaced.

- **Single mode with `docker run`:** `docker stop -t 35 peekaping`.
- **Single mode with Docker Compose:** `docker compose down`.
- **Cluster mode with Docker Compose:** `docker compose stop api`. Run `docker compose ps` and check that no `migrate` container is running. The `probe` uses no database, so it can keep running, but only for a while. It keeps checking and holds at most 10,000 results in memory, and when that is full it drops the oldest. When the api is back, the probe sends what it holds, but the API refuses results from checks that started more than one hour ago. So an outage longer than an hour, or one that fills the buffer first, leaves a gap in the heartbeats. A probe that restarts during the outage loses the results it holds.
- **Kubernetes, with your own manifests:** stop every workload that mounts the database volume, whatever its kind: a Deployment, a StatefulSet, a Job or a plain Pod. Scale it to 0 replicas, or delete it. Then run `kubectl get pods` in that namespace and check that no pod that mounts the volume is left. (The Helm chart supports PostgreSQL only.)

### 2. Preserve the original

Before anything else, copy `peekaping.db` to a safe place together with its `peekaping.db-wal` and `peekaping.db-shm` files. The `-wal` file (the write-ahead log) holds committed changes that aren't in the main file yet. The `-shm` file is its index. After a clean shutdown they can be missing, so copy the ones that exist.

Use the path on the host, not the path in the container. For example, `/app/data/peekaping.db` in the container is `.data/sqlite/peekaping.db` with the volume in the [SQLite deployment guide](./docker-with-sqlite.md). Replace the paths below with yours, and use a folder with enough free space:

```bash
mkdir -p /path/to/safe
cp -a .data/sqlite/peekaping.db* /path/to/safe/
ls -l /path/to/safe
```

With a named volume instead of a folder, copy the volume as [Upgrading to single mode](./upgrading-to-single-mode.md#back-up) shows.

Don't run `VACUUM`, `REINDEX` or any other repair on the original, and don't open it with Peekaping. Keep this copy as it is. The steps below work on other copies.

### 3. Restore a tested backup

If you have a backup, use it. A backup is a database that was whole when it was taken. A salvaged database is a best effort.

[Upgrading stats storage](./upgrading-stats.md#before-upgrading) shows how to make a backup with `.backup`. Test the backup before you rely on it. The check must print `ok`:

```bash
sqlite3 'file:/backup/peekaping-before-upgrade.db?mode=rw' 'PRAGMA integrity_check;'
```

Go to [step 5](#5-put-the-database-in-place-and-start-peekaping) with the backup. Everything Peekaping recorded after the backup is lost: heartbeats, and monitors and settings that changed since.

### 4. Without a backup, salvage with `.recover`

`.recover` reads what it can from a damaged database and prints SQL that builds a new one. It needs a `sqlite3` build that includes `.recover`, which is 3.29.0 or newer. The builds from sqlite.org and most distribution packages include it. Check that `.recover` is listed in the output of `sqlite3 :memory: '.help'`.

Work on a second copy, not on the original or on the copy from step 2. Opening a database can write to it, and the `-wal` file is applied when it opens. Keep the `-wal` and `-shm` files beside the copy, so `.recover` reads the changes in them too:

```bash
mkdir /path/to/work
cp -a /path/to/safe/peekaping.db* /path/to/work/
cd /path/to/work
sqlite3 peekaping.db '.recover' > recovered.sql
sqlite3 recovered.db < recovered.sql
```

`recovered.sql` is the SQL that rebuilds the database. Keep it.

The first command must finish without an error. If it fails, `recovered.sql` is incomplete, and the second command can still build a database that passes the check below. Don't use that database.

`recovered.db` must not exist before you run the second command. The second command may print `defensive off`. That is normal. If `.recover` can't tell which table some rows belong to, it puts them in a table named `lost_and_found`.

Run the full check on the result. It must print `ok`. Then compare row counts with what you expect:

```bash
sqlite3 'file:/path/to/work/recovered.db?mode=rw' 'PRAGMA integrity_check;'
sqlite3 'file:/path/to/work/recovered.db?mode=rw' 'SELECT count(*) FROM monitors; SELECT count(*) FROM heartbeats;'
```

:::warning Recovered data can be missing, altered or resurrected
`.recover` rebuilds a database from damaged pages, so the result is not a copy of what you lost.

- **Missing:** rows on pages SQLite couldn't read are gone, and so is any heartbeat history on them.
- **Altered:** a row can come back partial or with wrong values.
- **Resurrected:** rows that were deleted before the damage can come back, because deleted data stays in free pages until SQLite reuses them. This includes heartbeats older than the data retention period, and deleted monitors.

Chart history can be wrong too, because the stats are recovered like any other rows and may no longer match the heartbeats.
:::

Go to [step 5](#5-put-the-database-in-place-and-start-peekaping) with `recovered.db`. Keep the damaged original from step 2.

### 5. Put the database in place and start Peekaping

Move the damaged files out of the data folder, and put the good database there as `peekaping.db`. Leave no `-wal` or `-shm` file beside it: SQLite applies a leftover `-wal` file to the database next to it, which can damage the new file.

```bash
mkdir -p /path/to/damaged
mv .data/sqlite/peekaping.db* /path/to/damaged/
cp /path/to/database-to-restore.db .data/sqlite/peekaping.db
sudo chown 1000:1000 .data/sqlite/peekaping.db    # single mode: PUID:PGID of the bundle image, 1000:1000 by default
sudo chown 65532:65532 .data/sqlite/peekaping.db  # cluster mode: the user of the cluster images
```

For `/path/to/database-to-restore.db`, use the tested backup from step 3, or `/path/to/work/recovered.db` from step 4.

Run only the `chown` line for your mode. It gives the file to the user Peekaping runs as. If you set `PUID` and `PGID`, use your values. See [Bundle user and permissions](./bundle-user.md) and [Cluster user and permissions](./cluster-user.md#the-sqlite-data-folder).

Start Peekaping again: `docker start peekaping` or `docker compose up -d`. The quick check runs again, and the log says `SQLite quick check passed`. Open the web app and check your monitors and their recent heartbeats. Keep the damaged files until you're sure you don't need them.

If the check fails again, the database you put in place is damaged too. Stop Peekaping and try another backup, or the `.recover` steps.

## Running the full check by hand

`PRAGMA integrity_check` reads the whole file, so it also finds an index that disagrees with its table, which the default startup check misses. It takes about 30 seconds for a 1.5 GB database, and longer on a cold cache or a slow disk. To have Peekaping run it at every start instead, set `SQLITE_STARTUP_CHECK=full`.

Run it with Peekaping stopped, as the file's owner and with write access to its folder. Replace the path with the path on the host, as in [step 2](#2-preserve-the-original):

```bash
sqlite3 'file:/path/to/peekaping.db?mode=rw' 'PRAGMA integrity_check;'
```

To keep Peekaping running, check a `.backup` copy instead. Use a new backup filename in an existing folder with enough free space:

```bash
sqlite3 'file:/path/to/peekaping.db?mode=rw' ".backup /backup/peekaping-check.db"
sqlite3 'file:/backup/peekaping-check.db?mode=rw' 'PRAGMA integrity_check;'
```

The check must print `ok`. [Upgrading stats storage](./upgrading-stats.md#before-upgrading) explains the `mode=rw` form and what `.backup` includes.

Anything else is a list of findings, one per line. If the full check reports findings while the quick startup check passed, treat the database as corrupt and follow [the steps above](#if-the-check-fails).
