# SMTP Testing - Quick Start (Postfix)

This guide shows the **fastest path** to test the SMTP health check using the Postfix test server.

## 1. Start Postfix Test Server (Terminal 1)

```bash
cd test_utils/smtp
./test-smtp.sh start
```

This starts a single **Postfix** SMTP server with:
- **Plain SMTP**: `localhost:1027`
- **STARTTLS/Submission**: `localhost:1028`
- **Direct TLS/SMTPS**: `localhost:1029`
- **Auth**: `testuser` / `testpass` (realm: `test-smtp.local`)

## 2. Start Peekaping (Terminal 2)

```bash
# Install dependencies (first time only)
make setup
make install

# Start database
docker-compose -f docker-compose.dev.mongo.yml up -d

# Start application
make dev
```

Access:
- Frontend: http://localhost:8383
- API: http://localhost:8034
- API Docs: http://localhost:8034/swagger/index.html

## 3. Create SMTP Monitor

### Via Web UI:
1. Go to http://localhost:8383
2. Login/Register
3. Click "+ Add Monitor"
4. Select "SMTP" type
5. Configure:
   ```
   Name: My SMTP Test (Plain)
   Host: localhost
   Port: 1027
   Use TLS: false
   ```
6. Save

### Via API:
```bash
# Login first
curl -X POST http://localhost:8034/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "admin123",
    "email": "admin@test.com"
  }'

TOKEN=$(curl -X POST http://localhost:8034/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' \
  | jq -r '.access_token')

# Create SMTP monitor
curl -X POST http://localhost:8034/api/monitors \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name": "SMTP Test (Plain)",
    "type": "smtp",
    "interval": 60,
    "timeout": 10,
    "config": {
      "host": "localhost",
      "port": 1027,
      "use_tls": false
    }
  }'
```

## 4. Test Different Scenarios

All examples assume the Postfix test server from step 1 is running.

### 4.1 Basic SMTP (No TLS)
```json
{
  "host": "localhost",
  "port": 1027,
  "use_tls": false
}
```

### 4.2 SMTP with STARTTLS (Port 587)
```json
{
  "host": "localhost",
  "port": 1028,
  "use_tls": true,
  "use_direct_tls": false,
  "ignore_tls_errors": true,
  "check_cert_expiry": true
}
```

### 4.3 SMTP with Direct TLS/SMTPS (Port 465)
```json
{
  "host": "localhost",
  "port": 1029,
  "use_tls": true,
  "use_direct_tls": true,
  "ignore_tls_errors": true,
  "check_cert_expiry": true
}
```

### 4.4 SMTP with Auth (STARTTLS)
```json
{
  "host": "localhost",
  "port": 1028,
  "use_tls": true,
  "use_direct_tls": false,
  "ignore_tls_errors": true,
  "username": "testuser",
  "password": "testpass"
}
```

### 4.5 SMTP with Auth (Direct TLS)
```json
{
  "host": "localhost",
  "port": 1029,
  "use_tls": true,
  "use_direct_tls": true,
  "ignore_tls_errors": true,
  "username": "test",
  "password": "test123"
}
```

### 4.6 MAIL FROM Test
```json
{
  "host": "localhost",
  "port": 1027,
  "use_tls": false,
  "from_email": "monitor@example.com"
}
```

### 4.7 Open Relay Test
```json
{
  "host": "localhost",
  "port": 1027,
  "use_tls": false,
  "from_email": "test@example.com",
  "test_open_relay": true,
  "open_relay_failure": true
}
```

## 5. View Results

- **Web UI:** http://localhost:8383 → Monitor Dashboard
- **Logs:** `./test-smtp.sh logs`

## Cleanup

```bash
# Stop Postfix test server
./test-smtp.sh stop

# Stop Peekaping (Ctrl+C in terminal)

# Stop database
make docker-down
```

## Troubleshooting

```bash
# Check if SMTP servers are running
./test-smtp.sh status

# View logs
./test-smtp.sh logs

# Test connection manually (plain SMTP)
telnet localhost 1027
```

## Next: Real SMTP Testing

Test with Gmail (requires app password):
```json
{
  "host": "smtp.gmail.com",
  "port": 587,
  "use_tls": true,
  "check_cert_expiry": true,
  "username": "your-email@gmail.com",
  "password": "your-app-password"
}
```

For full documentation, see: [SMTP_TESTING_GUIDE.md](../../SMTP_TESTING_GUIDE.md)

