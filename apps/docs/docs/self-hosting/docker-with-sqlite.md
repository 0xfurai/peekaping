---
sidebar_position: 1
---

# Docker + SQLite

For the stats storage upgrade, backups and optional disk-space reclamation, see [Upgrading stats storage](./upgrading-stats.md). If Peekaping stops at start because the SQLite quick check found corruption, see [Recovering a corrupt SQLite database](./sqlite-recovery.md).

Peekaping runs in one of two modes. See [Deployment modes](./deployment-modes.md) for what each one runs:

- **[Single mode](#single-mode)**: one container runs one process, `peekaping`, with no Redis. Start here.
- **[Cluster mode](#cluster-mode)**: the API, producer, worker and ingester run in separate containers and talk through Redis. Use it when one process isn't enough, for example to run several workers.

Coming from MongoDB? [Move to SQLite or PostgreSQL](./docker-with-mongo.md#move-to-sqlite-or-postgresql) copies your install, with its history, into a new SQLite database.

## Single mode

The `peekaping-bundle-sqlite` image runs Peekaping in single mode. One process, `peekaping`, serves the web app, the API and the websocket on port 8383. It keeps its database in `/app/data/peekaping.db`, so keep `/app/data` on a volume.

```bash
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=/app/data/peekaping.db \
  -e TRUSTED_PROXIES \
  -v $(pwd)/.data/sqlite:/app/data \
  --name peekaping \
  0xfurai/peekaping-bundle-sqlite:latest
```

Or with Docker Compose:

```yaml
services:
  peekaping:
    image: 0xfurai/peekaping-bundle-sqlite:latest
    restart: unless-stopped
    # peekaping takes up to 30 seconds to shut down cleanly
    stop_grace_period: 35s
    ports:
      - "8383:8383"
    environment:
      # Behind a reverse proxy, set TRUSTED_PROXIES in .env (see Reverse proxy and HTTPS)
      - TRUSTED_PROXIES=${TRUSTED_PROXIES-}
    volumes:
      - ./.data/sqlite:/app/data
```

Then open `http://localhost:8383` and create your admin account.

`peekaping` runs as a non-root user. See [Bundle user and permissions](./bundle-user.md) to set `PUID`/`PGID`, to start the container with `--user`, and for what Docker and ping monitors need.

### Settings

The image reads the same environment variables as cluster mode (see the [`.env` file](#env-file) below), with these exceptions:

- `peekaping` always listens on port 8383 in the container. The image ignores `SERVER_PORT`, so a `.env` with `SERVER_PORT=8034` works unchanged. To use another port on the host, change the published port, for example `-p 8080:8383`.
- The database is always `/app/data/peekaping.db`, whatever `DB_TYPE` and `DB_NAME` say.
- There is no Redis, so `REDIS_*` variables are ignored.
- `MODE` defaults to `prod`.
- Leave `API_URL` unset, so the web app calls the API on its own origin. Set it only if the web app must call an API on another origin.

### Stopping

When it stops, `peekaping` finishes open requests, stops its checks and closes the database. That can take up to 30 seconds, but `docker stop` waits only 10 seconds before it kills the container. Give it more time:

```bash
docker stop -t 35 peekaping
```

In Docker Compose, set `stop_grace_period: 35s`, as above.

### Reverse proxy and HTTPS

The image serves plain HTTP on port 8383. For HTTPS or your own domain, put a reverse proxy in front of it, such as Caddy, nginx or Traefik. The proxy must pass websocket connections on `/socket.io/`. With Caddy on the same host, this is enough:

```
status.example.com {
    reverse_proxy localhost:8383
}
```

Set `CLIENT_URL` to the address people open, such as `https://status.example.com`, so links in notifications point there.

The login lockout counts failed logins per client address. A client can send any `X-Forwarded-For` header it likes, so Peekaping believes that header (and `X-Real-IP`, when there is no `X-Forwarded-For`) only on requests from an address listed in `TRUSTED_PROXIES`. The list is comma-separated IP addresses and CIDR ranges. The setting is empty unless you set it, as in this image, and every request then counts as coming from the address it connected from. (The cluster Compose files and the Helm chart set loopback and the private ranges when it is unset, and an explicitly empty value there trusts nobody.) Behind a reverse proxy, set it up like this:

1. Make the proxy put the client's address last in `X-Forwarded-For`. Caddy and Traefik do: they drop what the client sent and set their own. Plain nginx `proxy_pass` doesn't. It forwards the client's own `X-Forwarded-For` unchanged, and only `$proxy_add_x_forwarded_for` appends to it. So in nginx, add this next to `proxy_pass`:

   ```
   proxy_set_header X-Forwarded-For $remote_addr;
   ```

2. Set `TRUSTED_PROXIES` to the address the proxy connects from, as the container sees it. In the Compose example above, the `environment` line reads it from your `.env` file: Compose uses `.env` to fill in the file, but doesn't pass its variables to the container by itself. With `docker run`, `-e TRUSTED_PROXIES` passes on the variable from your shell, or give it a value: `-e TRUSTED_PROXIES=<address>`. List only proxies you run, because a listed address can claim to be anyone. Peekaping takes the last address in `X-Forwarded-For` that isn't in `TRUSTED_PROXIES`, so addresses a client adds to the left don't count.

3. Don't let clients reach port 8383 past the proxy. When the proxy runs on the same host, publish the port on loopback only: `-p 127.0.0.1:8383:8383`, or `"127.0.0.1:8383:8383"` in Docker Compose. Then only the host can connect, and the proxy's address is usually the gateway of the container's Docker network. A Compose project has its own network, so read the gateway from the container itself (replace `peekaping` with your container's name):

   ```bash
   docker inspect -f '{{range $name, $net := .NetworkSettings.Networks}}{{$name}} {{$net.Gateway}}{{println}}{{end}}' peekaping
   ```

   It prints each network the container is on with its gateway. Use the gateway of the network your reverse proxy shares with the container. On Docker's default network that is usually `172.17.0.1`:

   ```
   TRUSTED_PROXIES=172.17.0.1
   ```

Don't trust the gateway address while port 8383 is published on all interfaces (`-p 8383:8383`). On Docker Desktop, in rootless Docker and with the userland proxy, connections made straight to the published port arrive from the gateway address too, so anyone could send their own `X-Forwarded-For` and be believed.

When another proxy sits in front of yours, for example Cloudflare in front of Traefik, your proxy sees that proxy's address, not the client's. Make your proxy trust the front proxy's addresses, so that it keeps the client's address in `X-Forwarded-For`: in Traefik, `forwardedHeaders.trustedIPs`; in Caddy, `trusted_proxies`; in nginx, the real IP module, so that `$remote_addr` is the client's address and the `proxy_set_header X-Forwarded-For $remote_addr;` from step 1 passes it on:

```
set_real_ip_from <the front proxy's address ranges>;
real_ip_header X-Forwarded-For;
real_ip_recursive on;
```

Keep only your own proxy in `TRUSTED_PROXIES`, not the front proxy.

With `TRUSTED_PROXIES` empty behind a proxy, every login counts as coming from the proxy, so all clients behind it share the per-address counter and one person's failed logins can lock an account for everyone behind the proxy. The per-account counter below is account-wide whatever this setting is.

Whatever you set, Peekaping also locks an account after `BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS` failed logins (100 by default) from any addresses within `BRUTEFORCE_ACCOUNT_WINDOW` (10 minutes). A lock lasts `BRUTEFORCE_ACCOUNT_LOCKOUT` (1 minute). The count of failures is kept until the window, measured from the first failure, has passed, so until then each further failure after a lock ends locks the account again. So guessing is bounded to the limit per window plus one attempt each time a lock ends, and after an attack stops the owner is back in within the window at most. An attacker who keeps going can keep the account locked. [API Server](../architecture/api-server.md#security-configuration) lists the lockout settings.

### Upgrading from a bundle with Redis and Caddy

If you run a bundle from 0.0.44 to 0.0.47, which ran Redis and Caddy inside the container, see [Upgrading to single mode](./upgrading-to-single-mode.md).

## Cluster mode

In cluster mode, the API, producer, worker and ingester run in separate containers and talk through Redis. An nginx gateway serves the web app and the API on port 8383.

The images run as a non-root user. See [Cluster user and permissions](./cluster-user.md) for the SQLite data folder, Docker monitors and ping monitors.

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
DB_NAME=/app/data/peekaping.db
DB_TYPE=sqlite

# Server Configuration
# The API's port inside the Docker network. The single-mode image ignores it
# and always listens on 8383.
SERVER_PORT=8034
CLIENT_URL="http://localhost:8383"

# Application Settings
MODE=prod
TZ="America/New_York"

# Usage statistics and error reports are on until an admin turns them off in
# Settings. To switch them off whatever the settings say, uncomment the ones you
# want. See "Usage statistics and error reporting".
# TELEMETRY=off
# ERROR_REPORTING=off

# JWT settings are automatically managed in the database
# Default settings are initialized on first startup:
# - Access token expiration: 15 minutes
# - Refresh token expiration: 720 hours (30 days)
# - Secret keys are automatically generated securely
```

`TELEMETRY=off` and `ERROR_REPORTING=off` are optional. The first switches off the anonymous [usage statistics](./telemetry.md) and the second switches off the [error reports](./telemetry.md#error-reporting), whatever an admin chose. Without them, both are on until an admin turns them off in Settings.

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

<!-- snippet: docker-compose.prod.sqlite.yml -->
```yml
networks:
  appnet:

services:
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

  # The Peekaping images run as user 65532. This one-shot service makes the
  # data folder theirs before the migration starts: Docker creates a missing
  # ./.data/sqlite as root, and a database from an earlier version belongs to root.
  data-permissions:
    image: alpine
    restart: "no"
    command: ["chown", "-R", "65532:65532", "/app/data"]
    volumes:
      - ./.data/sqlite:/app/data

  migrate:
    image: 0xfurai/peekaping-migrate:latest
    restart: "no"
    env_file:
      - .env
    volumes:
      - ./.data/sqlite:/app/data
    depends_on:
      data-permissions:
        condition: service_completed_successfully

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
    volumes:
      - ./.data/sqlite:/app/data
    depends_on:
      redis:
        condition: service_started
      migrate:
        condition: service_completed_successfully
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
    volumes:
      - ./.data/sqlite:/app/data
    depends_on:
      redis:
        condition: service_healthy
      migrate:
        condition: service_completed_successfully
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
    volumes:
      - ./.data/sqlite:/app/data
    depends_on:
      redis:
        condition: service_started
      migrate:
        condition: service_completed_successfully
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

Single mode:

- **Bundle**: [`0xfurai/peekaping-bundle-sqlite`](https://hub.docker.com/r/0xfurai/peekaping-bundle-sqlite)

Cluster mode:

- **API**: [`0xfurai/peekaping-api`](https://hub.docker.com/r/0xfurai/peekaping-api)
- **Producer**: [`0xfurai/peekaping-producer`](https://hub.docker.com/r/0xfurai/peekaping-producer)
- **Worker**: [`0xfurai/peekaping-worker`](https://hub.docker.com/r/0xfurai/peekaping-worker)
- **Ingester**: [`0xfurai/peekaping-ingester`](https://hub.docker.com/r/0xfurai/peekaping-ingester)
- **Web**: [`0xfurai/peekaping-web`](https://hub.docker.com/r/0xfurai/peekaping-web)
- **Migrate**: [`0xfurai/peekaping-migrate`](https://hub.docker.com/r/0xfurai/peekaping-migrate), which applies the database migrations. The API, producer and ingester wait for it to finish

### Image Tags

- `latest` - Latest stable release
- `x.x.x` - Specific version tags

## Persistent Data

Peekaping stores data in SQLite. The docker-compose setup uses a local folder mount `./.data/sqlite:/app/data` to persist your monitoring data.

### Storage Options

You have two options for persistent storage:

1. **Local folder mount** (recommended):
   ```yaml
   volumes:
     - ./.data/sqlite:/app/data
   ```
   This creates a `.data/sqlite` folder in your project directory.

2. **Named volume**:
   ```yaml
   volumes:
     - sqlite_data:/app/data
   ```
   Then add at the bottom of your docker-compose.yml:
   ```yaml
   volumes:
     sqlite_data:
   ```
   In cluster mode, also mount `sqlite_data:/app/data` in the `data-permissions` service. See [Cluster user and permissions](./cluster-user.md#the-sqlite-data-folder).


### Updating Peekaping

```bash
# Pull latest images
docker compose pull

# Restart with new images
docker compose up -d

# Clean up old images
docker image prune
```
