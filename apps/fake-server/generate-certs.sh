#!/bin/bash

# Generate mTLS certificates for testing
set -e

CERT_DIR="certs"
DAYS=365

echo "🔐 Generating mTLS certificates for testing..."

# Create certs directory
mkdir -p $CERT_DIR
cd $CERT_DIR

# Generate CA private key
echo "📝 Generating CA private key..."
openssl genrsa -out ca.key 2048

# Generate CA certificate
echo "📝 Generating CA certificate..."
openssl req -new -x509 -days $DAYS -key ca.key -out ca.crt \
  -subj "/C=US/ST=Test/L=Test/O=PeekaPing Test CA/CN=Test CA"

# Generate server private key
echo "📝 Generating server private key..."
openssl genrsa -out server.key 2048

# Generate server certificate signing request
echo "📝 Generating server CSR..."
openssl req -new -key server.key -out server.csr \
  -subj "/C=US/ST=Test/L=Test/O=PeekaPing Test Server/CN=localhost"

# Create server certificate extensions file
cat > server.ext << 'EOF'
basicConstraints=CA:FALSE
keyUsage=nonRepudiation,digitalSignature,keyEncipherment
subjectAltName=@alt_names

[alt_names]
DNS.1=localhost
DNS.2=*.localhost
IP.1=127.0.0.1
IP.2=::1
EOF

# Generate server certificate signed by CA
echo "📝 Generating server certificate..."
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days $DAYS -extfile server.ext

# Generate client private key
echo "📝 Generating client private key..."
openssl genrsa -out client.key 2048

# Generate client certificate signing request
echo "📝 Generating client CSR..."
openssl req -new -key client.key -out client.csr \
  -subj "/C=US/ST=Test/L=Test/O=PeekaPing Test Client/CN=Test Client"

# Generate client certificate signed by CA
echo "📝 Generating client certificate..."
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt -days $DAYS

# Clean up CSR files and extensions
rm -f server.csr client.csr server.ext

# Set proper permissions
chmod 600 *.key
chmod 644 *.crt

echo "✅ Certificates generated successfully!"
echo ""
echo "📁 Files created in $CERT_DIR/:"
ls -la

echo ""
echo "🔍 Certificate info:"
echo "   CA Certificate: ca.crt"
echo "   Server Certificate: server.crt (for localhost)"
echo "   Client Certificate: client.crt"
echo ""
echo "📋 For PeekaPing mTLS monitor, use:"
echo "   Certificate: $(pwd)/client.crt"
echo "   Key: $(pwd)/client.key"
echo "   CA: $(pwd)/ca.crt"
echo ""
echo "🚀 Now you can start the mTLS server with: npm run mtls"
