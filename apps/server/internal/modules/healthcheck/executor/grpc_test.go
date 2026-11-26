//go:build !slim

package executor

import (
	"context"
	"encoding/json"
	"net"
	"peekaping/internal/modules/shared"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func TestGRPCExecutor_Unmarshal(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewGRPCExecutor(logger)

	tests := []struct {
		name                    string
		config                  string
		expectedError           bool
		expectedGrpcUrl         string
		expectedGrpcServiceName string
		expectedGrpcMethod      string
		expectedKeyword         string
		expectedInvertKeyword   bool
	}{
		{
			name: "valid gRPC config",
			config: `{
				"grpcUrl": "localhost:50051",
				"grpcProtobuf": "syntax = \"proto3\";",
				"grpcServiceName": "Health",
				"grpcMethod": "check",
				"grpcEnableTls": false,
				"grpcBody": "{\"key\": \"value\"}",
				"keyword": "OK",
				"invertKeyword": false
			}`,
			expectedError:           false,
			expectedGrpcUrl:         "localhost:50051",
			expectedGrpcServiceName: "Health",
			expectedGrpcMethod:      "check",
			expectedKeyword:         "OK",
			expectedInvertKeyword:   false,
		},
		{
			name: "valid gRPC config with TLS",
			config: `{
				"grpcUrl": "grpc.example.com:443",
				"grpcProtobuf": "syntax = \"proto3\";",
				"grpcServiceName": "MyService",
				"grpcMethod": "testMethod",
				"grpcEnableTls": true,
				"grpcBody": "",
				"keyword": "success",
				"invertKeyword": true
			}`,
			expectedError:           false,
			expectedGrpcUrl:         "grpc.example.com:443",
			expectedGrpcServiceName: "MyService",
			expectedGrpcMethod:      "testMethod",
			expectedKeyword:         "success",
			expectedInvertKeyword:   true,
		},
		{
			name: "invalid json",
			config: `{
				"grpcUrl": "localhost:50051",
				invalid_json
			}`,
			expectedError: true,
		},
		{
			name:          "empty config",
			config:        `{}`,
			expectedError: false,
		},
		{
			name: "config with unknown field",
			config: `{
				"grpcUrl": "localhost:50051",
				"grpcProtobuf": "syntax = \"proto3\";",
				"grpcServiceName": "Health",
				"grpcMethod": "check",
				"unknown_field": "value"
			}`,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Unmarshal(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.expectedGrpcUrl != "" {
					cfg := result.(*GRPCConfig)
					assert.Equal(t, tt.expectedGrpcUrl, cfg.GrpcUrl)
					assert.Equal(t, tt.expectedGrpcServiceName, cfg.GrpcServiceName)
					assert.Equal(t, tt.expectedGrpcMethod, cfg.GrpcMethod)
					assert.Equal(t, tt.expectedKeyword, cfg.Keyword)
					assert.Equal(t, tt.expectedInvertKeyword, cfg.InvertKeyword)
				}
			}
		})
	}
}

