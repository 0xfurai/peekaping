#!/bin/bash
# Don't exit on errors for non-critical commands
set +e

# Generate certificates if they don't exist
if [ ! -f /etc/postfix/certs/server.crt ] || [ ! -f /etc/postfix/certs/server.key ]; then
    echo "Generating self-signed certificates..."
    mkdir -p /etc/postfix/certs
    openssl genrsa -out /etc/postfix/certs/server.key 2048
    openssl req -new -x509 -key /etc/postfix/certs/server.key \
        -out /etc/postfix/certs/server.crt -days 365 \
        -subj "/C=US/ST=Test/L=Test/O=Test/CN=test-smtp.local" \
        -addext "subjectAltName=DNS:test-smtp.local,DNS:localhost,IP:127.0.0.1"
    chmod 600 /etc/postfix/certs/server.key
    chmod 644 /etc/postfix/certs/server.crt
fi

# Create SASL database with test user
# saslpasswd2 creates the database in /etc/sasldb2 by default
# Always recreate to ensure it's correct (for testing purposes)
echo "Creating/updating SASL database with test user..."
rm -f /etc/sasldb2
echo "testpass" | saslpasswd2 -c -p -u test-smtp.local testuser
chown postfix:postfix /etc/sasldb2
chmod 660 /etc/sasldb2

# Verify user was created
if sasldblistusers2 | grep -q "testuser@test-smtp.local"; then
    echo "SASL user created successfully"
else
    echo "Warning: SASL user may not have been created correctly"
    sasldblistusers2
fi

# Postfix runs in chroot at /var/spool/postfix, so we need to copy the SASL database there
# Create the etc directory in chroot if it doesn't exist
mkdir -p /var/spool/postfix/etc
cp /etc/sasldb2 /var/spool/postfix/etc/sasldb2
chown postfix:postfix /var/spool/postfix/etc/sasldb2
chmod 660 /var/spool/postfix/etc/sasldb2

# Also create symlink in postfix directory for easier access
ln -sf /etc/sasldb2 /etc/postfix/sasldb2 || true

# Set permissions
chown -R postfix:postfix /etc/postfix/certs
chmod 755 /etc/postfix/certs

# Ensure SASL configuration directory exists
mkdir -p /etc/postfix/sasl
chown -R postfix:postfix /etc/postfix/sasl

# Start Postfix
echo "Starting Postfix..."
# set-permissions may fail on some files (like man pages), but that's OK
postfix set-permissions 2>/dev/null || true

# Check configuration (warn but don't fail)
if ! postfix check 2>&1; then
    echo "Warning: Postfix configuration check had issues, but continuing..."
fi

# Start Postfix in background
echo "Starting Postfix daemon..."
postfix start 2>&1 || echo "Note: postfix start returned non-zero, but this may be normal"

# Give Postfix time to start
sleep 5

# Verify Postfix is running
if postfix status >/dev/null 2>&1; then
    echo "✅ Postfix is running successfully"
    # Execute the command (usually a keep-alive command)
    exec "$@"
else
    echo "❌ Postfix status check failed:"
    postfix status 2>&1 || true
    echo "Attempting to start Postfix again..."
    postfix start 2>&1 || true
    sleep 3
    if postfix status >/dev/null 2>&1; then
        echo "✅ Postfix started on second attempt"
        exec "$@"
    else
        echo "❌ Postfix still not running, but keeping container alive for debugging"
        exec "$@"
    fi
fi

