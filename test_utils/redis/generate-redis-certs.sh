#!/bin/bash

# Script to generate Redis TLS certificates for testing
# This creates self-signed certificates for local testing only

set -e

CERT_DIR="./redis-certs"
mkdir -p "$CERT_DIR"

echo "Generating Redis TLS certificates for testing..."

# Generate CA private key
openssl genrsa -out "$CERT_DIR/ca.key" 2048

# Generate CA certificate
openssl req -new -x509 -days 365 -key "$CERT_DIR/ca.key" -sha256 -out "$CERT_DIR/ca.crt" -subj "/C=US/ST=Test/L=Test/O=Test/CN=Redis-Test-CA"

# Generate server private key
openssl genrsa -out "$CERT_DIR/redis.key" 2048

# Generate server certificate signing request with SAN
cat > "$CERT_DIR/redis.conf" << EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
C = US
ST = Test
L = Test
O = Test
CN = localhost

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = redis
IP.1 = 127.0.0.1
EOF

# Generate server certificate signing request
openssl req -new -key "$CERT_DIR/redis.key" -out "$CERT_DIR/redis.csr" -config "$CERT_DIR/redis.conf"

# Sign server certificate with CA
openssl x509 -req -in "$CERT_DIR/redis.csr" -CA "$CERT_DIR/ca.crt" -CAkey "$CERT_DIR/ca.key" -CAcreateserial -out "$CERT_DIR/redis.crt" -days 365 -sha256 -extfile "$CERT_DIR/redis.conf" -extensions v3_req

# Generate client private key for mutual TLS
openssl genrsa -out "$CERT_DIR/client.key" 2048

# Generate client certificate signing request with SAN
cat > "$CERT_DIR/client.conf" << EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
C = US
ST = Test
L = Test
O = Test
CN = redis-client

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = redis-client
DNS.2 = localhost
IP.1 = 127.0.0.1
EOF

# Generate client certificate signing request
openssl req -new -key "$CERT_DIR/client.key" -out "$CERT_DIR/client.csr" -config "$CERT_DIR/client.conf"

# Sign client certificate with CA
openssl x509 -req -in "$CERT_DIR/client.csr" -CA "$CERT_DIR/ca.crt" -CAkey "$CERT_DIR/ca.key" -CAcreateserial -out "$CERT_DIR/client.crt" -days 365 -sha256 -extfile "$CERT_DIR/client.conf" -extensions v3_req

# Generate additional client certificates for testing different scenarios
# Client 2
cat > "$CERT_DIR/client2.conf" << EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
C = US
ST = Test
L = Test
O = Test
CN = redis-client-2

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = redis-client-2
DNS.2 = localhost
IP.1 = 127.0.0.1
EOF

openssl genrsa -out "$CERT_DIR/client2.key" 2048
openssl req -new -key "$CERT_DIR/client2.key" -out "$CERT_DIR/client2.csr" -config "$CERT_DIR/client2.conf"
openssl x509 -req -in "$CERT_DIR/client2.csr" -CA "$CERT_DIR/ca.crt" -CAkey "$CERT_DIR/ca.key" -CAcreateserial -out "$CERT_DIR/client2.crt" -days 365 -sha256 -extfile "$CERT_DIR/client2.conf" -extensions v3_req

# Client 3 (for testing certificate rotation)
cat > "$CERT_DIR/client3.conf" << EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
C = US
ST = Test
L = Test
O = Test
CN = redis-client-3

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = redis-client-3
DNS.2 = localhost
IP.1 = 127.0.0.1
EOF

openssl genrsa -out "$CERT_DIR/client3.key" 2048
openssl req -new -key "$CERT_DIR/client3.key" -out "$CERT_DIR/client3.csr" -config "$CERT_DIR/client3.conf"
openssl x509 -req -in "$CERT_DIR/client3.csr" -CA "$CERT_DIR/ca.crt" -CAkey "$CERT_DIR/ca.key" -CAcreateserial -out "$CERT_DIR/client3.crt" -days 365 -sha256 -extfile "$CERT_DIR/client3.conf" -extensions v3_req

# Set proper permissions
chmod 600 "$CERT_DIR"/*.key
chmod 644 "$CERT_DIR"/*.crt

# Clean up temporary files
rm -f "$CERT_DIR"/*.csr "$CERT_DIR"/*.conf

echo "Redis TLS certificates generated successfully!"
echo "Certificate files created in: $CERT_DIR"
echo ""
echo "Generated certificates:"
echo "- ca.crt: CA certificate (use as CA Certificate in Peekaping)"
echo "- redis.crt/redis.key: Server certificate and key"
echo "- client.crt/client.key: Client certificate and key (use for mutual TLS)"
echo "- client2.crt/client2.key: Additional client certificate"
echo "- client3.crt/client3.key: Additional client certificate"
echo ""
echo "You can now start the Redis containers with:"
echo "docker-compose -f docker-compose.redis-test.yml up -d"
echo ""
echo "Test connection strings:"
echo "- Simple Redis: redis://localhost:6390"
echo "- Redis with auth: redis://:testpassword@localhost:6388"
echo "- Redis with TLS (ignore cert): rediss://:testpassword@localhost:6389 (set ignoreTls=true)"
echo "- Redis with TLS + CA cert: rediss://:testpassword@localhost:6389 (use ca.crt as CA Certificate)"
echo "- Redis with mutual TLS: rediss://:testpassword@localhost:6389 (use client.crt/client.key)"
echo ""
echo "For Peekaping monitoring with certificates:"
echo "1. Use rediss://:testpassword@localhost:6389 as connection string"
echo "2. Set Ignore TLS to false"
echo "3. Copy ca.crt content to CA Certificate field"
echo "4. Copy client.crt content to Client Certificate field"
echo "5. Copy client.key content to Client Private Key field"
