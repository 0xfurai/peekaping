# Postfix Test Server

This directory contains a Postfix SMTP server configured for testing SMTP health checks with:
- **STARTTLS** on port 587 (submission)
- **Direct TLS (SMTPS)** on port 465
- **Authentication** with test user credentials
- **Self-signed certificates** for TLS testing
- **Open relay protection** (rejects unauthorized relaying)

## Configuration

### Ports
- **1027**: Plain SMTP (port 25)
- **1028**: Submission with STARTTLS (port 587)
- **1029**: SMTPS with direct TLS (port 465)

### Authentication
- **Username**: `testuser`
- **Password**: `testpass`
- **Realm**: `test-smtp.local`

### TLS Certificates
- Self-signed certificates are automatically generated on first startup
- Certificate CN: `test-smtp.local`
- Subject Alternative Names: `test-smtp.local`, `localhost`, `127.0.0.1`
- **Important**: Use `ignore_tls_errors: true` in tests since certificates are self-signed

## Usage

The Postfix container is automatically started with the other test servers:

```bash
cd test_utils/smtp
docker-compose -f docker-compose.smtp-test.yml up -d
```

## Test Examples

### STARTTLS on port 587
```json
{
  "host": "localhost",
  "port": 1028,
  "use_tls": true,
  "use_direct_tls": false,
  "ignore_tls_errors": true
}
```

### Direct TLS (SMTPS) on port 465
```json
{
  "host": "localhost",
  "port": 1029,
  "use_tls": true,
  "use_direct_tls": true,
  "ignore_tls_errors": true
}
```

### STARTTLS with Authentication
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

### Direct TLS with Authentication
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

## Files

- `Dockerfile`: Container definition
- `main.cf`: Main Postfix configuration
- `master.cf`: Postfix master process configuration (defines services)
- `sasl/smtpd.conf`: SASL authentication configuration
- `entrypoint.sh`: Startup script that generates certificates and creates test user
- `generate-certs.sh`: Helper script to generate certificates manually (optional)

## Troubleshooting

### Container won't start
- Check Docker logs: `docker logs peekaping-test-postfix`
- Verify ports 1027, 1028, 1029 are not in use
- Ensure Docker has sufficient resources

### Authentication fails
- Verify the SASL database was created: `docker exec peekaping-test-postfix ls -la /etc/postfix/sasldb2`
- Check SASL configuration: `docker exec peekaping-test-postfix cat /etc/postfix/sasl/smtpd.conf`

### TLS errors
- Certificates are self-signed, always use `ignore_tls_errors: true` in tests
- Verify certificates exist: `docker exec peekaping-test-postfix ls -la /etc/postfix/certs/`

