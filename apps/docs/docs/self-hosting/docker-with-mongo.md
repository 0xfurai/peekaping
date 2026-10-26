---
sidebar_position: 8
---

# Move off MongoDB storage

MongoDB storage was removed in this release. `DB_TYPE=mongo` and `DB_TYPE=mongodb` now stop every Peekaping binary with an error that points to this page, and no new `peekaping-bundle-mongo` image is published. 0.0.47 is the last release that ran on MongoDB. An install that keeps its data in MongoDB stays on 0.0.47 until it moves to SQLite or PostgreSQL with the `bun db import-mongo` of this release, as this guide describes, and only then starts this release. The MongoDB monitor type, which checks other MongoDB servers, is not affected.

## Move to SQLite or PostgreSQL

`bun db import-mongo` copies a MongoDB install into a new SQLite or PostgreSQL database. The `bun` tool is in the SQLite and PostgreSQL bundle images and in the migrate image (`peekaping-migrate`) of this release. It copies:

- users, with their password hashes and two-factor secrets
- settings, including the keys that sign login tokens
- API keys, proxies, monitors, notification channels, tags and maintenance windows
- status pages with their custom domains
- the links between all of these, such as a monitor's tags, channels and status pages
- heartbeats, stats, TLS information and which certificate expiry notifications were sent

The MongoDB it reads must be version 4.4 or later. The data folder that the import reads has to be opened by the same major version of MongoDB that wrote it, so step 2 starts that version, and step 1 finds it. The MongoDB bundle and the Docker Compose file ran MongoDB 7.

The new database must hold no data of its own. If it has a row in any of these tables, the command refuses it and changes nothing: users, API keys, monitors, notification channels, proxies, tags, maintenance windows, status pages, the links between them, TLS information, notification history, heartbeats and stats. The one exception is the default settings that a Peekaping writes when it first starts. A database that Peekaping has only started on is fine: the import replaces those settings with the settings of your install, the login keys included.

The copy is one transaction. If anything fails, it is rolled back and the new database keeps no data. The SQL migrations that the command applies first stay applied. That does no harm: a migrated database with no data is still a valid target, and you can run the import again.

After the move:

- Users log in with their password and two-factor code. Browsers that were signed in have to sign in once more.
- The API keys you handed out keep working.
- Badge URLs and status page URLs, including custom domains, keep working. The monitors of a status page keep their order.
- The history is there: heartbeats and stats, with the same uptime and response times.
- Monitors, tags, channels and every other row get new IDs, which are UUIDs. Badge URLs and API keys that carry an old ID are mapped to the new one. Your own scripts that put a monitor's ID in an API URL need the new ID, which `GET /api/v1/monitors` lists.

Because monitor IDs change, a few things that carry the old ID don't follow the monitor:

- **Open incidents in PagerDuty, PagerTree and Grafana OnCall.** PagerDuty's deduplication key is `Peekaping/<monitor ID>`, PagerTree's alert carries the ID in its own ID and metadata, and Grafana OnCall gets it in the `monitor_id` field. The "up" notification after the move has the new ID, so it doesn't resolve an incident that the old ID opened. Resolve those incidents by hand.
- **Webhook receivers** that key on the monitor's `id` in the payload see the new ID.
- **Links** to `/monitors/<id>` in earlier Slack, Microsoft Teams and Google Chat messages, and in PagerDuty incidents, no longer open the monitor.

The command prints what it did with every collection: how many documents it read, copied and skipped, and why. It skips only what can't be copied as it is, such as a link to a monitor that was deleted. It also lists what the SQL tables have no place for: which monitors a status page hides, and a status page's password, search engine and Google Analytics fields, which Peekaping doesn't use. On PostgreSQL it cuts text that is wider than its column, and lists that. It doesn't copy the login lockout counters (`login_state`).

Peekaping is stopped while you move, so its monitors aren't checked, and the history of that time is missing. The steps below take a few minutes; the copy itself takes seconds for a typical install.

### 1. Stop Peekaping and back up

While the container still runs, ask it which MongoDB it runs. The first line says, for example, `db version v7.0.14`, and the major version is 7:

```bash
docker exec peekaping mongod --version
```

If you already started the new release on your data and it stopped with the removal message, nothing was written: the data folder is as it was, and there is no container to ask. The MongoDB bundle ran MongoDB 7, and so did the Docker Compose file as shipped, so use 7 for `MONGO_VERSION` in step 2. If you changed the `image:` of the Compose file's `database` service, or ran Peekaping against a MongoDB of your own, use the major version that MongoDB ran: its tag, or `mongod --version` on that server.

Stop the container, so that the files are complete, and copy the data folder. The import reads the copy, and the original is your way back. Rename the stopped container, so that the new one can take the name `peekaping`.

```bash
docker stop -t 35 peekaping
docker rename peekaping peekaping-mongo-bundle
cp -a .data/mongodb .data/mongodb-backup
```

