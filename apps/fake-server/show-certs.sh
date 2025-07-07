#!/bin/bash

# Display certificate contents for copying into PeekaPing UI
set -e

CERT_DIR="certs"

if [ ! -d "$CERT_DIR" ]; then
  echo "❌ Certificates not found! Please run: npm run generate-certs"
  exit 1
fi

echo "📋 mTLS Certificate Contents for PeekaPing"
echo "========================================="
echo ""

echo "🔑 CLIENT CERTIFICATE (paste into 'Certificate' field):"
echo "┌────────────────────────────────────────────────────────────────┐"
cat $CERT_DIR/client.crt
echo "└────────────────────────────────────────────────────────────────┘"
echo ""

echo "🔐 CLIENT PRIVATE KEY (paste into 'Key' field):"
echo "┌────────────────────────────────────────────────────────────────┐"
cat $CERT_DIR/client.key
echo "└────────────────────────────────────────────────────────────────┘"
echo ""

echo "🏢 CA CERTIFICATE (paste into 'CA' field):"
echo "┌────────────────────────────────────────────────────────────────┐"
cat $CERT_DIR/ca.crt
echo "└────────────────────────────────────────────────────────────────┘"
echo ""

echo "✅ Copy the contents above (including -----BEGIN/END----- lines)"
echo "📝 Paste them into PeekaPing monitor mTLS authentication fields"
echo "🎯 Test URL: https://localhost:3443/"
