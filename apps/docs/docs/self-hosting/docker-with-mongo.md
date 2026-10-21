---
sidebar_position: 7
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

The copy is one transaction. If anything fails, the new database keeps no data. The new database must hold no users, monitors or history of its own: if it does, the command refuses it and changes nothing. A database that Peekaping has only started on is fine. It holds just the default settings that the first start writes, and the import replaces them with the settings of your install, the login keys included.

After the move:

- Users log in with their password and two-factor code. Browsers that were signed in have to sign in once more.
- The API keys you handed out keep working.
- Badge URLs and status page URLs, including custom domains, keep working.
- The history is there: heartbeats and stats, with the same uptime and response times.
- Monitors, tags, channels and every other row get new IDs, which are UUIDs. Badge URLs and API keys that carry an old ID are mapped to the new one. Your own scripts that put a monitor's ID in an API URL need the new ID, which `GET /api/v1/monitors` lists.

The command prints what it did with every collection: how many documents it read, copied and skipped, and why. It skips only what can't be copied as it is, such as a link to a monitor that was deleted. It also lists what the SQL tables have no place for: the order of the monitors on a status page, which monitors it hides, and a status page's password, search engine and Google Analytics fields, which Peekaping doesn't use. It doesn't copy the login lockout counters (`login_state`).

Peekaping is stopped while you move, so its monitors aren't checked, and the history of that time is missing. The steps below take a few minutes; the copy itself takes seconds for a typical install.

### 1. Stop Peekaping and back up

Stop the container, so that the files are complete, and copy the data folder. The import reads the copy, and the original is your way back. Rename the stopped container, so that the new one can take the name `peekaping`.

```bash
docker stop -t 35 peekaping
docker rename peekaping peekaping-mongo-bundle
cp -a .data/mongodb .data/mongodb-backup
```

With the [Docker Compose](#docker-compose) setup, run `docker compose down` in its folder instead, then copy `.data/mongodb` the same way.

### 2. Start a MongoDB on the copy

The import reads from a running MongoDB. Start one on the copy, on a Docker network of its own. `mongo:7` is the MongoDB version of the bundle. This MongoDB asks for no login, and only containers on that network can reach it.

```bash
docker network create peekaping-move
docker run -d --name peekaping-mongo --network peekaping-move \
  -v $(pwd)/.data/mongodb-backup:/data/db \
  mongo:7
```

### 3. Import into a SQLite database

Run the import from the SQLite bundle image, with the folder for the new database mounted at `/app/data`. Use a new folder: if a database with users, monitors or history is in it already, the import refuses it. `--mongo-db` is the `DB_NAME` of the old install, `peekaping` unless you changed it.

```bash
docker run --rm --network peekaping-move \
  -e DB_TYPE=sqlite \
  -e DB_NAME=/app/data/peekaping.db \
  -v $(pwd)/.data/sqlite:/app/data \
  --entrypoint /app/server/bun \
  0xfurai/peekaping-bundle-sqlite:latest \
  db import-mongo --mongo-uri mongodb://peekaping-mongo:27017 --mongo-db peekaping
```

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

If your MongoDB asks for a login, put it in the URI: `mongodb://<user>:<password>@<host>:27017/<database>?authSource=admin`. The database in the URI replaces `--mongo-db`.

With Docker Compose, the MongoDB is the `database` service, so skip step 2. Start it alone with `docker compose up -d database`. In the command above, use `--network <folder name>_appnet` and `--mongo-uri 'mongodb://root:<DB_PASS>@database:27017/?authSource=admin'`, with `--mongo-db peekaping`.

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

The PostgreSQL bundle runs Peekaping and its PostgreSQL in one container, and Peekaping writes its default settings when it first starts. So start the bundle first, let it create its database, and run the import inside it. Do steps 1 and 2 above, then start the bundle on the `peekaping-move` network, with a new data folder, as in [Docker + PostgreSQL](./docker-with-postgres.md):

```bash
docker run -d --restart=always --network peekaping-move \
  -p 8383:8383 \
  -e DB_NAME=peekaping \
  -e DB_USER=peekaping \
  -e DB_PASS=secure_test_password_123 \
  -v $(pwd)/.data/postgres:/var/lib/postgresql/data \
  --name peekaping \
  0xfurai/peekaping-bundle-postgres:latest
```

When `docker ps` says the container is `healthy`, run the import inside it. Don't open the web app or create an account before the import: the database must not hold an admin yet. The container's environment has `DB_NAME`, `DB_USER` and `DB_PASS`, so only the rest is added:

```bash
docker exec \
  -e DB_TYPE=postgres -e DB_HOST=localhost -e DB_PORT=5432 \
  peekaping /app/server/bun db import-mongo \
  --mongo-uri mongodb://peekaping-mongo:27017 --mongo-db peekaping
```

The report says how many default settings of the new database were replaced. Then restart the container, so that Peekaping loads the settings it was given:

```bash
docker restart -t 35 peekaping
```

Log in as in step 4, and remove the MongoDB and the network. The container may be published on another host port after the restart, so look at `docker port peekaping` if the page doesn't open.

### Moving to your own PostgreSQL server

For a PostgreSQL server you run yourself, such as the `database` service of the Docker Compose setup for PostgreSQL, set the `DB_*` variables to its database and run the import from the migrate image. The database must hold no users, monitors or history. Put the server on the `peekaping-move` network of step 2 with `docker network connect peekaping-move <its container>`, so that the migrate image reaches both it and the MongoDB:

```bash
docker run --rm --network peekaping-move \
  -e DB_TYPE=postgres \
  -e DB_HOST=<its container's name> \
  -e DB_PORT=5432 \
  -e DB_USER=<its user> \
  -e DB_PASS=<its password> \
  -e DB_NAME=<its database> \
  0xfurai/peekaping-migrate:latest \
  ./bun db import-mongo --mongo-uri mongodb://peekaping-mongo:27017 --mongo-db peekaping
```

Then start Peekaping on that database. With the [Docker Compose setup for PostgreSQL](./docker-with-postgres.md#cluster-mode), start its `database` service alone with `docker compose up -d database` for the import, and everything else after it.

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

# Application Settings
MODE=prod
TZ="America/New_York"

# JWT settings are automatically managed in the database
# Default settings are initialized on first startup:
# - Access token expiration: 15 minutes
# - Refresh token expiration: 720 hours (30 days)
# - Secret keys are automatically generated securely
```
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

```nginx
events {}
http {
  upstream server  { server server:8034; }
  upstream web { server web:80; }

  server {
    listen 80;

    # Pure API calls
    location /api/ {
      proxy_pass         http://server;
      proxy_set_header   Host $host;
      proxy_set_header   X-Real-IP $remote_addr;
    }

    # socket.io
    location /socket.io/ {
      proxy_pass http://server;
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
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

Peekaping provides official Docker images:

- **Server**: [`0xfurai/peekaping-server`](https://hub.docker.com/r/0xfurai/peekaping-server)
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
