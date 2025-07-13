---
sidebar_position: 1
---

# Docker + SQLite

## Monolithic mode

The simplest mode of operation is the monolithic deployment mode. This mode runs all of Peekaping microservice components (db + api + web + gateway) inside a single process as a single Docker image.

```bash
docker run -d --rm --restart=always \
  -p 8383:8383 \
  -e DB_NAME=/app/data/peekaping.db \
  -e ACCESS_TOKEN_SECRET_KEY=test_access_token_secret_key_16_characters_long \
  -e REFRESH_TOKEN_SECRET_KEY=test_refresh_token_secret_key_16_characters_long \
  -v $(pwd)/.data/sqlite:/app/data \
  0xfurai/peekaping-bundle-sqlite:latest
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
DB_NAME=/app/data/peekaping.db
DB_TYPE=sqlite

# Server Configuration
SERVER_PORT=8034
CLIENT_URL="http://localhost:8383"

# JWT Configuration
ACCESS_TOKEN_EXPIRED_IN=15m
ACCESS_TOKEN_SECRET_KEY=your-access-token-secret-here
REFRESH_TOKEN_EXPIRED_IN=60m
REFRESH_TOKEN_SECRET_KEY=your-refresh-token-secret-here

# Application Settings
MODE=prod
TZ="America/New_York"
```
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
  migrate:
    image: 0xfurai/peekaping-migrate:latest
    restart: "no"
    env_file:
      - .env
    volumes:
      - ./.data/sqlite:/app/data

  server:
    image: 0xfurai/peekaping-server:latest
    restart: unless-stopped
    env_file:
      - .env
    volumes:
      - ./.data/sqlite:/app/data
    depends_on:
      migrate:
        condition: service_completed_successfully
    networks:
      - appnet
    healthcheck:
      test: ["CMD-SHELL", "wget -q http://localhost:8034/api/v1/health || exit 1"]
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


### Updating Peekaping

```bash
# Pull latest images
docker compose pull

# Restart with new images
docker compose up -d

# Clean up old images
docker image prune
```
