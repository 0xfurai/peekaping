#!/bin/bash

# Script to test Redis TLS certificates
# This script tests various Redis TLS connection scenarios

set -e

CERT_DIR="./redis-certs"

echo "Testing Redis TLS certificates..."

# Check if certificates exist
if [ ! -f "$CERT_DIR/ca.crt" ] || [ ! -f "$CERT_DIR/client.crt" ] || [ ! -f "$CERT_DIR/client.key" ]; then
    echo "Error: Certificates not found. Please run generate-redis-certs.sh first."
    exit 1
fi

echo "✓ Certificates found"

# Test 1: Simple Redis (no TLS)
echo ""
echo "Test 1: Simple Redis (no TLS)"
if docker exec peekaping-redis-simple-test redis-cli ping > /dev/null 2>&1; then
    echo "✓ Simple Redis connection successful"
else
    echo "✗ Simple Redis connection failed"
fi

# Test 2: Redis with password (no TLS)
echo ""
echo "Test 2: Redis with password (no TLS)"
if docker exec peekaping-redis-test redis-cli -a testpassword ping > /dev/null 2>&1; then
    echo "✓ Redis with password connection successful"
else
    echo "✗ Redis with password connection failed"
fi

# Test 3: Redis with TLS (ignore certificate)
echo ""
echo "Test 3: Redis with TLS (ignore certificate)"
if docker exec peekaping-redis-tls-test redis-cli --tls --insecure -a testpassword ping > /dev/null 2>&1; then
    echo "✓ Redis TLS connection (ignore cert) successful"
else
    echo "✗ Redis TLS connection (ignore cert) failed"
fi

# Test 4: Redis with TLS + CA certificate
echo ""
echo "Test 4: Redis with TLS + CA certificate"
if docker exec peekaping-redis-tls-test redis-cli --tls --cacert /etc/redis/certs/ca.crt -a testpassword ping > /dev/null 2>&1; then
    echo "✓ Redis TLS connection with CA cert successful"
else
    echo "✗ Redis TLS connection with CA cert failed"
fi

# Test 5: Redis with mutual TLS (client certificate)
echo ""
echo "Test 5: Redis with mutual TLS (client certificate)"
if docker exec peekaping-redis-tls-mutual-test redis-cli --tls --cacert /etc/redis/certs/ca.crt --cert /etc/redis/certs/client.crt --key /etc/redis/certs/client.key -a testpassword ping > /dev/null 2>&1; then
    echo "✓ Redis mutual TLS connection successful"
else
    echo "✗ Redis mutual TLS connection failed"
fi

# Test 6: Test from host with certificates
echo ""
echo "Test 6: Testing from host with certificates"
echo "Testing CA certificate validation..."
if redis-cli --tls --cacert "$CERT_DIR/ca.crt" -h localhost -p 6389 -a testpassword ping > /dev/null 2>&1; then
    echo "✓ Host connection with CA cert successful"
else
    echo "✗ Host connection with CA cert failed"
fi

echo ""
echo "Testing mutual TLS from host..."
if redis-cli --tls --cacert "$CERT_DIR/ca.crt" --cert "$CERT_DIR/client.crt" --key "$CERT_DIR/client.key" -h localhost -p 6391 -a testpassword ping > /dev/null 2>&1; then
    echo "✓ Host mutual TLS connection successful"
else
    echo "✗ Host mutual TLS connection failed"
fi

echo ""
echo "Certificate test summary:"
echo "Connection strings for Peekaping testing:"
echo ""
echo "1. Simple Redis:"
echo "   Connection String: redis://localhost:6390"
echo "   Ignore TLS: false"
echo ""
echo "2. Redis with password:"
echo "   Connection String: redis://:testpassword@localhost:6388"
echo "   Ignore TLS: false"
echo ""
echo "3. Redis with TLS (ignore certificate):"
echo "   Connection String: rediss://:testpassword@localhost:6389"
echo "   Ignore TLS: true"
echo ""
echo "4. Redis with TLS + CA certificate:"
echo "   Connection String: rediss://:testpassword@localhost:6389"
echo "   Ignore TLS: false"
echo "   CA Certificate: [Copy content of $CERT_DIR/ca.crt]"
echo ""
echo "5. Redis with mutual TLS:"
echo "   Connection String: rediss://:testpassword@localhost:6391"
echo "   Ignore TLS: false"
echo "   CA Certificate: [Copy content of $CERT_DIR/ca.crt]"
echo "   Client Certificate: [Copy content of $CERT_DIR/client.crt]"
echo "   Client Private Key: [Copy content of $CERT_DIR/client.key]"
echo ""
echo "Certificate files:"
ls -la "$CERT_DIR"/
