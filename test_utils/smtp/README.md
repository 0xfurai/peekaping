# SMTP Test Environment

This directory contains tools for testing the SMTP health check implementation using a Postfix test server.

## Quick Start

### 1. Start Postfix Test Server

```bash
cd test_utils/smtp
docker-compose -f docker-compose.smtp-test.yml up -d postfix
```

This will start a Postfix SMTP server with:
- **Plain SMTP**: Port 1027 (container port 25)
- **STARTTLS/Submission**: Port 1028 (container port 587)
- **Direct TLS/SMTPS**: Port 1029 (container port 465)
- **Authentication**: `testuser` / `testpass` (realm: `test-smtp.local`)
- **Self-signed certificates**: CN `test-smtp.local`

### 2. Verify Server is Running

```bash
docker ps | grep peekaping-test-postfix
```

You should see the Postfix container running.

### 3. Test SMTP Connection

```bash
# Test plain SMTP
telnet localhost 1027

# Test STARTTLS
openssl s_client -connect localhost:1028 -starttls smtp

# Test direct TLS/SMTPS
openssl s_client -connect localhost:1029
```

## Using with Peekaping

Once you have Peekaping running locally, you can create SMTP monitors with these configurations:

### Example 1: Plain SMTP (No TLS)

```json
{
  "host": "localhost",
  "port": 1027,
  "use_tls": false
}
```

### Example 2: SMTP with STARTTLS (Port 587)

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

### Example 3: SMTP with Direct TLS/SMTPS (Port 465)

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

### Example 4: SMTP with Authentication and STARTTLS

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

### Example 5: SMTP with Authentication and Direct TLS

```json
{
  "host": "localhost",
  "port": 1029,
  "use_tls": true,
  "use_direct_tls": true,
  "ignore_tls_errors": true,
  "username": "testuser",
  "password": "testpass"
}
```

### Example 6: SMTP with MAIL FROM Test

```json
{
  "host": "localhost",
  "port": 1027,
  "use_tls": false,
  "from_email": "monitor@example.com"
}
```

### Example 7: Open Relay Test

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

## Testing with Real SMTP Servers

You can also test with real SMTP servers:

### Gmail (requires App Password)

```json
{
  "host": "smtp.gmail.com",
  "port": 587,
  "use_tls": true,
  "check_cert_expiry": true,
  "username": "your-email@gmail.com",
  "password": "your-app-password",
  "from_email": "your-email@gmail.com"
}
```

### SendGrid

```json
{
  "host": "smtp.sendgrid.net",
  "port": 587,
  "use_tls": true,
  "check_cert_expiry": true,
  "username": "apikey",
  "password": "your-sendgrid-api-key"
}
```

### Mailgun

```json
{
  "host": "smtp.mailgun.org",
  "port": 587,
  "use_tls": true,
  "check_cert_expiry": true,
  "username": "your-mailgun-username",
  "password": "your-mailgun-password"
}
```

## Cleanup

To stop and remove the Postfix test server:

```bash
docker-compose -f docker-compose.smtp-test.yml down
```

## Troubleshooting

### Port Already in Use

If ports are already in use, you can modify the ports in `docker-compose.smtp-test.yml`:

```yaml
ports:
  - "1027:25"    # Change left side: "NEW_PORT:25"
  - "1028:587"   # Change left side: "NEW_PORT:587"
  - "1029:465"   # Change left side: "NEW_PORT:465"
```

### Connection Refused

Make sure Docker is running:

```bash
docker ps
```

If the container is not running, start it again:

```bash
docker-compose -f docker-compose.smtp-test.yml up -d postfix
```

### View Logs

To see what's happening in the Postfix server:

```bash
docker-compose -f docker-compose.smtp-test.yml logs -f postfix
```

## Running Tests

### Unit Tests

Run the unit tests (uses mock servers):

```bash
cd test_utils/smtp
./test-smtp.sh run-tests
```

Or directly:

```bash
cd apps/server
go test -v ./internal/modules/healthcheck/executor -run TestSMTP
```

### Integration Tests

Integration tests run against the Postfix test server. First, start the server:

```bash
cd test_utils/smtp
./test-smtp.sh start
```

Then run integration tests:

```bash
./test-smtp.sh run-integration-tests
```

Or directly:

```bash
cd apps/server
POSTFIX_TESTS=1 go test -v ./internal/modules/healthcheck/executor -run TestSMTPExecutor_Execute_Postfix_Integration
```

**Note:** Integration tests require the `POSTFIX_TESTS=1` environment variable to run. By default, they are skipped. If the Postfix container is not running when tests are enabled, the tests will fail with a clear error message.

## Testing Features

### Certificate Expiration

When `check_cert_expiry` is enabled, Peekaping will:
1. Extract the TLS certificate during STARTTLS
2. Display certificate expiration information
3. Send notifications when certificates are about to expire

### Authentication

Test servers accept any username/password. For real servers, use valid credentials.

### STARTTLS and Direct TLS

The Postfix test server supports both STARTTLS (port 1028) and direct TLS/SMTPS (port 1029). Set `use_tls: true` and `use_direct_tls: false` for STARTTLS, or `use_tls: true` and `use_direct_tls: true` for direct TLS.

### Open Relay Testing

The SMTP monitor supports testing for open relay vulnerabilities:

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

- `test_open_relay`: Enable open relay testing
- `open_relay_failure`: If `true`, treat open relay as failure (security risk). If `false`, treat open relay as success (expected behavior)

Integration tests verify that Postfix correctly rejects relay attempts on all ports (1027, 1028, 1029) when not authenticated, and allows relay for authenticated users.

