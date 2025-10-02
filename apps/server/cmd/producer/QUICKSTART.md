# Producer Service - Quick Start Guide

## What is the Producer?

The Producer is a **background service** that schedules and enqueues health check tasks for monitors. It uses leader election to ensure only one instance is active at a time, making it perfect for high-availability deployments.

```
┌──────────────┐       ┌──────────────┐       ┌──────────────┐
│  Producer    │──────▶│    Redis     │◀──────│   Workers    │
│ (Scheduler)  │       │   (Queue)    │       │ (Executors)  │
└──────────────┘       └──────────────┘       └──────────────┘
```

## Prerequisites

Before running the producer, ensure you have:

1. **Go 1.24+** installed
2. **Redis** running (for leader election and task queue)
3. **Database** running (PostgreSQL, MySQL, SQLite, or MongoDB)
4. **Environment configured** (see Configuration section)

## Installation

### Option 1: Run from Source

```bash
cd apps/server
go run ./cmd/producer/main.go
```

### Option 2: Build Binary

```bash
# Using Makefile
make build-producer

# The binary will be in bin/producer
./bin/producer
```

### Option 3: Build Manually

```bash
cd apps/server
go build -o producer ./cmd/producer
./producer
```

## Configuration

Create a `.env` file in the project root or set environment variables:

```bash
# Database Configuration
DB_TYPE=postgres          # or mysql, sqlite, mongo
DB_HOST=localhost
DB_PORT=5432
DB_NAME=peekaping
DB_USER=postgres
DB_PASS=yourpassword

# Redis Configuration
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=          # Leave empty if no password
REDIS_DB=0

# Application Settings
MODE=dev                 # or prod
LOG_LEVEL=info          # debug, info, warn, error
TZ=UTC                  # Your timezone
```

## Quick Start

### 1. Start Redis

```bash
# Using Docker
docker run -d -p 6379:6379 redis:alpine

# Or using Docker Compose
docker-compose -f docker-compose.dev.postgres.yml up -d redis
```

### 2. Start Database

```bash
# PostgreSQL example
docker run -d \
  -e POSTGRES_DB=peekaping \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=password \
  -p 5432:5432 \
  postgres:16-alpine

# Run migrations
cd apps/server
go run cmd/bun/main.go db migrate
```

### 3. Start Producer

```bash
# From project root
make producer

# Or directly
cd apps/server
go run ./cmd/producer/main.go
```

You should see output like:

```
2025/10/02 10:00:00 Starting Peekaping Producer v1.0.0
[INFO] Starting producer
[INFO] Starting leader election for node: 550e8400-e29b-41d4-a716-446655440000
[INFO] Starting monitor scheduler
[INFO] Became leader node_id=550e8400-e29b-41d4-a716-446655440000
[INFO] Syncing monitors with scheduler
[INFO] Added/updated monitor job monitor_id=mon-123 monitor_name="My API" interval=60 cron_expr=@every 60s
[INFO] Synced 5 active monitors
[INFO] Producer is running. Press Ctrl+C to stop.
```

## Testing

### 1. Create a Test Monitor

Using the API:

```bash
curl -X POST http://localhost:8084/api/v1/monitors \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Monitor",
    "type": "http",
    "interval": 30,
    "timeout": 10,
    "active": true,
    "config": "{\"url\":\"https://example.com\"}"
  }'
```

### 2. Check Logs

You should see:

```
[INFO] Monitor created event received monitor_id=mon-456
[INFO] Added/updated monitor job monitor_id=mon-456 monitor_name="Test Monitor" interval=30 cron_expr=@every 30s
```

### 3. Verify Tasks are Enqueued

```bash
# Connect to Redis
redis-cli

# Check queue
LLEN asynq:queues:default
```

### 4. Monitor Redis Leader Key

```bash
# Check current leader
redis-cli GET peekaping:producer:leader

# Monitor key with TTL
redis-cli --no-raw WATCH peekaping:producer:leader
```

## Running Multiple Producers (High Availability)

### Terminal 1:
```bash
cd apps/server
go run ./cmd/producer/main.go
```

### Terminal 2:
```bash
cd apps/server
go run ./cmd/producer/main.go
```

### Terminal 3:
```bash
cd apps/server
go run ./cmd/producer/main.go
```

**Expected behavior:**
- Only **one** producer becomes the leader
- Others wait as standbys
- If you kill the leader, another takes over within ~10 seconds

