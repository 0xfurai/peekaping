---
sidebar_position: 1
---

# Docker + PostgreSQL

## Monolithic mode

The simplest mode of operation is the monolithic deployment mode. This mode runs all of Vigi microservice components (db + api + web + gateway) inside a single process as a single Docker image.

```bash
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=vigi \
  -e DB_USER=vigi \
  -e DB_PASS=secure_test_password_123 \
  -v $(pwd)/.data/postgres:/var/lib/postgresql/data \
  --name vigi \
  0xfurai/vigi-bundle-postgres:latest
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

Create a new directory for your Vigi installation and set up the following structure:

```
vigi/
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
DB_NAME=vigi
DB_HOST=database
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
      POSTGRES_DB: ${DB_NAME:-vigi}
    volumes:
      - ./.data/postgres:/var/lib/postgresql/data
    networks:
      - appnet
    healthcheck:
      test:
        [
          "CMD-SHELL",
          "pg_isready -U ${DB_USER:-postgres} -d ${DB_NAME:-vigi}",
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
    image: 0xfurai/vigi-migrate:latest
    restart: "no"
    env_file:
      - .env
    depends_on:
      database:
        condition: service_healthy
    networks:
      - appnet

  api:
    image: 0xfurai/vigi-api:latest
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
    image: 0xfurai/vigi-producer:latest
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
    image: 0xfurai/vigi-worker:latest
    restart: unless-stopped
    env_file:
      - .env
    depends_on:
      redis:
        condition: service_healthy
    networks:
      - appnet

  ingester:
    image: 0xfurai/vigi-ingester:latest
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
    image: 0xfurai/vigi-web:latest
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



### 3. Start Vigi

```bash
# Navigate to your project directory
cd vigi

# Start all services
docker compose up -d

# Check status
docker compose ps

# View logs
docker compose logs -f
```

### 4. Access Vigi

Once all containers are running:

1. Open your browser and go to `http://localhost:8383`
2. Create your admin account
3. Create your first monitor!

## Docker Images

Vigi provides official Docker images:

- **Server**: [`0xfurai/vigi-server`](https://hub.docker.com/r/0xfurai/vigi-server)
- **Web**: [`0xfurai/vigi-web`](https://hub.docker.com/r/0xfurai/vigi-web)

### Image Tags

- `latest` - Latest stable release
- `x.x.x` - Specific version tags

## Persistent Data

Vigi stores data in Postgres. The docker-compose setup uses a local folder mount `./.data/postgres:/var/lib/postgresql/data` to persist your monitoring data.

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


### Updating Vigi

```bash
# Pull latest images
docker compose pull

# Restart with new images
docker compose up -d

# Clean up old images
docker image prune
```
