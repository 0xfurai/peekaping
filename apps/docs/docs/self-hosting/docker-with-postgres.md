---
sidebar_position: 1
---

# Docker + PostgreSQL

Peekaping runs in one of two modes. See [Deployment modes](./deployment-modes.md) for what each one runs:

- **[Single mode](#single-mode)**: one container runs one Peekaping process, `peekaping`, beside its own PostgreSQL, with no Redis. Start here.
- **[Cluster mode](#cluster-mode)**: the API, producer, worker and ingester run in separate containers and talk through Redis. Use it when one process isn't enough, for example to run several workers.

## Single mode

The `peekaping-bundle-postgres` image runs Peekaping in single mode, beside PostgreSQL 17 in the same container. One process, `peekaping`, serves the web app, the API and the websocket on port 8383. PostgreSQL keeps its data in `/var/lib/postgresql/data`, so keep that on a volume.

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

Or with Docker Compose:

```yaml
services:
  peekaping:
    image: 0xfurai/peekaping-bundle-postgres:latest
    restart: unless-stopped
    # peekaping takes up to 30 seconds to shut down cleanly, and PostgreSQL stops after it
    stop_grace_period: 35s
    ports:
      - "8383:8383"
    environment:
      - DB_NAME=peekaping
      - DB_USER=peekaping
      - DB_PASS=secure_test_password_123
    volumes:
      - ./.data/postgres:/var/lib/postgresql/data
```

Then open `http://localhost:8383` and create your admin account.

`DB_USER` and `DB_PASS` are required. On the first start, the container creates the database cluster, the `DB_USER` user with the password `DB_PASS`, and the `DB_NAME` database (`peekaping` by default). A user or database that already exists, such as PostgreSQL's own `postgres`, is kept as it is.

`peekaping` runs as a non-root user, and PostgreSQL as its own `postgres` user. See [Bundle user and permissions](./bundle-user.md) to set `PUID`/`PGID`, to start the container with `--user`, and for what Docker and ping monitors need.

### Settings

The image reads the same environment variables as cluster mode (see the [`.env` file](#env-file) below), with these exceptions:

- `peekaping` always listens on port 8383 in the container. The image ignores `SERVER_PORT`, so a `.env` with `SERVER_PORT=8034` works unchanged. To use another port on the host, change the published port, for example `-p 8080:8383`.
- Leave `DB_HOST` unset, or set it to `localhost`: that's the bundled PostgreSQL. The cluster-mode `.env` sets `DB_HOST=database`, which doesn't work here.
- There is no Redis, so `REDIS_*` variables are ignored.
- `MODE` defaults to `prod`.
- Leave `API_URL` unset, so the web app calls the API on its own origin. Set it only if the web app must call an API on another origin.

### Stopping

When it stops, `peekaping` finishes open requests, stops its checks and closes the database. Then PostgreSQL shuts down. Together that can take about 30 seconds, but `docker stop` waits only 10 seconds before it kills the container. Give it more time:

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

The login lockout counts failed logins per client address. A client can send any `X-Forwarded-For` header it likes, so Peekaping believes that header (and `X-Real-IP`) only on requests from an address listed in `TRUSTED_PROXIES`. The list is comma-separated IP addresses and CIDR ranges, and it is empty by default. Behind a reverse proxy, set it to the address the proxy connects from, as the container sees it, in your `.env` file or with `-e`:

```
TRUSTED_PROXIES=172.17.0.1
```

With Docker's default network and a proxy on the same host, that is usually the network's gateway address, which `docker network inspect bridge` shows as `Gateway`. The proxy must put the client's address last in `X-Forwarded-For`, by adding it or by replacing the header. Caddy, nginx and Traefik do. Peekaping takes the last address in the header that isn't in `TRUSTED_PROXIES`, so addresses a client adds to the left don't count. List only proxies you run, because a listed address can claim to be anyone. A request from any other address, including one that reaches port 8383 past your proxy, counts as coming from the address it connected from.

With `TRUSTED_PROXIES` empty behind a proxy, every login counts as coming from the proxy, so one person's failed logins can lock an account for everyone.

Whatever you set, Peekaping also locks an account after `BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS` failed logins (100 by default) from any addresses within `BRUTEFORCE_ACCOUNT_WINDOW` (10 minutes). A lock lasts `BRUTEFORCE_ACCOUNT_LOCKOUT` (1 minute). The count of failures is kept until the window, measured from the first failure, has passed, so until then each further failure after a lock ends locks the account again. So guessing is bounded to the limit per window plus one attempt each time a lock ends, and after an attack stops the owner is back in within the window at most. An attacker who keeps going can keep the account locked. [API Server](../architecture/api-server.md#security-configuration) lists the lockout settings.

### Upgrading from a bundle with Redis and Caddy

If you run a bundle from 0.0.44 to 0.0.47, which ran Redis and Caddy inside the container, see [Upgrading to single mode](./upgrading-to-single-mode.md).

## Cluster mode

In cluster mode, the API, producer, worker and ingester run in separate containers and talk through Redis. An nginx gateway serves the web app and the API on port 8383.

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
DB_PORT=5432
DB_TYPE=postgres

# Server Configuration
# The API's port inside the Docker network. The single-mode image ignores it
# and always listens on 8383.
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

```yaml
networks:
  appnet:

services:
  database:
    image: postgres:17
    restart: unless-stopped
    env_file:
      - .env
    environment:
      POSTGRES_USER: ${DB_USER:-postgres}
      POSTGRES_PASSWORD: ${DB_PASS:-password}
      POSTGRES_DB: ${DB_NAME:-peekaping}
    volumes:
      - ./.data/postgres:/var/lib/postgresql/data
    networks:
      - appnet
    healthcheck:
      test:
        [
          "CMD-SHELL",
          "pg_isready -U ${DB_USER:-postgres} -d ${DB_NAME:-peekaping}",
        ]
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

  migrate:
    image: 0xfurai/peekaping-migrate:latest
    restart: "no"
    env_file:
      - .env
    depends_on:
      database:
        condition: service_healthy
    networks:
      - appnet

  api:
    image: 0xfurai/peekaping-api:latest
    restart: unless-stopped
    env_file:
      - .env
    environment:
      # The api isn't published: only the gateway and the other services on this network reach
      # it, all from private addresses. The gateway (nginx.conf) sets X-Forwarded-For to the
      # client's address, and the api believes that header from these addresses only. Set
      # TRUSTED_PROXIES in .env to change them.
      TRUSTED_PROXIES: "${TRUSTED_PROXIES:-127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7}"
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

The gateway replaces the `X-Forwarded-For` header of every request with the client's address, and the `api` service believes that header from loopback and the private ranges (`TRUSTED_PROXIES` in the compose file), which cover the Docker network. Don't publish the `api` service's port: a connection to a published port reaches the container from the Docker network's gateway address, which that setting trusts, so a client could send its own `X-Forwarded-For`.



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

Peekaping stores data in Postgres. The docker-compose setup uses a local folder mount `./.data/postgres:/var/lib/postgresql/data` to persist your monitoring data.

### Storage Options

You have two options for persistent storage:

1. **Local folder mount** (recommended):
   ```yaml
   volumes:
     - ./.data/postgres:/var/lib/postgresql/data
   ```
   This creates a `.data/postgres` folder in your project directory.

2. **Named volume**:
   ```yaml
   volumes:
     - postgres_data:/var/lib/postgresql/data
   ```
   Then add at the bottom of your docker-compose.yml:
   ```yaml
   volumes:
     postgres_data:
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