func TestGRPCExecutor_Validate(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewGRPCExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
		description   string
	}{
		{
			name: "valid gRPC config",
			config: `{
				"grpcUrl": "localhost:50051",
				"grpcProtobuf": "syntax = \"proto3\";",
				"grpcServiceName": "Health",
				"grpcMethod": "check"
			}`,
			expectedError: false,
			description:   "Valid gRPC configuration",
		},
		{
			name: "missing grpcUrl",
			config: `{
				"grpcProtobuf": "syntax = \"proto3\";",
				"grpcServiceName": "Health",
				"grpcMethod": "check"
			}`,
			expectedError: true,
			description:   "grpcUrl is required",
		},
		{
			name: "empty grpcUrl",
			config: `{
				"grpcUrl": "",
				"grpcProtobuf": "syntax = \"proto3\";",
				"grpcServiceName": "Health",
				"grpcMethod": "check"
			}`,
			expectedError: true,
			description:   "grpcUrl cannot be empty",
		},
		{
			name: "missing grpcProtobuf",
			config: `{
				"grpcUrl": "localhost:50051",
				"grpcServiceName": "Health",
				"grpcMethod": "check"
			}`,
			expectedError: true,
			description:   "grpcProtobuf is required",
		},
		{
			name: "missing grpcServiceName",
			config: `{
				"grpcUrl": "localhost:50051",
				"grpcProtobuf": "syntax = \"proto3\";",
				"grpcMethod": "check"
			}`,
			expectedError: true,
			description:   "grpcServiceName is required",
		},
		{
			name: "missing grpcMethod",
			config: `{
				"grpcUrl": "localhost:50051",
				"grpcProtobuf": "syntax = \"proto3\";",
				"grpcServiceName": "Health"
			}`,
			expectedError: true,
			description:   "grpcMethod is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.config)
			if tt.expectedError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// healthProto is the proto that the monitor form fills in for a new gRPC monitor.
const healthProto = `syntax = "proto3";

package grpc.health.v1;

service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}`

// startHealthServer serves the standard gRPC health service on a loopback port.
func startHealthServer(t *testing.T) (string, *health.Server) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	hs := health.NewServer()
	healthpb.RegisterHealthServer(srv, hs)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	return lis.Addr().String(), hs
}

func grpcMonitor(t *testing.T, cfg GRPCConfig) *Monitor {
	t.Helper()
	b, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &Monitor{ID: "grpc", Type: "grpc-keyword", Name: "gRPC", Timeout: 2, Config: string(b)}
}

func TestGRPCExecutor_Execute(t *testing.T) {
	executor := NewGRPCExecutor(zap.NewNop().Sugar())
	addr, hs := startHealthServer(t)
	hs.SetServingStatus("peekaping.Worker", healthpb.HealthCheckResponse_NOT_SERVING)

	// healthMonitor starts from the monitor form's defaults: a lower-case method
	// name and a body with no HealthCheckRequest fields, so the server reports
	// its overall status.
	healthMonitor := func(edit func(*GRPCConfig)) *Monitor {
		cfg := GRPCConfig{
			GrpcUrl:         addr,
			GrpcProtobuf:    healthProto,
			GrpcServiceName: "Health",
			GrpcMethod:      "check",
			GrpcBody:        `{"key": "value"}`,
		}
		edit(&cfg)
		return grpcMonitor(t, cfg)
	}

	tests := []struct {
		name        string
		monitor     *Monitor
		wantStatus  shared.MonitorStatus
		wantMessage string
	}{
		{
			name:        "serving",
			monitor:     healthMonitor(func(c *GRPCConfig) {}),
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "SERVING",
		},
		{
			name:        "keyword found",
			monitor:     healthMonitor(func(c *GRPCConfig) { c.Keyword = "SERVING" }),
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "keyword [SERVING] is found",
		},
		{
			name:        "keyword missing",
			monitor:     healthMonitor(func(c *GRPCConfig) { c.Keyword = "NOT_SERVING" }),
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "keyword [NOT_SERVING] is not in",
		},
		{
			name:        "inverted keyword absent",
			monitor:     healthMonitor(func(c *GRPCConfig) { c.Keyword, c.InvertKeyword = "NOT_SERVING", true }),
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "keyword [NOT_SERVING] not found",
		},
		{
			name: "inverted keyword present for the service in the body",
			monitor: healthMonitor(func(c *GRPCConfig) {
				c.GrpcBody = `{"service": "peekaping.Worker"}`
				c.Keyword, c.InvertKeyword = "NOT_SERVING", true
			}),
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "keyword [NOT_SERVING] is present",
		},
		{
			name: "method spelled differently by another service",
			monitor: healthMonitor(func(c *GRPCConfig) {
				c.GrpcProtobuf = strings.Replace(healthProto, "service Health {", `service Legacy {
  rpc check(HealthCheckRequest) returns (HealthCheckResponse);
}

service Health {`, 1)
			}),
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "SERVING",
		},
		{
			name: "method declared after an rpc with options",
			monitor: healthMonitor(func(c *GRPCConfig) {
				c.GrpcProtobuf = `syntax = "proto3";

package grpc.health.v1;

service Health {
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse) {}
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
}`
			}),
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "SERVING",
		},
		{
			name:        "unknown method",
			monitor:     healthMonitor(func(c *GRPCConfig) { c.GrpcMethod = "Ping" }),
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "code = Unimplemented",
		},
		{
			name:        "method name with regexp metacharacters",
			monitor:     healthMonitor(func(c *GRPCConfig) { c.GrpcMethod = "Check(" }),
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "code = Unimplemented",
		},
		{
			name:        "unreachable target",
			monitor:     healthMonitor(func(c *GRPCConfig) { c.GrpcUrl = closedAddr(t) }),
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "code = Unavailable",
		},
		{
			name:        "invalid json config",
			monitor:     &Monitor{ID: "grpc", Type: "grpc-keyword", Name: "gRPC", Timeout: 2, Config: `{invalid json}`},
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "invalid config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.Execute(context.Background(), tt.monitor, nil)
			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			assert.Contains(t, result.Message, tt.wantMessage)
		})
	}
}

func TestNewGRPCExecutor(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()

	// Test executor creation
	executor := NewGRPCExecutor(logger)

	// Verify executor is properly initialized
	assert.NotNil(t, executor)
	assert.NotNil(t, executor.logger)
}

func TestGRPCExecutor_Interface(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewGRPCExecutor(logger)

	// Verify that GRPCExecutor implements the Executor interface
	var _ Executor = executor
}
