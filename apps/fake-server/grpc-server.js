const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');
const fs = require('fs');
const path = require('path');

// Define proto files inline to avoid external dependencies
const HEALTH_PROTO = `
syntax = "proto3";

package grpc.health.v1;

service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}

message HealthCheckRequest {
  string service = 1;
}

message HealthCheckResponse {
  enum ServingStatus {
    UNKNOWN = 0;
    SERVING = 1;
    NOT_SERVING = 2;
    SERVICE_UNKNOWN = 3;
  }
  ServingStatus status = 1;
}
`;

const TEST_PROTO = `
syntax = "proto3";

package test;

service TestService {
  rpc Echo(EchoRequest) returns (EchoResponse);
  rpc GetStatus(StatusRequest) returns (StatusResponse);
  rpc ProcessData(DataRequest) returns (DataResponse);
}

message EchoRequest {
  string message = 1;
}

message EchoResponse {
  string response = 1;
  bool success = 2;
}

message StatusRequest {
  string service_name = 1;
}

message StatusResponse {
  string status = 1;
  string message = 2;
  int32 code = 3;
}

message DataRequest {
  string data = 1;
  int32 count = 2;
}

message DataResponse {
  string result = 1;
  bool processed = 2;
  string error = 3;
}
`;

class GRPCTestServer {
  constructor() {
    this.server = new grpc.Server();
    this.port = process.env.GRPC_PORT || 50051;
    this.tlsPort = process.env.GRPC_TLS_PORT || 50052;

    // Service configurations for different test scenarios
    this.serviceConfigs = {
      health: {
        status: 'SERVING',
        responses: {
          'OK': 'SERVING',
          'ERROR': 'NOT_SERVING',
          'UNKNOWN': 'UNKNOWN'
        }
      },
      test: {
        includeKeywords: ['SUCCESS', 'OK', 'PASSED', 'HEALTHY'],
        excludeKeywords: ['ERROR', 'FAILED', 'DOWN', 'UNHEALTHY']
      }
    };
  }

  // Write proto files to temp directory
  writeProtoFiles() {
    const tempDir = path.join(__dirname, 'temp-protos');
    if (!fs.existsSync(tempDir)) {
      fs.mkdirSync(tempDir, { recursive: true });
    }

    const healthProtoPath = path.join(tempDir, 'health.proto');
    const testProtoPath = path.join(tempDir, 'test.proto');

    fs.writeFileSync(healthProtoPath, HEALTH_PROTO);
    fs.writeFileSync(testProtoPath, TEST_PROTO);

    return { healthProtoPath, testProtoPath };
  }

  // Load proto definitions
  loadProtos() {
    const { healthProtoPath, testProtoPath } = this.writeProtoFiles();

    const healthPackageDefinition = protoLoader.loadSync(healthProtoPath, {
      keepCase: true,
      longs: String,
      enums: String,
      defaults: true,
      oneofs: true,
    });

    const testPackageDefinition = protoLoader.loadSync(testProtoPath, {
      keepCase: true,
      longs: String,
      enums: String,
      defaults: true,
      oneofs: true,
    });

    this.healthProto = grpc.loadPackageDefinition(healthPackageDefinition);
    this.testProto = grpc.loadPackageDefinition(testPackageDefinition);
  }

  // Health service implementation
  healthCheck(call, callback) {
    const request = call.request;
    const serviceName = request.service || '';

    console.log(`Health check requested for service: "${serviceName}"`);

    // Simulate different responses based on service name
    let status = 'SERVING';

    if (serviceName.includes('error') || serviceName.includes('down')) {
      status = 'NOT_SERVING';
    } else if (serviceName.includes('unknown')) {
      status = 'UNKNOWN';
    }

    const response = {
      status: status
    };

    console.log(`Health check response: ${JSON.stringify(response)}`);
    callback(null, response);
  }