With the Docker Compose setup for MongoDB, whose MongoDB is the `database` service, ask with `docker compose exec database mongod --version`, and run `docker compose down` in its folder instead of the `docker stop` and `docker rename` lines. Then copy `.data/mongodb` the same way.

If the data is in a named volume, such as `mongodb_data:/data/db`, and not in a folder, there is no `.data/mongodb` to copy. Find the name of the volume from the container, running or stopped. It is on the line that ends in `/data/db`. The bundle's container is `peekaping`. With Docker Compose the MongoDB is the `database` service, and its container has to be asked for before `docker compose down` removes it:

```bash
# the bundle
docker inspect -f '{{range .Mounts}}{{.Name}} {{.Destination}}{{"\n"}}{{end}}' peekaping

# Docker Compose, in its folder
docker inspect -f '{{range .Mounts}}{{.Name}} {{.Destination}}{{"\n"}}{{end}}' "$(docker compose ps -q database)"
```

Then, with the container stopped, copy the contents of the volume into `.data/mongodb-backup` with a helper container, in place of the `cp` line. The rest of the steps use the copy as they are written:

```bash
mkdir -p .data/mongodb-backup
docker run --rm -v <the volume>:/from:ro -v "$PWD/.data/mongodb-backup:/to" alpine cp -a /from/. /to/
```

### 2. Start a MongoDB on the copy

The import reads from a running MongoDB. Start one on the copy, on a Docker network of its own. Use the major version from step 1: a MongoDB can't open data that is more than one major version older than itself, or any that is newer, and the same version is the safe choice. `MONGO_VERSION=7` is the version of the bundle. This MongoDB asks for no login, and only containers on that network can reach it.

```bash
MONGO_VERSION=7
docker network create peekaping-move
docker run -d --name peekaping-mongo --network peekaping-move \
  -v $(pwd)/.data/mongodb-backup:/data/db \
  mongo:$MONGO_VERSION
```

### 3. Import into a SQLite database

Run the import from the SQLite bundle image, with the folder for the new database mounted at `/app/data`. Use a new folder: if a database with users, monitors or history is in it already, the import refuses it.

```bash
docker run --rm --network peekaping-move \
  -e DB_TYPE=sqlite \
  -e DB_NAME=/app/data/peekaping.db \
  -e MONGO_URI=mongodb://peekaping-mongo:27017 \
  -e MONGO_DB=peekaping \
  -v $(pwd)/.data/sqlite:/app/data \
  --entrypoint /app/server/bun \
  0xfurai/peekaping-bundle-sqlite:latest \
  db import-mongo
```

The command reads where to import from in `MONGO_URI` and `MONGO_DB`, or in the flags `--mongo-uri` and `--mongo-db`, which win. Prefer the environment: a URI with a password then stays out of the command line and the shell history. If the URI has a password, put the `MONGO_URI=` line in a file, and pass `--env-file <that file>` instead of `-e MONGO_URI`. `MONGO_DB` is the `DB_NAME` of the old install, `peekaping` unless you changed it. If you leave it out, the command takes the database in the URI, and then `peekaping`.

It applies the SQL migrations, copies the data and prints the report, which ends like this:

```
collection                 table                         read    copied   skipped
users                      users                            1         1         0
...
heartbeat                  heartbeats                      18        18         0
stat_minutely              stat_minutely                    9         9         0
...

Copied 60 rows into the sqlite database. The login lockout counters (login_state) are not copied. Users sign in again once.
```

If it stops with an error, it wrote no data. Fix what the error says, and run it again.

The import runs as root, so the database file in `.data/sqlite` belongs to root. The bundle gives `/app/data` to the `peekaping` user when it starts. If you start it with `docker run --user`, it doesn't, so give the folder to that user first: `sudo chown -R <uid>:<gid> .data/sqlite`.

If your MongoDB asks for a login, put it in the URI: `mongodb://<user>:<password>@<host>:27017/<database>?authSource=admin`. Percent-encode the user name and password. A reserved character in them, such as `@ : / ? # [ ]`, would end the user name or the password early and break the URI. For example, the password `p@ss:word/1` is written `p%40ss%3Aword%2F1`.

With Docker Compose, the MongoDB is the `database` service, so skip step 2. Start it alone with `docker compose up -d database`. Its URI has a password, so keep it out of the command and the shell history. Put the `MONGO_URI` line in a file that only you can read, and pass the file:

```bash
(umask 077; echo 'MONGO_URI=mongodb://root:<DB_PASS>@database:27017/?authSource=admin' > mongo.env)
```

In the command above, use `--network <folder name>_appnet` and `--env-file mongo.env` in place of the `-e MONGO_URI` line. The file is read as it is, so don't quote the value in it. Docker doesn't hide an environment variable from anyone who can use Docker: `docker inspect` shows it for as long as the container exists. The file keeps the password out of your command and your shell history. The import container is removed when it ends (`--rm`), so delete the file then too.

