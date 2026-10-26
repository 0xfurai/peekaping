---
sidebar_position: 5
---

# Upgrading to single mode

This page takes you from a SQLite or PostgreSQL bundle of release 0.0.44 to 0.0.47 to single mode. Those releases ran Peekaping's api, producer, worker and ingester processes, Redis and Caddy in one container, under supervisord. Single mode runs one process, `peekaping`, instead. See [Deployment modes](./deployment-modes.md) for the two modes.

**The SQLite bundle.** The image name, port 8383, the data volume and your environment variables stay the same. Upgrading is pulling the new image and starting it with the same volume. Migrations run at start, as before.

**The PostgreSQL bundle.** The `peekaping-bundle-postgres` image is gone: it gets no new tags, and 0.0.47 is its last. In single mode, PostgreSQL runs in a container of its own, from the official `postgres` image, and `peekaping` runs from the `peekaping-bundle-sqlite` image with `DB_TYPE=postgres`. You move your data with `pg_dump` and `pg_restore`: see [Moving from the PostgreSQL bundle](#moving-from-the-postgresql-bundle).

Helm and the Docker Compose stacks stay in cluster mode.

## Which release you run

```bash
curl -s http://localhost:8383/api/v1/version
```

The answer looks like `{"version":"0.0.47"}`. If you run 0.0.44, 0.0.45 or 0.0.46, read [the changes you pass on the way](#the-changes-you-pass-on-the-way) before you upgrade. If you run 0.0.47, you can skip that section.

## Back up

Back up before you pull the new image.

With the PostgreSQL bundle, you can first take a dump while the container still runs, as an extra copy you can read with PostgreSQL tools. [Moving from the PostgreSQL bundle](#moving-from-the-postgresql-bundle) takes a dump of its own. `postgres` is the superuser of the bundle's PostgreSQL, which needs no password for connections from inside the container. Replace `peekaping` at the end with your `DB_NAME`:

```bash
docker exec peekaping pg_dump -U postgres -h localhost -Fc peekaping > peekaping-before-upgrade.dump
```

Then stop the container, so that the files are complete, and copy the data directory. This copy is what [rolling back](#rolling-back) restores. Run the `cp` commands with `sudo` where the files belong to another user, as the PostgreSQL files do.

```bash
docker stop -t 35 peekaping
```

For the SQLite bundle, copy the directory you mount at `/app/data`:

```bash
cp -a .data/sqlite .data/sqlite-before-upgrade
```

For the PostgreSQL bundle, copy the directory you mount at `/var/lib/postgresql/data`:

```bash
cp -a .data/postgres .data/postgres-before-upgrade
```

With a named volume instead of a folder, copy the volume with a throwaway container. Replace `peekaping_data` with the volume's name:

```bash
docker run --rm -v peekaping_data:/data -v "$PWD":/backup alpine tar czf /backup/peekaping-before-upgrade.tar.gz -C /data .
```

For a SQLite backup that you can check with `sqlite3`, see [Upgrading stats storage](./upgrading-stats.md#before-upgrading).

## The changes you pass on the way

Skip this section if you run 0.0.47. From 0.0.44, 0.0.45 or 0.0.46, the upgrade also brings you the changes of 0.0.47:

- **A non-root user.** The bundle runs Peekaping as the user `peekaping` (UID and GID 1000) instead of root. `PUID` and `PGID` change the IDs. In the SQLite bundle, the container makes that user the owner of `/app/data` at each start, which changes the owner of files an older image wrote as root. Where root can't change owners, such as NFS with `root_squash`, startup logs a warning and carries on. Then set `PUID` and `PGID` to the folder's owner on the host. A Docker socket at `/var/run/docker.sock` keeps working. Ping monitors need `net.ipv4.ping_group_range` to include the user's groups, which rootful Docker with a private network namespace allows by default (rootless Docker, `userns-remap`, host networking and Podman don't). See [Bundle user and permissions](./bundle-user.md).
- **The stats storage upgrade.** A migration separates the minute, hour and day stats into three tables, rebuilds them from the heartbeats you still keep, and drops the old `stats` table. Another replaces three heartbeat indexes with one, which takes about a second for each million heartbeats. They run once, before `peekaping` starts, and can take minutes on a large database. Until they finish, the web app doesn't answer. Chart history whose heartbeats are gone can't be rebuilt, and your backup is the only copy of the old `stats` table. Read [Upgrading stats storage](./upgrading-stats.md) before you start.
- **A sign-out.** The upgrade generates new JWT signing secrets, so everyone is signed out once.

## Pull the new image and start it

This section is for the SQLite bundle. With the PostgreSQL bundle, go to [Moving from the PostgreSQL bundle](#moving-from-the-postgresql-bundle).

Use the same volume and the same environment variables as before. The commands below show the defaults from the setup pages: replace every `-e` value and the volume path with your own. If you don't have them written down, read them from the old container before you remove it:

```bash
docker inspect peekaping --format '{{range .Config.Env}}{{println .}}{{end}}{{range .Mounts}}{{.Source}} -> {{.Destination}}{{println}}{{end}}'
```

If you pinned a tag such as `0.0.47`, use the tag of the single-mode release in place of `latest`:

```bash
docker rm peekaping
docker pull 0xfurai/peekaping-bundle-sqlite:latest
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=/app/data/peekaping.db \
  -v $(pwd)/.data/sqlite:/app/data \
  --name peekaping \
  0xfurai/peekaping-bundle-sqlite:latest
```

Keep any other flags you started the old container with, such as `-e CLIENT_URL`, `-e PUID` or the Docker socket mount. Drop the Caddyfile mount if you had one: see below.

With Docker Compose, change the image tag, add `stop_grace_period: 35s`, and run `docker compose pull` and `docker compose up -d`. [Docker + SQLite](./docker-with-sqlite.md#single-mode) shows the whole service.

## Moving from the PostgreSQL bundle

The PostgreSQL bundle ran its own PostgreSQL 17 in the same container. Single mode runs it in a container of its own, so you copy the database from one to the other with `pg_dump` and `pg_restore`. Don't start the official `postgres` image on the bundle's data folder: its files belong to the bundle's `postgres` user (UID 102), and they were written on an older Debian release than the official image runs, so the image can't use them as they are. A dump and a restore avoid both.

The bundle's PostgreSQL listens on `localhost` only, and its `pg_hba.conf` accepts connections from the container itself only (the local socket, `127.0.0.1` and `::1`), so no other container can reach it. You dump from inside it with `docker exec`, and nothing needs to change in the bundle to do that.

The bundle used your `DB_USER` as the database role (it is required) and your `DB_NAME` as the database (`peekaping` by default), and the new stack uses the same two. The commands below use `peekaping` for both: replace them with your values, which `docker inspect` on the old container shows (see [Pull the new image and start it](#pull-the-new-image-and-start-it)). The new stack's password, `DB_PASS`, can be a new one: the bundle never asked for it, because it trusts connections from inside the container.

Back up first, as in [Back up](#back-up). The commands use the container name `peekaping`. If you started the bundle with Docker Compose, use its container name (`peekaping-bundle-postgres` in the repository's file) and replace its Compose file with the new one in step 3. Then:

1. **Dump the database** while the bundle runs, and stop the bundle right after, so that you lose as few heartbeats as possible. The `docker update` keeps Docker from starting the old container again after a restart of Docker or the host, which would run it against a data folder you are about to move:

   ```bash
   docker exec peekaping pg_dump -U peekaping -Fc peekaping > peekaping.dump
   docker update --restart=no peekaping
   docker stop -t 35 peekaping
   ```

2. **Move the bundle's data folder aside.** The new PostgreSQL needs an empty folder. Use `sudo` where the files belong to another user:

   ```bash
   sudo mv .data/postgres .data/postgres-bundle
   ```

3. **Set up the new stack.** Take the Compose file from [Docker + PostgreSQL](./docker-with-postgres.md#single-mode) and a `.env` with the role and database of the bundle:

   ```
   DB_USER=peekaping
   DB_PASS=secure_test_password_123
   DB_NAME=peekaping
   ```

4. **Start PostgreSQL alone.** If `peekaping` starts first, it creates its tables in the empty database, and the restore stops with `already exists` errors. `--wait` returns once PostgreSQL is healthy:

   ```bash
   docker compose up -d --wait postgres
   ```

5. **Restore the dump** into it:

   ```bash
   docker compose exec -T postgres pg_restore -U peekaping -d peekaping --no-owner --no-privileges < peekaping.dump
   ```

   The role `peekaping` owns the restored tables, because it is the user the container's PostgreSQL was created with. It prints nothing when it works.

6. **Start Peekaping.**

   ```bash
   docker compose up -d
   ```

   `peekaping` waits for PostgreSQL, then applies the migrations that your release doesn't have yet: from 0.0.47, the log shows `migrated to group #2`. Check the result as under [Check that it worked](#check-that-it-worked), reading the log with `docker compose logs peekaping`. Your account, monitors and history are there.

Keep the stopped `peekaping` container, `.data/postgres-bundle` and `peekaping.dump` until you are sure you don't need them. To go back, run `docker compose down`, then `sudo rm -rf .data/postgres` and `sudo mv .data/postgres-bundle .data/postgres`, and `docker update --restart=always peekaping` and `docker start peekaping`. What Peekaping recorded after the move stays in the new database only.

When you don't need them, remove them: `docker rm peekaping`, `sudo rm -rf .data/postgres-bundle` and `rm peekaping.dump`.

## What changes

Redis, Caddy and supervisord are gone. That changes these things:

- **Redis and its data.** Redis held only the schedule, the queued checks and the stale-monitor records. `peekaping` rebuilds the schedule from the database at start, and a check that was running is run again. The stale-monitor records start empty, so a monitor that is still stale is found again and notified again. The `REDIS_*` variables are ignored, and the log says so once if any is set. You can remove them.
- **A custom Caddyfile.** A Caddyfile mounted at `/etc/caddy/Caddyfile` no longer works, because the image has no Caddy. Put your own reverse proxy (Caddy, nginx or Traefik) in front of port 8383. The proxy must pass websocket connections on `/socket.io/`. See [Reverse proxy and HTTPS](./docker-with-sqlite.md#reverse-proxy-and-https).
- **The client's address behind a proxy.** The login lockout counts failed logins per client address. The Caddy inside the image used to set `X-Forwarded-For`, and the lockout believed that header from any client. Now it believes `X-Forwarded-For` and `X-Real-IP` only from the addresses in `TRUSTED_PROXIES`, which the single-mode image leaves empty, so every request counts as coming from the address it connected from. Behind your own proxy, make the proxy put the client's address last in `X-Forwarded-For` (in nginx, `proxy_set_header X-Forwarded-For $remote_addr;`, because plain `proxy_pass` forwards the client's own header), set `TRUSTED_PROXIES` to the address the proxy connects from (and pass it into the container, as the examples on that page do), and publish port 8383 on loopback (`-p 127.0.0.1:8383:8383`) when the proxy runs on the host. Until you do, every login counts as coming from the proxy. See [Reverse proxy and HTTPS](./docker-with-sqlite.md#reverse-proxy-and-https).
- **`SERVER_PORT`.** The image ignores it and always listens on 8383. A `.env` that sets it, such as `SERVER_PORT=8034`, keeps working. To use another port on the host, change the published port, for example `-p 8080:8383`.
- **`MODE`.** Your value now applies, and the default is `prod`. Before, the bundles always ran with `prod`. With `MODE=dev`, `peekaping` logs in the development format and logs every request.
- **`API_URL`.** Your value now reaches the web app. Before, the bundles always gave the web app an empty `API_URL`, which means the API on the web app's own origin. If your environment sets `API_URL`, unset it, unless the web app must call an API on another origin.
- **The 35-second stop.** `peekaping` takes up to 30 seconds to shut down cleanly. `docker stop` waits 10 seconds by default, so use `docker stop -t 35`, or `stop_grace_period: 35s` in Docker Compose.
- **Logs.** Everything logs to the container's output, so use `docker logs peekaping`. Nothing writes to `/var/log/supervisor`, so you can drop a volume mounted there.
- **`/metrics`.** With `METRICS_ENABLED=true`, port 8383 serves `/metrics`. Anyone who can open the web app can read it, and it lists your monitors' names. Before, Caddy didn't forward it. If the web app is public, leave `METRICS_ENABLED` off or block `/metrics` in your proxy. See [Prometheus Metrics](../architecture/api-server.md#prometheus-metrics).

## Check that it worked

Read the log:

```bash
docker logs peekaping
```

You should see `Migrations completed successfully!`, and then the lines `Starting Peekaping in single mode` and `Serving the API`. When there is nothing to migrate, the migrations print `there are no new migrations to run (database is up to date)`. If you see `ERROR: Migrations failed`, the container exits, and with `--restart=always` it keeps restarting. Stop it, keep the log, and roll back as described below.

Then check these:

- `docker ps` shows the container as `healthy`.
- `curl -s http://localhost:8383/api/v1/health` answers `{"status":"success","stale_monitors":0,"watchdog_ran_at":null}`. `watchdog_ran_at` stays `null` until the watchdog, which looks for stale monitors every minute, has run once.
- `http://localhost:8383` loads, and your usual account signs in.
- Your monitors are all there with their history, and their latest heartbeats are recent.

## Rolling back

The single-mode release adds no database migrations, so the database it leaves works with 0.0.47 as it is. What you do depends on the release you came from.

**From 0.0.47.** You don't need to restore the backup. Stop the container and start the 0.0.47 image with the same volume:

```bash
docker stop -t 35 peekaping
docker rm peekaping
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=/app/data/peekaping.db \
  -v $(pwd)/.data/sqlite:/app/data \
  --name peekaping \
  0xfurai/peekaping-bundle-sqlite:0.0.47
```

For the PostgreSQL bundle, you moved to a stack of two containers, so go back as [Moving from the PostgreSQL bundle](#moving-from-the-postgresql-bundle) ends: the old container and its data folder are still there. Put back your Caddyfile mount, if you had one. Heartbeats recorded in the new database aren't in the old one.

**From 0.0.44, 0.0.45 or 0.0.46.** The upgrade also ran the stats migration of 0.0.47. It dropped the old `stats` table, which those releases still use, and rolling it back recreates an empty table instead of restoring your chart history. So restore the backup. Everything recorded after the backup is lost.

```bash
docker stop -t 35 peekaping
docker rm peekaping
mv .data/sqlite .data/sqlite-after-upgrade
cp -a .data/sqlite-before-upgrade .data/sqlite
```

For the PostgreSQL bundle, swap the PostgreSQL directories instead:

```bash
docker stop -t 35 peekaping
docker rm peekaping
sudo mv .data/postgres .data/postgres-after-upgrade
sudo cp -a .data/postgres-before-upgrade .data/postgres
```

With a named volume, empty it and unpack the archive from [Back up](#back-up) into it with a throwaway container. Replace `peekaping_data` with the volume's name:

```bash
docker run --rm -v peekaping_data:/data -v "$PWD":/backup alpine sh -c 'find /data -mindepth 1 -delete && tar xzf /backup/peekaping-before-upgrade.tar.gz -C /data'
```

Then start the release you came from with the same volume and your own variables. For the SQLite bundle, use the `docker run` command above with, for example, `0xfurai/peekaping-bundle-sqlite:0.0.46`. For the PostgreSQL bundle:

```bash
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=peekaping \
  -e DB_USER=peekaping \
  -e DB_PASS=secure_test_password_123 \
  -v $(pwd)/.data/postgres:/var/lib/postgresql/data \
  --name peekaping \
  0xfurai/peekaping-bundle-postgres:0.0.46
```

Restore the directory copy, not the `pg_dump`. The old bundle starts Peekaping's processes as soon as PostgreSQL is up, so a `pg_restore` into it would run while they use the database. Keep the dump as an extra copy.
