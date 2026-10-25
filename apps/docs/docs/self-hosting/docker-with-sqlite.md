---
sidebar_position: 1
---

# Docker + SQLite

For the stats storage upgrade, backups and optional disk-space reclamation, see [Upgrading stats storage](./upgrading-stats.md). If Peekaping stops at start because the SQLite quick check found corruption, see [Recovering a corrupt SQLite database](./sqlite-recovery.md). [Startup check](#startup-check) shows how to choose the check Peekaping runs at start.

Peekaping runs in one of two modes. See [Deployment modes](./deployment-modes.md) for what each one runs:

- **[Single mode](#single-mode)**: one container runs one process, `peekaping`, with no Redis. Start here.
- **[Cluster mode](#cluster-mode)**: the API and the probe run in separate containers. The API replicas talk to each other through Redis, and the probes run the checks. Use it when one process isn't enough, for example to run several probe instances.

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
- `PROBE_BOOTSTRAP_TOKEN` is ignored too, and `peekaping` logs a warning when it is set. The `peekaping` process checks the local probe's monitors, so a `peekaping-probe` connects only as a [remote probe](./remote-probes.md), with that probe's token. To check from another machine, add a remote probe. A `peekaping-probe` that uses the local probe's token is refused.
- `MODE` defaults to `prod`.
- Leave `API_URL` unset, so the web app calls the API on its own origin. Set it only if the web app must call an API on another origin.
- Leave `CORS_ORIGINS` unset. The web app calls the API on its own origin, which needs no CORS entry. Set it to a comma-separated list of origins, such as `https://app.example.com`, only if a page on another origin must read the API's responses. Unset, it allows the origin of `CLIENT_URL`. `*` allows any origin.

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

In cluster mode, the API and the probe run in separate containers. The API replicas talk to each other through Redis. The `probe` service runs the checks of the local probe's monitors: it connects to the API, gets its monitors and sends the results back, and it needs no database, no Redis and no volume. Run more probe containers to check more monitors. An nginx gateway serves the web app and the API on port 8383.

:::note Single mode fits SQLite better
In cluster mode, the `api` containers open the same SQLite file, and SQLite lets one process write to it at a time, so replicas wait for each other's writes. That works for a small install, but [single mode](#single-mode) runs everything in one process and fits SQLite better, and [PostgreSQL](./docker-with-postgres.md) takes concurrent writers. Prefer the SQLite bundle image, unless you need separate processes for the API and the probes.

Each `api` container logs this warning at start:

```text
SQLite in cluster mode: the api replicas share one database file and write to it one at a time. Single mode (the SQLite bundle image) or PostgreSQL fits better. See https://docs.peekaping.com/self-hosting/docker-with-sqlite#cluster-mode
```

The [Helm chart](./kubernetes-helm.md) supports PostgreSQL only and refuses SQLite.
:::

The images run as a non-root user. See [Cluster user and permissions](./cluster-user.md) for the SQLite data folder, Docker monitors and ping monitors.

Moving from a compose file with `producer`, `worker` and `ingester` services? See [Upgrading to probes](./upgrading-to-probes.md).

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
# Origins that may read the API's responses from another origin. Default: the origin of CLIENT_URL
# CORS_ORIGINS=https://app.example.com

# The token of the local probe, which checks the monitors that run on Peekaping
# itself. The api stores it and the probe service sends it. Make it a random
# string of at least 32 characters, such as the output of: openssl rand -hex 32
# The compose file below refuses to start while it is empty.
PROBE_BOOTSTRAP_TOKEN=

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

`PROBE_BOOTSTRAP_TOKEN` is required: run `openssl rand -hex 32` and paste the result after the `=`. It is the token of the local probe, which gives access to the config of the monitors on it, so keep it secret. If you change it, run `docker compose up -d` again: it recreates the `api` and the `probe` with the new value. `docker compose up` stops with `PROBE_BOOTSTRAP_TOKEN is not set` while it is missing or empty.

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

Create a `docker-compose.yml` file. The `probe` service checks the local probe's monitors. To run more of them, use `docker compose up -d --scale probe=3`: the monitors are dealt among the containers.

The `probe` reaches the `api` over plain HTTP inside `appnet`, so it sets `PROBE_ALLOW_INSECURE: "true"`. Its token crosses `appnet` unencrypted. Keep `appnet` trusted and isolated: put only the services of this stack on it, and don't publish the `api` port. If the probe has to reach the API through a network you don't trust, point `PEEKAPING_URL` at an `https://` address and remove `PROBE_ALLOW_INSECURE`.

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
    # the api takes up to 30 seconds to shut down cleanly
    stop_grace_period: 35s
    env_file:
      - .env
    environment:
      # The api isn't published: only the gateway and the other services on this network reach
      # it, all from private addresses. The gateway (nginx.conf) sets X-Forwarded-For to the
      # client's address, and the api believes that header from these addresses only. Set
      # TRUSTED_PROXIES in .env to change them; an empty TRUSTED_PROXIES= trusts nobody.
      TRUSTED_PROXIES: "${TRUSTED_PROXIES-127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7}"
      # The local probe's token: the api stores it, and the probe sends it. Without it no
      # monitor is checked.
      PROBE_BOOTSTRAP_TOKEN: "${PROBE_BOOTSTRAP_TOKEN:?PROBE_BOOTSTRAP_TOKEN is not set: add it to .env as a random string of at least 32 characters, such as the output of openssl rand -hex 32}"
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

  # The local probe: its instances run the checks of the monitors that run on the local probe.
  # They connect to the api, so they need no database, no Redis and no volume. Run more of
  # them with `docker compose up -d --scale probe=3`: the monitors are dealt among them.
  probe:
    image: 0xfurai/peekaping-probe:latest
    restart: unless-stopped
    # the probe takes up to 25 seconds to shut down cleanly
    stop_grace_period: 30s
    environment:
      PEEKAPING_URL: http://api:8034
      # The probe refuses a plain http:// address to another host unless told otherwise.
      # This one stays on the compose network.
      PROBE_ALLOW_INSECURE: "true"
      # The token the api stores on the local probe (PROBE_BOOTSTRAP_TOKEN)
      PROBE_TOKEN: "${PROBE_BOOTSTRAP_TOKEN:?PROBE_BOOTSTRAP_TOKEN is not set: add it to .env as a random string of at least 32 characters, such as the output of openssl rand -hex 32}"
      LOG_LEVEL: ${LOG_LEVEL:-info}
    depends_on:
      api:
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

    # Remote probes (the probe protocol). A compressed results batch can be a little over
    # 1 MiB, which is more than nginx's default client_max_body_size of 1m allows.
    location /probe/ {
      client_max_body_size 2m;
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

`docker compose ps` should list the `api` and the `probe` as running. The `probe` starts once the `api` is healthy, and the Probes page in Peekaping shows the local probe as online once it has connected.

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
- **Probe**: [`0xfurai/peekaping-probe`](https://hub.docker.com/r/0xfurai/peekaping-probe) runs the checks of the local probe's monitors. It also runs checks from another machine, as a remote probe. See [Remote probes](./remote-probes.md)
- **Web**: [`0xfurai/peekaping-web`](https://hub.docker.com/r/0xfurai/peekaping-web)
- **Migrate**: [`0xfurai/peekaping-migrate`](https://hub.docker.com/r/0xfurai/peekaping-migrate), which applies the database migrations. The API waits for it to finish

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


### Startup check

At start, Peekaping checks the database file for corruption, and stops if the check finds any. In single mode, `peekaping` runs the check. In cluster mode, each `api` container runs it. `SQLITE_STARTUP_CHECK` chooses the check:

| Value | What runs | Time for a 1.5 GB database |
| --- | --- | --- |
| `quick` (default) | `PRAGMA quick_check`, which walks the b-tree pages. It doesn't find an index that disagrees with its table. | About 2 seconds |
| `full` | `PRAGMA integrity_check`, which reads the whole file and also finds an index that disagrees with its table. | About 30 seconds, at every start |
| `off` | Nothing. The log says that the check is skipped. | None |

The values are lower case. Any other value fails the configuration check, and Peekaping doesn't start.

In single mode, pass the setting to the container: `-e SQLITE_STARTUP_CHECK=full` with `docker run`, or `- SQLITE_STARTUP_CHECK=full` under `environment` in the Compose file above. In cluster mode, add `SQLITE_STARTUP_CHECK=full` to the `.env` file, which the `api` reads.

With `off`, a damaged database goes unnoticed, and Peekaping keeps writing to it, which can make the damage worse. [Recovering a corrupt SQLite database](./sqlite-recovery.md#the-startup-check) says what each check finds and what to do when one fails.

### Updating Peekaping

```bash
# Pull latest images
docker compose pull

# Restart with new images
docker compose up -d

# Clean up old images
docker image prune
```
