package executor

import (
	"context"
	"peekaping/src/modules/shared"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
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

func TestGRPCExecutor_Execute(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewGRPCExecutor(logger)

	tests := []struct {
		name           string
		monitor        *Monitor
		expectedStatus shared.MonitorStatus
		expectMessage  string
		description    string
	}{
		{
			name: "valid gRPC config with keyword match",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "grpc-keyword",
				Name:     "Test gRPC Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"grpcUrl": "localhost:50051",
					"grpcProtobuf": "syntax = \"proto3\";",
					"grpcServiceName": "Health",
					"grpcMethod": "check",
					"keyword": "OK",
					"invertKeyword": false
				}`,
			},
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "keyword [OK] is found",
			description:    "Valid gRPC config with keyword match should return UP status",
		},
		{
			name: "valid gRPC config with keyword mismatch",
			monitor: &Monitor{
				ID:       "monitor2",
				Type:     "grpc-keyword",
				Name:     "Test gRPC Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"grpcUrl": "localhost:50051",
					"grpcProtobuf": "syntax = \"proto3\";",
					"grpcServiceName": "Health",
					"grpcMethod": "check",
					"keyword": "FAIL",
					"invertKeyword": false
				}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "keyword [FAIL] is not in",
			description:    "Valid gRPC config with keyword mismatch should return DOWN status",
		},
		{
			name: "invalid json config",
			monitor: &Monitor{
				ID:       "monitor3",
				Type:     "grpc-keyword",
				Name:     "Test gRPC Monitor",
				Interval: 30,
				Timeout:  5,
				Config:   `{invalid json}`,
			},
			expectedStatus: shared.MonitorStatusDown,
			expectMessage:  "invalid config",
			description:    "Invalid JSON configuration should return DOWN status",
		},
		{
			name: "empty config",
			monitor: &Monitor{
				ID:       "monitor4",
				Type:     "grpc-keyword",
				Name:     "Test gRPC Monitor",
				Interval: 30,
				Timeout:  5,
				Config:   `{}`,
			},
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "gRPC call successful",
			description:    "Empty config uses mock response which succeeds",
		},
		{
			name: "inverted keyword match",
			monitor: &Monitor{
				ID:       "monitor5",
				Type:     "grpc-keyword",
				Name:     "Test gRPC Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"grpcUrl": "localhost:50051",
					"grpcProtobuf": "syntax = \"proto3\";",
					"grpcServiceName": "Health",
					"grpcMethod": "check",
					"keyword": "ERROR",
					"invertKeyword": true
				}`,
			},
			expectedStatus: shared.MonitorStatusUp,
			expectMessage:  "keyword [ERROR] not found",
			description:    "Inverted keyword check (keyword not found) should return UP status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			result := executor.Execute(ctx, tt.monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status, tt.description)
			if tt.expectMessage != "" {
				assert.Contains(t, result.Message, tt.expectMessage, tt.description)
			}
			assert.NotZero(t, result.StartTime)
			assert.NotZero(t, result.EndTime)
			assert.True(t, result.EndTime.After(result.StartTime) || result.EndTime.Equal(result.StartTime))
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