## Common Issues

### Issue: "Failed to connect to Redis"

**Solution:**
```bash
# Check if Redis is running
redis-cli ping

# Check Redis connection
redis-cli -h localhost -p 6379 -a yourpassword ping
```

### Issue: "Failed to get active monitors"

**Solution:**
```bash
# Check database connection
# PostgreSQL
psql -h localhost -U postgres -d peekaping -c "SELECT COUNT(*) FROM monitors;"

# Check if migrations are run
cd apps/server
go run cmd/bun/main.go db status
```

### Issue: "Producer not becoming leader"

**Solution:**
```bash
# Check if another producer holds the lock
redis-cli GET peekaping:producer:leader

# If stuck, manually delete the key
redis-cli DEL peekaping:producer:leader
```

### Issue: "No tasks being enqueued"

**Checklist:**
1. Is producer the leader? Check logs for "Became leader"
2. Are there active monitors? Check database: `SELECT * FROM monitors WHERE active = true;`
3. Is scheduler running? Check logs for "Added/updated monitor job"
4. Is Redis queue accessible? Check with `redis-cli LLEN asynq:queues:default`

## Development

### Running Tests

```bash
cd apps/server/internal/modules/producer
go test -v ./...
```

### Debug Logging

Set `LOG_LEVEL=debug` in your environment:

```bash
LOG_LEVEL=debug go run ./cmd/producer/main.go
```

You'll see detailed logs:

```
[DEBUG] Enqueuing health check task monitor_id=mon-123 payload={"monitor_id":"mon-123","scheduled_at":"2025-10-02T10:00:00Z"}
[DEBUG] Successfully enqueued health check task monitor_id=mon-123
```

### Hot Reload During Development

Use `air` for hot reload:

```bash
# Install air
go install github.com/cosmtrek/air@latest

# Run with air
cd apps/server
air -c .air-producer.toml
```

Create `.air-producer.toml`:

```toml
root = "."
tmp_dir = "tmp"

[build]
  cmd = "go build -o ./tmp/producer ./cmd/producer"
  bin = "./tmp/producer"
  include_ext = ["go"]
  exclude_dir = ["tmp", "vendor", "node_modules"]
```

## Production Deployment

### Docker

Create `Dockerfile.producer`:

```dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY . .

RUN go mod download
RUN go build -o producer ./apps/server/cmd/producer

FROM alpine:latest

RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/producer .

CMD ["./producer"]
```

Build and run:

```bash
docker build -f Dockerfile.producer -t peekaping-producer .
docker run -d \
  --name producer \
  --env-file .env \
  peekaping-producer
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: peekaping-producer
spec:
  replicas: 3  # For high availability
  selector:
    matchLabels:
      app: producer
  template:
    metadata:
      labels:
        app: producer
    spec:
      containers:
      - name: producer
        image: peekaping-producer:latest
        env:
        - name: DB_TYPE
          value: "postgres"
        - name: REDIS_HOST
          value: "redis-service"
        # ... more env vars
```

### systemd Service

Create `/etc/systemd/system/peekaping-producer.service`:

```ini
[Unit]
Description=Peekaping Producer Service
After=network.target redis.service postgresql.service

[Service]
Type=simple
User=peekaping
WorkingDirectory=/opt/peekaping
EnvironmentFile=/opt/peekaping/.env
ExecStart=/opt/peekaping/producer
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl enable peekaping-producer
sudo systemctl start peekaping-producer
sudo systemctl status peekaping-producer
```

## Next Steps

1. **Set up workers** to consume and execute the health check tasks
2. **Monitor producer** using logs and metrics
3. **Scale producers** by running multiple instances
4. **Configure alerts** for producer failures

## Resources

- [Architecture Documentation](./ARCHITECTURE.md) - Detailed technical overview
- [README](./README.md) - Complete feature documentation
- [Main Server](../server/main.go) - API server that creates monitors
- [Queue Documentation](../../internal/modules/queue/README.md) - Queue system details

## Need Help?

Check the logs for detailed error messages:

```bash
# Follow logs
tail -f /var/log/peekaping-producer.log

# Or with journalctl
sudo journalctl -u peekaping-producer -f
```

Look for:
- `[ERROR]` - Critical errors
- `[WARN]` - Warnings that might need attention
- `[INFO]` - Important operational messages
- `[DEBUG]` - Detailed debugging information