  // Test service implementations
  echo(call, callback) {
    const request = call.request;
    const message = request.message || 'Hello World';

    console.log(`Echo request: ${message}`);

    // Include keywords for testing
    const keywords = this.serviceConfigs.test.includeKeywords;
    const randomKeyword = keywords[Math.floor(Math.random() * keywords.length)];

    const response = {
      response: `Echo: ${message} - Status: ${randomKeyword}`,
      success: true
    };

    console.log(`Echo response: ${JSON.stringify(response)}`);
    callback(null, response);
  }

  getStatus(call, callback) {
    const request = call.request;
    const serviceName = request.service_name || 'default';

    console.log(`Status request for service: ${serviceName}`);

    let status = 'OK';
    let message = 'Service is running normally';
    let code = 200;

    // Simulate different responses based on service name
    if (serviceName.includes('error')) {
      status = 'ERROR';
      message = 'Service encountered an error';
      code = 500;
    } else if (serviceName.includes('maintenance')) {
      status = 'MAINTENANCE';
      message = 'Service is under maintenance';
      code = 503;
    } else if (serviceName.includes('slow')) {
      // Simulate slow response
      setTimeout(() => {
        callback(null, {
          status: 'SLOW',
          message: 'Service is responding slowly',
          code: 200
        });
      }, 2000);
      return;
    }

    const response = {
      status: status,
      message: message,
      code: code
    };

    console.log(`Status response: ${JSON.stringify(response)}`);
    callback(null, response);
  }

  processData(call, callback) {
    const request = call.request;
    const data = request.data || '';
    const count = request.count || 1;

    console.log(`Process data request: data="${data}", count=${count}`);

    let result = `Processed ${count} items`;
    let processed = true;
    let error = '';

    // Simulate different processing results
    if (data.includes('fail')) {
      processed = false;
      error = 'Processing failed due to invalid data';
      result = 'FAILED to process data';
    } else if (data.includes('error')) {
      processed = false;
      error = 'An ERROR occurred during processing';
      result = 'ERROR in data processing';
    } else if (count > 100) {
      processed = false;
      error = 'Too many items to process';
      result = 'REJECTED - count exceeds limit';
    } else {
      // Include success keywords
      const keywords = this.serviceConfigs.test.includeKeywords;
      const randomKeyword = keywords[Math.floor(Math.random() * keywords.length)];
      result = `${result} - Status: ${randomKeyword}`;
    }

    const response = {
      result: result,
      processed: processed,
      error: error
    };

    console.log(`Process data response: ${JSON.stringify(response)}`);
    callback(null, response);
  }

  // Configure services
  configureServices() {
    // Health service
    this.server.addService(this.healthProto.grpc.health.v1.Health.service, {
      Check: this.healthCheck.bind(this),
      Watch: (call) => {
        // Simple streaming implementation
        const response = { status: 'SERVING' };
        call.write(response);
        call.end();
      }
    });

    // Test service
    this.server.addService(this.testProto.test.TestService.service, {
      Echo: this.echo.bind(this),
      GetStatus: this.getStatus.bind(this),
      ProcessData: this.processData.bind(this)
    });
  }

  // Start insecure server
  startInsecure() {
    return new Promise((resolve, reject) => {
      this.server.bindAsync(
        `0.0.0.0:${this.port}`,
        grpc.ServerCredentials.createInsecure(),
        (err, port) => {
          if (err) {
            reject(err);
            return;
          }

          this.server.start();
          console.log(`🚀 gRPC server started (insecure) on port ${port}`);
          console.log(`📡 Available services:`);
          console.log(`   - grpc.health.v1.Health`);
          console.log(`   - test.TestService`);
          console.log(`\n📋 Test endpoints:`);
          console.log(`   Health Check: grpcurl -plaintext localhost:${port} grpc.health.v1.Health/Check`);
          console.log(`   Echo: grpcurl -plaintext localhost:${port} test.TestService/Echo`);
          console.log(`   Status: grpcurl -plaintext localhost:${port} test.TestService/GetStatus`);
          console.log(`   Process: grpcurl -plaintext localhost:${port} test.TestService/ProcessData`);
          resolve(port);
        }
      );
    });
  }

