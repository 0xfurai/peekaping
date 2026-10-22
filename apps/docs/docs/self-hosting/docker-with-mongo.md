---
sidebar_position: 8
---

# Docker + MongoDB

:::warning MongoDB storage is deprecated
MongoDB as a storage backend (the database Peekaping keeps its data in) is deprecated. Existing MongoDB installs keep working.

- **New installs** should use [SQLite](./docker-with-sqlite.md) or [PostgreSQL](./docker-with-postgres.md).
- **Existing installs** can [move to SQLite or PostgreSQL](#move-to-sqlite-or-postgresql) with `bun db import-mongo`, which copies everything into a new database. It's in the releases after 0.0.47.
- MongoDB is removed no earlier than the second release after the one that ships that command.
- Until then, MongoDB gets no new features. It gets fixes only for data loss, startup failures and security problems.

The MongoDB monitor type, which checks other MongoDB servers, is not affected.
:::

With MongoDB, Peekaping runs in cluster mode only: the API, producer, worker and ingester are separate processes that talk through Redis. Single mode, one `peekaping` process, supports SQLite and PostgreSQL only.

## Move to SQLite or PostgreSQL

`bun db import-mongo` copies a MongoDB install into a new SQLite or PostgreSQL database. The `bun` tool is in the SQLite and PostgreSQL bundle images and in the migrate image (`peekaping-migrate`) of the releases after 0.0.47. It copies:

- users, with their password hashes and two-factor secrets
- settings, including the keys that sign login tokens
- API keys, proxies, monitors, notification channels, tags and maintenance windows
- status pages with their custom domains
- the links between all of these, such as a monitor's tags, channels and status pages
- heartbeats, stats, TLS information and which certificate expiry notifications were sent

The MongoDB it reads must be version 4.4 or later. The data folder that the import reads has to be opened by the same major version of MongoDB that wrote it, so step 2 starts that version, and step 1 finds it. The bundle and the Docker Compose file of the current release run MongoDB 7.

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

Stop the container, so that the files are complete, and copy the data folder. The import reads the copy, and the original is your way back. Rename the stopped container, so that the new one can take the name `peekaping`.

```bash
docker stop -t 35 peekaping
docker rename peekaping peekaping-mongo-bundle
cp -a .data/mongodb .data/mongodb-backup
```

With the [Docker Compose](#docker-compose) setup, ask with `docker compose exec database mongod --version`, and run `docker compose down` in its folder instead of the `docker stop` and `docker rename` lines. Then copy `.data/mongodb` the same way.

If the data is in a named volume, such as `mongodb_data:/data/db` ([Persistent Data](#persistent-data)), and not in a folder, there is no `.data/mongodb` to copy. Find the name of the volume from the container, running or stopped. It is on the line that ends in `/data/db`. The bundle's container is `peekaping`. With Docker Compose the MongoDB is the `database` service, and its container has to be asked for before `docker compose down` removes it:

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

Start Peekaping on that database. With the [Docker Compose setup for PostgreSQL](./docker-with-postgres.md#cluster-mode), start its `database` service alone with `docker compose up -d database` for the import, and everything else after it.

## Bundle image

The `peekaping-bundle-mongo` image runs all of cluster mode in one container: MongoDB, Redis, the API, producer, worker and ingester processes, and Caddy, which serves the web app and the API on port 8383. supervisord starts and watches them.

```bash
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=peekaping \
  -e DB_USER=peekaping \
  -e DB_PASS=secure_test_password_123 \
  -v $(pwd)/.data/mongodb:/data/db \
  --name peekaping \
  0xfurai/peekaping-bundle-mongo:latest
```
To use your own Caddyfile, add:
```
-v ./custom-Caddyfile:/etc/caddy/Caddyfile:ro
```

To run each part in its own container, see [Docker Compose](#docker-compose).

The bundle runs Peekaping's api, producer, worker and ingester processes as a non-root user. See [Bundle user and permissions](./bundle-user.md) to set `PUID`/`PGID` and for what Docker and ping monitors need.

## Docker Compose

Here each part of cluster mode runs in its own container: MongoDB, Redis, the API, producer, worker and ingester, the web app, and an nginx gateway on port 8383.

### Prerequisites

- Docker Compose 2.0+

### 1. Create Project Structure

Create a new directory for your Peekaping installation and set up the following structure:

```
peekaping/
├── .env
├── docker-compose.yml
└── nginx.conf
```

### 2. Create Configuration Files

#### `.env` file

Create a `.env` file with your configuration:

```env
# Database Configuration
DB_USER=root
DB_PASS=your-secure-password-here
DB_NAME=peekaping
DB_HOST=database
DB_PORT=27017
DB_TYPE=mongo

# Server Configuration
SERVER_PORT=8034
CLIENT_URL="http://localhost:8383"
# Origins that may read the API's responses from another origin. Default: the origin of CLIENT_URL
# CORS_ORIGINS=https://app.example.com

# Application Settings
MODE=prod
TZ="America/New_York"

# Usage statistics and error reports are off until an admin turns them on. To
# switch them off whatever the settings say, uncomment the ones you want. See
# "Usage statistics and error reporting".
# TELEMETRY=off
# ERROR_REPORTING=off
# DO_NOT_TRACK=1

# JWT settings are automatically managed in the database
# Default settings are initialized on first startup:
# - Access token expiration: 15 minutes
# - Refresh token expiration: 720 hours (30 days)
# - Secret keys are automatically generated securely
```

`TELEMETRY=off`, `ERROR_REPORTING=off` and `DO_NOT_TRACK=1` are optional. The first switches off the anonymous [usage statistics](./telemetry.md), the second switches off the [error reports](./telemetry.md#error-reporting), and the third switches off both, whatever an admin chose. Without them, nothing is sent until an admin turns it on.

:::info JWT Settings
JWT settings (access/refresh token expiration times and secret keys) are now automatically managed in the database. Default secure settings are initialized on first startup, and secret keys are generated automatically.
:::
:::warning Important Security Notes
- **Change all default passwords and secret keys**
- Use strong, unique passwords for the database
- Consider using environment-specific secrets management
:::

#### `docker-compose.yml` file

Create a `docker-compose.yml` file:

<!-- snippet: docker-compose.prod.mongo.yml -->
```yml
networks:
  appnet:

services:
  database:
    image: mongo:7
    restart: unless-stopped
    env_file:
      - .env
    environment:
      MONGO_INITDB_ROOT_USERNAME: ${DB_USER}
      MONGO_INITDB_ROOT_PASSWORD: ${DB_PASS}
      MONGO_INITDB_DATABASE: ${DB_NAME}
    volumes:
      - ./.data/mongodb:/data/db
    networks:
      - appnet
    healthcheck:
      test: echo 'db.runCommand("ping").ok' | mongosh localhost:27017/test --quiet
      interval: 30s
      timeout: 2s
      retries: 5
      start_period: 5s

  redis:
    image: redis:7
    restart: unless-stopped
    networks:
      - appnet
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 30s
      timeout: 2s
      retries: 5
      start_period: 5s

  api:
    image: 0xfurai/peekaping-api:latest
    restart: unless-stopped
    env_file:
      - .env
    environment:
      # The api isn't published: only the gateway and the other services on this network reach
      # it, all from private addresses. The gateway (nginx.conf) sets X-Forwarded-For to the
      # client's address, and the api believes that header from these addresses only. Set
      # TRUSTED_PROXIES in .env to change them; an empty TRUSTED_PROXIES= trusts nobody.
      TRUSTED_PROXIES: "${TRUSTED_PROXIES-127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7}"
    depends_on:
      database:
        condition: service_healthy
      redis:
        condition: service_healthy
    networks:
      - appnet
    healthcheck:
      test:
        [
          "CMD-SHELL",
          "wget -qO - http://localhost:8034/api/v1/health || exit 1",
        ]
      interval: 30s
      timeout: 2s
      retries: 5
      start_period: 5s

  producer:
    image: 0xfurai/peekaping-producer:latest
    restart: unless-stopped
    env_file:
      - .env
    depends_on:
      database:
        condition: service_healthy
      redis:
        condition: service_healthy
    networks:
      - appnet

  worker:
    image: 0xfurai/peekaping-worker:latest
    restart: unless-stopped
    env_file:
      - .env
    depends_on:
      redis:
        condition: service_healthy
    networks:
      - appnet

  ingester:
    image: 0xfurai/peekaping-ingester:latest
    restart: unless-stopped
    env_file:
      - .env
    depends_on:
      database:
        condition: service_healthy
      redis:
        condition: service_healthy
    networks:
      - appnet

  web:
    image: 0xfurai/peekaping-web:latest
    restart: unless-stopped
    depends_on:
      api:
        condition: service_healthy
    networks:
      - appnet
    healthcheck:
      test: ["CMD-SHELL", "curl -f http://localhost:80 || exit 1"]
      interval: 30s
      timeout: 2s
      retries: 5
      start_period: 5s

  gateway:
    image: nginx:latest
    restart: unless-stopped
    ports:
      - "8383:80"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
    depends_on:
      api:
        condition: service_healthy
      web:
        condition: service_healthy
    networks:
      - appnet
    healthcheck:
      test: ["CMD-SHELL", "curl -f http://localhost:80 || exit 1"]
      interval: 30s
      timeout: 2s
      retries: 5
      start_period: 5s
```


#### `nginx.conf` file

If you want to use Nginx as a reverse proxy, create this file:

<!-- snippet: nginx.conf -->
```nginx
events {}
http {
  upstream api  { server api:8034; }
  upstream web { server web:80; }

  server {
    listen 80;

    # Pure API calls
    location /api/ {
      proxy_pass         http://api;
      proxy_set_header   Host $host;
      proxy_set_header   X-Real-IP $remote_addr;
      # Replace the client's X-Forwarded-For: the api trusts this gateway's header
      proxy_set_header   X-Forwarded-For $remote_addr;
    }

    # socket.io
    location /socket.io/ {
      proxy_pass http://api;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $remote_addr;
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection "upgrade";
    }

    # Everything else → static SPA
    location / {
      proxy_pass http://web;
    }
  }
}
```

The gateway replaces the `X-Forwarded-For` header of every request with the client's address, and the `api` service believes that header from loopback and the private ranges (`TRUSTED_PROXIES` in the compose file), which cover the Docker network. Don't publish the `api` service's port: a connection to a published port reaches the container from the Docker network's gateway address, which that setting trusts, so a client could send its own `X-Forwarded-For`. The default ranges are wide: narrowing `TRUSTED_PROXIES` to the gateway's network (`docker network inspect` shows its subnet) is safer, because a client with a private address behind a gateway that appends to `X-Forwarded-For`, unlike this one, would be skipped as a trusted proxy and could pick its own address from the entries to its left. The account counter still bounds guessing either way.



### 3. Start Peekaping

```bash
# Navigate to your project directory
cd peekaping

# Start all services
docker compose up -d

# Check status
docker compose ps

# View logs
docker compose logs -f
```

### 4. Access Peekaping

Once all containers are running:

1. Open your browser and go to `http://localhost:8383`
2. Create your admin account
3. Create your first monitor!

## Docker Images

Peekaping provides official Docker images.

The bundle image runs all of cluster mode in one container:

- **Bundle**: [`0xfurai/peekaping-bundle-mongo`](https://hub.docker.com/r/0xfurai/peekaping-bundle-mongo)

Docker Compose runs one container for each part of cluster mode:

- **API**: [`0xfurai/peekaping-api`](https://hub.docker.com/r/0xfurai/peekaping-api)
- **Producer**: [`0xfurai/peekaping-producer`](https://hub.docker.com/r/0xfurai/peekaping-producer)
- **Worker**: [`0xfurai/peekaping-worker`](https://hub.docker.com/r/0xfurai/peekaping-worker)
- **Ingester**: [`0xfurai/peekaping-ingester`](https://hub.docker.com/r/0xfurai/peekaping-ingester)
- **Web**: [`0xfurai/peekaping-web`](https://hub.docker.com/r/0xfurai/peekaping-web)

### Image Tags

- `latest` - Latest stable release
- `x.x.x` - Specific version tags

## Persistent Data

Peekaping stores data in MongoDB. The docker-compose setup uses a local folder mount `./.data/mongodb:/data/db` to persist your monitoring data.

### Storage Options

You have two options for persistent storage:

1. **Local folder mount** (recommended):
   ```yaml
   volumes:
     - ./.data/mongodb:/data/db
   ```
   This creates a `.data/mongodb` folder in your project directory.

2. **Named volume**:
   ```yaml
   volumes:
     - mongodb_data:/data/db
   ```
   Then add at the bottom of your docker-compose.yml:
   ```yaml
   volumes:
     mongodb_data:
   ```


### Updating Peekaping

```bash
# Pull latest images
docker compose pull

# Restart with new images
docker compose up -d

# Clean up old images
docker image prune
```
