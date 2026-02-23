#!/bin/bash
set -e

CERT_DIR="certs"
mkdir -p "$CERT_DIR"

# Generate private key
openssl genrsa -out "$CERT_DIR/server.key" 2048

# Generate self-signed certificate
openssl req -new -x509 -key "$CERT_DIR/server.key" -out "$CERT_DIR/server.crt" -days 365 \
    -subj "/C=US/ST=Test/L=Test/O=Test/CN=test-smtp.local" \
    -addext "subjectAltName=DNS:test-smtp.local,DNS:localhost,IP:127.0.0.1"

echo "✅ Certificates generated in $CERT_DIR/"
echo "   - server.key (private key)"
echo "   - server.crt (certificate)"