  // Start TLS server
  startTLS() {
    const certPath = path.join(__dirname, 'certs', 'server.crt');
    const keyPath = path.join(__dirname, 'certs', 'server.key');

    if (!fs.existsSync(certPath) || !fs.existsSync(keyPath)) {
      console.log('⚠️  TLS certificates not found. Run `npm run generate-certs` first.');
      return Promise.resolve(null);
    }

    const serverCert = fs.readFileSync(certPath);
    const serverKey = fs.readFileSync(keyPath);

    const tlsServer = new grpc.Server();
    this.configureServices.call({ server: tlsServer });

    return new Promise((resolve, reject) => {
      tlsServer.bindAsync(
        `0.0.0.0:${this.tlsPort}`,
        grpc.ServerCredentials.createSsl(null, [{
          cert_chain: serverCert,
          private_key: serverKey
        }]),
        (err, port) => {
          if (err) {
            reject(err);
            return;
          }

          tlsServer.start();
          console.log(`🔒 gRPC TLS server started on port ${port}`);
          resolve(port);
        }
      );
    });
  }

  // Start both servers
  async start() {
    try {
      this.loadProtos();
      this.configureServices();

      const insecurePort = await this.startInsecure();

      // Try to start TLS server if certificates exist
      try {
        await this.startTLS();
      } catch (tlsErr) {
        console.log('⚠️  Could not start TLS server:', tlsErr.message);
      }

      // Graceful shutdown
      process.on('SIGINT', () => {
        console.log('\n🛑 Shutting down gRPC server...');
        this.server.tryShutdown((err) => {
          if (err) {
            console.error('Error during shutdown:', err);
            process.exit(1);
          }
          console.log('✅ gRPC server shut down gracefully');
          process.exit(0);
        });
      });

      console.log(`\n🧪 Example test configurations for PeekaPing:`);
      console.log(`
┌─────────────────────────────────────────────────────────────────────────┐
│ Basic Health Check Test                                                 │
├─────────────────────────────────────────────────────────────────────────┤
│ URL: localhost:${insecurePort}                                                   │
│ Service: Health                                                         │
│ Method: Check                                                           │
│ Keyword: SERVING                                                        │
│ Body: {"service": ""}                                                   │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│ Echo Test with Success Keywords                                         │
├─────────────────────────────────────────────────────────────────────────┤
│ URL: localhost:${insecurePort}                                                   │
│ Service: TestService                                                    │
│ Method: Echo                                                            │
│ Keyword: SUCCESS                                                        │
│ Body: {"message": "test"}                                               │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│ Error Test (Inverted Keywords)                                          │
├─────────────────────────────────────────────────────────────────────────┤
│ URL: localhost:${insecurePort}                                                   │
│ Service: TestService                                                    │
│ Method: GetStatus                                                       │
│ Keyword: ERROR                                                          │
│ Invert: true                                                            │
│ Body: {"service_name": "normal-service"}                                │
└─────────────────────────────────────────────────────────────────────────┘
      `);

    } catch (err) {
      console.error('Failed to start gRPC server:', err);
      process.exit(1);
    }
  }

  // Cleanup temp files
  cleanup() {
    const tempDir = path.join(__dirname, 'temp-protos');
    if (fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true });
    }
  }
}

// Start server if run directly
if (require.main === module) {
  const server = new GRPCTestServer();

  // Cleanup on exit
  process.on('exit', () => {
    server.cleanup();
  });

  process.on('SIGINT', () => {
    server.cleanup();
    process.exit(0);
  });

  server.start().catch(console.error);
}

module.exports = GRPCTestServer;