### 4. Start the new bundle

Start the SQLite bundle on the folder the import wrote, as in [Docker + SQLite](./docker-with-sqlite.md):

```bash
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=/app/data/peekaping.db \
  -v $(pwd)/.data/sqlite:/app/data \
  --name peekaping \
  0xfurai/peekaping-bundle-sqlite:latest
```

Open `http://localhost:8383`, log in with the same email, password and two-factor code, and look at your monitors, their history and your status pages. The monitors are checked again as soon as the container is up.

Then remove what the move needed:

```bash
docker rm -f peekaping-mongo
docker network rm peekaping-move
```

Keep `.data/mongodb`, `.data/mongodb-backup` and the stopped `peekaping-mongo-bundle` container until you are sure you don't need them. To go back, remove the new container with `docker rm -f peekaping`, then run `docker rename peekaping-mongo-bundle peekaping` and `docker start peekaping`. What Peekaping recorded after the move stays in the new database only.

### Moving to the PostgreSQL bundle

The PostgreSQL bundle runs Peekaping and its PostgreSQL in one container, and Peekaping writes its default settings when it first starts. So start the bundle first, let it create its database, and run the import inside it. Do steps 1 and 2 above, then start the bundle with a new data folder, as in [Docker + PostgreSQL](./docker-with-postgres.md). It stays off the `peekaping-move` network, so that removing the network later doesn't touch it:

```bash
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=peekaping \
  -e DB_USER=peekaping \
  -e DB_PASS=secure_test_password_123 \
  -v $(pwd)/.data/postgres:/var/lib/postgresql/data \
  --name peekaping \
  0xfurai/peekaping-bundle-postgres:latest
```

When `docker ps` says the container is `healthy`, join it to the network of the MongoDB for the import. Don't open the web app or create an account before the import: the database must not hold an admin yet.

```bash
docker network connect peekaping-move peekaping
```

Then run the import inside it. The container's environment has `DB_NAME`, `DB_USER` and `DB_PASS`, so only the rest is added:

```bash
docker exec \
  -e DB_TYPE=postgres -e DB_HOST=localhost -e DB_PORT=5432 \
  -e MONGO_URI=mongodb://peekaping-mongo:27017 -e MONGO_DB=peekaping \
  peekaping /app/server/bun db import-mongo
```

The report says how many default settings of the new database were replaced. Then restart the container, so that Peekaping loads the settings it was given:

```bash
docker restart -t 35 peekaping
```

Log in as in step 4. Then remove the MongoDB, and take the bundle off the network before you remove the network:

```bash
docker rm -f peekaping-mongo
docker network disconnect peekaping-move peekaping
docker network rm peekaping-move
```

### Moving to your own PostgreSQL server

For a PostgreSQL server you run yourself, such as the `database` service of the Docker Compose setup for PostgreSQL, set the `DB_*` variables to its database and run the import from the migrate image. The database must hold no users, monitors or history. Put the server on the `peekaping-move` network of step 2 with `docker network connect peekaping-move <its container>`, so that the migrate image reaches both it and the MongoDB:

The server's password goes in a file that only you can read, not on the command line, so that it stays out of the shell history. Anyone who can use Docker can still read it with `docker inspect` while the import container exists, which is `--rm`, so delete the file when the import is done. The file is read as it is, so don't quote the values in it:

```bash
(umask 077; echo 'DB_PASS=<its password>' > postgres.env)
chmod 600 postgres.env
```

```bash
docker run --rm --network peekaping-move \
  --env-file postgres.env \
  -e DB_TYPE=postgres \
  -e DB_HOST=<its container's name> \
  -e DB_PORT=5432 \
  -e DB_USER=<its user> \
  -e DB_NAME=<its database> \
  -e MONGO_URI=mongodb://peekaping-mongo:27017 \
  -e MONGO_DB=peekaping \
  0xfurai/peekaping-migrate:latest \
  sh -c 'until nc -z "$DB_HOST" "$DB_PORT"; do sleep 1; done; exec ./bun db import-mongo'
```

The command runs in place of the image's own, `./run-migrations.sh`, which waits for the database to accept connections before it migrates. The `until` loop does the same wait, so the import also works right after you start the server. Then remove the MongoDB, take the server off the network, and remove the network:

```bash
docker rm -f peekaping-mongo
docker network disconnect peekaping-move <its container>
docker network rm peekaping-move
```

Start Peekaping on that database. With the [Docker Compose setup for PostgreSQL](./docker-with-postgres.md#cluster-mode), put `PROBE_BOOTSTRAP_TOKEN` in its `.env` first: Compose reads the whole file, so it refuses even `docker compose up -d database` without it, and the stack runs a `probe` service instead of the producer, worker and ingester. Start its `database` service alone with `docker compose up -d database` for the import, and everything else after it.
