const https = require('https');
const fs = require('fs');
const path = require('path');
const express = require('express');

const app = express();
const port = 3443; // HTTPS port

// Middleware
app.use(express.json());
app.use(express.urlencoded({ extended: true }));

// Enhanced logger that shows certificate info
const mtlsLogger = (req, res, next) => {
  const start = performance.now();

  res.on('finish', () => {
    const ms = (performance.now() - start).toFixed(1);
    const clientCert = req.socket.getPeerCertificate();
    const certInfo = clientCert && Object.keys(clientCert).length > 0
      ? `cert: ${clientCert.subject?.CN || 'unknown'}`
      : 'no cert';

    console.log(
      `${req.method} ${req.originalUrl} → ${res.statusCode} (${ms} ms) [${certInfo}]`
    );
  });

  next();
};

app.use(mtlsLogger);

// Main handler that returns certificate details
const mtlsHandler = (req, res) => {
  const clientCert = req.socket.getPeerCertificate();
  const delay = parseInt(req.query.delay, 10) || 100;

  setTimeout(() => {
    const response = {
      status: "ok",
      message: "mTLS connection successful",
      timestamp: new Date().toISOString(),
      delay: delay,
      client_certificate: clientCert && Object.keys(clientCert).length > 0 ? {
        subject: clientCert.subject,
        issuer: clientCert.issuer,
        valid_from: clientCert.valid_from,
        valid_to: clientCert.valid_to,
        fingerprint: clientCert.fingerprint,
        serial_number: clientCert.serialNumber
      } : null,
      headers: req.headers,
      method: req.method,
      url: req.url
    };

    res.json(response);
  }, delay);
};

// Routes
app.get('/', mtlsHandler);
app.post('/', mtlsHandler);
app.put('/', mtlsHandler);
app.delete('/', mtlsHandler);
app.patch('/', mtlsHandler);
app.head('/', mtlsHandler);
app.options('/', mtlsHandler);

// Health check endpoint (no cert required)
app.get('/health', (req, res) => {
  res.json({ status: 'healthy', message: 'Server is running' });
});

// Different test endpoints
app.get('/require-cert', (req, res) => {
  const clientCert = req.socket.getPeerCertificate();
  if (!clientCert || Object.keys(clientCert).length === 0) {
    return res.status(401).json({
      error: 'Client certificate required but not provided'
    });
  }
  res.json({ message: 'Certificate validated successfully', cert: clientCert.subject });
});

app.get('/optional-cert', (req, res) => {
  const clientCert = req.socket.getPeerCertificate();
  res.json({
    message: 'This endpoint works with or without certificates',
    has_cert: clientCert && Object.keys(clientCert).length > 0
  });
});

// Error handlers
app.use((err, req, res, next) => {
  console.error('Error:', err);
  res.status(500).json({ error: 'Internal Server Error', details: err.message });
});

// Check if certificates exist
const certDir = path.join(__dirname, 'certs');
const serverKey = path.join(certDir, 'server.key');
const serverCert = path.join(certDir, 'server.crt');
const caCert = path.join(certDir, 'ca.crt');

if (!fs.existsSync(serverKey) || !fs.existsSync(serverCert) || !fs.existsSync(caCert)) {
  console.error('❌ Certificates not found! Please run: npm run generate-certs');
  process.exit(1);
}

// HTTPS server options with mTLS
const options = {
  key: fs.readFileSync(serverKey),
  cert: fs.readFileSync(serverCert),
  ca: fs.readFileSync(caCert),
  requestCert: true,           // Request client certificate
  rejectUnauthorized: false,   // Don't reject, but make cert available for inspection
};

// Create HTTPS server
const server = https.createServer(options, app);

server.listen(port, () => {
  console.log(`🔒 mTLS Test Server running on https://localhost:${port}`);
  console.log(`📋 Available endpoints:`);
  console.log(`   GET  https://localhost:${port}/           - Main endpoint (cert info)`);
  console.log(`   GET  https://localhost:${port}/health     - Health check (no cert needed)`);
  console.log(`   GET  https://localhost:${port}/require-cert - Requires valid client cert`);
  console.log(`   GET  https://localhost:${port}/optional-cert - Works with/without cert`);
  console.log(`\n💡 Use query param ?delay=X to add artificial delay (ms)`);
});

// Graceful shutdown
process.on('SIGTERM', () => {
  console.log('\n🛑 Received SIGTERM, shutting down gracefully...');
  server.close(() => {
    console.log('✅ Server closed');
    process.exit(0);
  });
});

process.on('SIGINT', () => {
  console.log('\n🛑 Received SIGINT, shutting down gracefully...');
  server.close(() => {
    console.log('✅ Server closed');
    process.exit(0);
  });
});
