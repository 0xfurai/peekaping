---
sidebar_position: 7
---

# Docker + MongoDB

:::warning MongoDB storage is deprecated
MongoDB as a storage backend (the database Peekaping keeps its data in) is deprecated. Existing MongoDB installs keep working.

- **New installs** should use [SQLite](./docker-with-sqlite.md) or [PostgreSQL](./docker-with-postgres.md).
- A command to move your data from MongoDB to SQLite or PostgreSQL will ship before MongoDB is removed.
- MongoDB is removed no earlier than the second release after the one that ships that command.
- Until then, MongoDB gets no new features. It gets fixes only for data loss, startup failures and security problems.

The MongoDB monitor type, which checks other MongoDB servers, is not affected.
:::

With MongoDB, Peekaping runs in cluster mode only: the API, producer, worker and ingester are separate processes that talk through Redis. Single mode, one `peekaping` process, supports SQLite and PostgreSQL only.

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
