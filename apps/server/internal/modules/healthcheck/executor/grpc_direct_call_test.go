//go:build !slim

package executor

import (
	"context"
	"net"
	"peekaping/internal/modules/shared"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// startEchoServer serves every method of every service by echoing the first string field of the
// request back, prefixed with "echo:", except for server reflection, which it doesn't have. It reports the full name of the method it was called with.
func startEchoServer(t *testing.T) (string, <-chan string) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	methods := make(chan string, 4)
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		method, _ := grpc.MethodFromServerStream(stream)
		if strings.Contains(method, "ServerReflection") {
			return status.Error(codes.Unimplemented, "no reflection")
		}
		methods <- method
		var req wrapperspb.StringValue
		if err := stream.RecvMsg(&req); err != nil {
			return err
		}
		return stream.SendMsg(&wrapperspb.StringValue{Value: "echo:" + req.Value})
	}))
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	return lis.Addr().String(), methods
}

func TestGRPCExecutor_Execute_DirectCallTypes(t *testing.T) {
	executor := NewGRPCExecutor(zap.NewNop().Sugar())
	healthAddr, _ := startHealthServer(t)
	echoAddr, methods := startEchoServer(t)

	tests := []struct {
		name        string
		cfg         GRPCConfig
		wantStatus  shared.MonitorStatus
		wantMessage string
		wantMethod  string
	}{
		{
			name:        "service the proto doesn't declare, with the default message types",
			cfg:         GRPCConfig{GrpcUrl: echoAddr, GrpcProtobuf: `syntax = "proto3";`, GrpcServiceName: "Echo", GrpcMethod: "Say", GrpcBody: `{"data": "hello"}`},
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: `gRPC call successful: {"data":"echo:hello"}`,
			wantMethod:  "/Echo/Say",
		},
		{
			name:        "service in a package",
			cfg:         GRPCConfig{GrpcUrl: echoAddr, GrpcProtobuf: "syntax = \"proto3\";\npackage demo.v1;", GrpcServiceName: "Echo", GrpcMethod: "Say", GrpcBody: `{"data": "hi"}`},
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: `gRPC call successful: {"data":"echo:hi"}`,
			wantMethod:  "/demo.v1.Echo/Say",
		},
		{
			name:        "message types the proto declares",
			cfg:         GRPCConfig{GrpcUrl: echoAddr, GrpcProtobuf: "package demo.v1;\nservice Echo {\n  rpc Say(Ping) returns (Pong);\n}", GrpcServiceName: "Echo", GrpcMethod: "say", GrpcBody: `{"data": "x"}`},
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: `gRPC call successful: {"data":"echo:x"}`,
			wantMethod:  "/demo.v1.Echo/Say",
		},
		{
			name:        "Health service the proto doesn't declare, with the default message types",
			cfg:         GRPCConfig{GrpcUrl: healthAddr, GrpcProtobuf: "package grpc.health.v1;", GrpcServiceName: "Health", GrpcMethod: "Check", GrpcBody: `{"service": ""}`},
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "SERVING",
		},
		{
			name:        "empty body",
			cfg:         GRPCConfig{GrpcUrl: healthAddr, GrpcProtobuf: healthProto, GrpcServiceName: "Health", GrpcMethod: "Check"},
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "SERVING",
		},
		{
			name:        "body that is not JSON",
			cfg:         GRPCConfig{GrpcUrl: healthAddr, GrpcProtobuf: healthProto, GrpcServiceName: "Health", GrpcMethod: "Check", GrpcBody: "not json"},
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "Error in send gRPC: failed to unmarshal request body: ",
		},
		{
			name:        "message types that have no descriptor",
			cfg:         GRPCConfig{GrpcUrl: healthAddr, GrpcProtobuf: "package demo.v1;\nservice Echo {\n  rpc Say(demo.Ping) returns (demo.Pong);\n}", GrpcServiceName: "Echo", GrpcMethod: "Say"},
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "Error in send gRPC: failed to create message descriptors for demo.Ping and demo.Pong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.Execute(context.Background(), grpcMonitor(t, tt.cfg), nil)

			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			assert.Contains(t, result.Message, tt.wantMessage)
			if tt.wantMethod != "" {
				assert.Equal(t, tt.wantMethod, <-methods)
			}
		})
	}
}
