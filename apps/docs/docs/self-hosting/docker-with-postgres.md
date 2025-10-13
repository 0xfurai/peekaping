---
sidebar_position: 1
---

# Docker + PostgreSQL

## Security Features

### Non-root User Support

Peekaping bundle images now run the server and web components as a non-root user (`peekaping`) for enhanced security. The database services continue to run as their dedicated system users (e.g., `postgres`).

#### Custom UID/GID

You can customize the user and group IDs for the `peekaping` user using build arguments:

```bash
docker build --build-arg UID=1001 --build-arg GID=1001 -f Dockerfile.bundle.postgres -t peekaping-custom .
```

Or in docker-compose.yml:

```yaml
services:
  peekaping:
    build:
      context: .
      dockerfile: Dockerfile.bundle.postgres
      args:
        UID: 1001
        GID: 1001
```

#### Volume Permissions

When using custom UID/GID, ensure your host volumes have appropriate permissions:

```bash
# Create data directory with correct ownership
sudo mkdir -p ./.data/postgres
sudo chown -R 1001:1001 ./.data/postgres
```

### Docker Socket Security

For Docker monitoring, Peekaping supports both socket and TCP connections. For enhanced security, consider using a Docker socket proxy:

1. Set up [docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy)
2. Configure your Docker monitors to use TCP connection type
3. Point to your proxy endpoint (e.g., `http://dockerproxy:2375`)

This eliminates the need to mount the Docker socket directly into the Peekaping container.

#### Docker Socket Proxy Setup

For maximum security, use `docker-socket-proxy` to provide read-only access to Docker API:

```yaml
version: "3.8"

services:
  dockerproxy:
    image: ghcr.io/tecnativa/docker-socket-proxy:latest
    container_name: dockerproxy
    environment:
      CONTAINERS: 1
      SERVICES: 1
      TASKS: 1
      POST: 0
      PRIVILEGED: 0
      IMAGES: 0
      VOLUMES: 0
      NETWORKS: 0
      SYSTEM: 0
    ports: []
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    restart: unless-stopped
    networks:
      - internal

  peekaping:
    image: ghcr.io/0xfurai/peekaping-bundle-postgres:latest
    container_name: peekaping
    restart: unless-stopped
    ports:
      - 8383:8383
    environment:
      DB_NAME: peekaping
      DB_USER: peekaping
      DB_PASS: secure_test_password_123
      DOCKERPROXY_HOST: "http://dockerproxy:2375"
      UID: 1000
      GID: 1000
      TZ: "Europe/Berlin"
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    networks:
      - internal
    depends_on:
      - dockerproxy

networks:
  internal: {}
```

**Security Benefits:**
- **Read-only access** - Cannot create/delete containers
- **No privileged operations** - Prevents system modifications
- **Network isolation** - Proxy runs in internal network
- **Minimal permissions** - Only necessary Docker API endpoints

**Configuration in Peekaping:**
1. Go to **Settings** → **Docker**
2. **Connection Type**: Select "TCP"
3. **Host**: Enter `http://dockerproxy:2375`
4. **Test Connection** to verify proxy access


## Monolithic mode

The simplest mode of operation is the monolithic deployment mode. This mode runs all of Peekaping microservice components (db + api + web + gateway) inside a single process as a single Docker image.

```bash
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=peekaping \
  -e DB_USER=peekaping \
  -e DB_PASS=secure_test_password_123 \
  -v $(pwd)/.data/postgres:/var/lib/postgresql/data \
  0xfurai/peekaping-bundle-postgres:latest
```
To add custom caddy file add
```
-v ./custom-Caddyfile:/etc/caddy/Caddyfile:ro
```

If you need more granular control on system components read [Microservice mode section](#microservice-mode)

## Microservice mode

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
DB_HOST=postgres
DB_PORT=5432
DB_TYPE=postgres

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
- Generate secure JWT secret keys (use a password generator)
- Consider using environment-specific secrets management
:::

#### `docker-compose.yml` file

Create a `docker-compose.yml` file:

```yaml
networks:
  appnet:

services:
  postgres:
    image: postgres:17
    restart: unless-stopped
    env_file:
      - .env
    environment:
      POSTGRES_USER: ${DB_USER}
      POSTGRES_PASSWORD: ${DB_PASS}
      POSTGRES_DB: ${DB_NAME}
    volumes:
      - ./.data/postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${DB_USER} -d ${DB_NAME}"]
      interval: 1s
      timeout: 60s
      retries: 60
    networks:
      - appnet

  migrate:
    image: 0xfurai/peekaping-migrate:latest
    restart: "no"
    env_file:
      - .env
    depends_on:
      postgres:
        condition: service_healthy
    networks:
      - appnet

  server:
    image: 0xfurai/peekaping-server:latest
    restart: unless-stopped
    env_file:
      - .env
    depends_on:
      postgres:
        condition: service_healthy
      migrate:
        condition: service_completed_successfully
    networks:
      - appnet
    healthcheck:
      test: ["CMD-SHELL", "wget -qO - http://localhost:8034/api/v1/health || exit 1"]
      interval: 1s
      timeout: 60s
      retries: 60

  web:
    image: 0xfurai/peekaping-web:latest
    restart: unless-stopped
    networks:
      - appnet
    healthcheck:
      test: ["CMD-SHELL", "curl -f http://localhost:80 || exit 1"]
      interval: 1s
      timeout: 60s
      retries: 60

  gateway:
    image: nginx:latest
    restart: unless-stopped
    ports:
      - "8383:80"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
    depends_on:
      server:
        condition: service_healthy
      web:
        condition: service_healthy
    networks:
      - appnet
    healthcheck:
      test: ["CMD-SHELL", "curl -f http://localhost:80 || exit 1"]
      interval: 1s
      timeout: 60s
      retries: 60
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
