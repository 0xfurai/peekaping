//go:build !slim

package executor

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

const (
	mongoOpReply = 1
	mongoOpQuery = 2004
	mongoOpMsg   = 2013
)

// startFakeMongo serves the MongoDB wire protocol on a loopback port as a standalone server: it
// answers the driver's handshake, and any other command with what reply returns for it. It
// returns the address.
func startFakeMongo(t *testing.T, reply func(command string) bson.M) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })

	hello := bson.M{
		"ismaster":            true,
		"isWritablePrimary":   true,
		"helloOk":             true,
		"maxBsonObjectSize":   int32(16777216),
		"maxMessageSizeBytes": int32(48000000),
		"maxWriteBatchSize":   int32(100000),
		"minWireVersion":      int32(0),
		"maxWireVersion":      int32(17),
		"ok":                  float64(1),
	}

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go serveFakeMongo(conn, hello, reply)
		}
	}()
	return lis.Addr().String()
}

func serveFakeMongo(conn net.Conn, hello bson.M, reply func(command string) bson.M) {
	defer conn.Close()
	for {
		var header [16]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return
		}
		length := binary.LittleEndian.Uint32(header[0:4])
		requestID := binary.LittleEndian.Uint32(header[4:8])
		opCode := binary.LittleEndian.Uint32(header[12:16])
		body := make([]byte, length-16)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}

		var doc bson.D
		switch opCode {
		case mongoOpQuery:
			// flags (4), collection name (cstring), skip (4), return (4), query
			rest := body[4:]
			for i, b := range rest {
				if b == 0 {
					rest = rest[i+1+8:]
					break
				}
			}
			if bson.Unmarshal(rest, &doc) != nil {
				return
			}
		case mongoOpMsg:
			// flag bits (4), then a section of kind 0 holding the command
			if body[4] != 0 || bson.Unmarshal(body[5:], &doc) != nil {
				return
			}
		default:
			return
		}

		// The command is the first element of the document.
		command := doc[0].Key
		response := hello
		if command != "hello" && command != "isMaster" && command != "ismaster" {
			response = reply(command)
		}
		payload, err := bson.Marshal(response)
		if err != nil {
			return
		}

		var out []byte
		if opCode == mongoOpQuery {
			// response flags (4), cursor id (8), starting from (4), number returned (4), document
			out = append(out, 8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0)
		} else {
			out = append(out, 0, 0, 0, 0, 0) // flag bits, section kind 0
		}
		out = append(out, payload...)
		replyOp := uint32(mongoOpMsg)
		if opCode == mongoOpQuery {
			replyOp = mongoOpReply
		}
		var outHeader [16]byte
		binary.LittleEndian.PutUint32(outHeader[0:4], uint32(16+len(out)))
		binary.LittleEndian.PutUint32(outHeader[8:12], requestID)
		binary.LittleEndian.PutUint32(outHeader[12:16], replyOp)
		if _, err := conn.Write(append(outHeader[:], out...)); err != nil {
			return
		}
	}
}

func TestMongoDBExecutor_Execute_CommandOutcomes(t *testing.T) {
	addr := startFakeMongo(t, func(command string) bson.M {
		switch command {
		case "ping":
			return bson.M{"ok": float64(1)}
		case "serverStatus":
			return bson.M{"ok": float64(1), "version": "7.0.5", "connections": bson.M{"current": int32(12), "note": nil}}
		case "weird":
			return bson.M{"ok": float64(2)}
		case "refused":
			return bson.M{"ok": float64(0), "errmsg": "not allowed", "code": int32(13), "codeName": "Unauthorized"}
		}
		return bson.M{"ok": float64(1)}
	})
	connectionString := "mongodb://" + addr + "/admin?directConnection=true"

	tests := []struct {
		name        string
		cfg         MongoDBConfig
		wantStatus  shared.MonitorStatus
		wantMessage string
	}{
		{name: "default ping", cfg: MongoDBConfig{}, wantStatus: shared.MonitorStatusUp, wantMessage: "Command executed successfully"},
		{name: "command without a path", cfg: MongoDBConfig{Command: `{"serverStatus": 1}`}, wantStatus: shared.MonitorStatusUp, wantMessage: "Command executed successfully"},
		{name: "ok is not 1", cfg: MongoDBConfig{Command: `{"weird": 1}`}, wantStatus: shared.MonitorStatusDown, wantMessage: "MongoDB command failed"},
		{
			name: "server refuses the command", cfg: MongoDBConfig{Command: `{"refused": 1}`}, wantStatus: shared.MonitorStatusDown,
			wantMessage: "MongoDB command failed: failed to execute MongoDB command: (Unauthorized) not allowed",
		},
		{name: "path to a value", cfg: MongoDBConfig{Command: `{"serverStatus": 1}`, JsonPath: "$.version"}, wantStatus: shared.MonitorStatusUp, wantMessage: "Command executed successfully and the jsonata expression produces a result"},
		{name: "path to a nested value", cfg: MongoDBConfig{Command: `{"serverStatus": 1}`, JsonPath: "connections.current", ExpectedValue: "12"}, wantStatus: shared.MonitorStatusUp, wantMessage: "Command executed successfully and expected value was found"},
		{name: "path to a missing field", cfg: MongoDBConfig{Command: `{"serverStatus": 1}`, JsonPath: "$.missing"}, wantStatus: shared.MonitorStatusDown, wantMessage: "JSON path evaluation failed: field 'missing' not found"},
		{name: "path through a value that isn't an object", cfg: MongoDBConfig{Command: `{"serverStatus": 1}`, JsonPath: "version.major"}, wantStatus: shared.MonitorStatusDown, wantMessage: "JSON path evaluation failed: cannot access field 'major' on non-object type"},
		{name: "path to null", cfg: MongoDBConfig{Command: `{"serverStatus": 1}`, JsonPath: "connections.note"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Queried value not found"},
		{name: "value differs", cfg: MongoDBConfig{Command: `{"serverStatus": 1}`, JsonPath: "version", ExpectedValue: "6.0"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Query executed, but value is not equal to expected value, value was: [7.0.5]"},
		{name: "value matches", cfg: MongoDBConfig{Command: `{"serverStatus": 1}`, JsonPath: "version", ExpectedValue: "7.0.5"}, wantStatus: shared.MonitorStatusUp, wantMessage: "Command executed successfully and expected value was found"},
		{name: "root path with an expected value", cfg: MongoDBConfig{Command: `{"serverStatus": 1}`, JsonPath: "$", ExpectedValue: "x"}, wantStatus: shared.MonitorStatusDown, wantMessage: "Query executed, but value is not equal to expected value, value was: [map["},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.ConnectionString = connectionString

			result := NewMongoDBExecutor(zap.NewNop().Sugar()).Execute(context.Background(), mongoMonitor(t, 5, tt.cfg), nil)

			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			assert.Contains(t, result.Message, tt.wantMessage)
			assert.False(t, result.EndTime.Before(result.StartTime))
		})
	}
}

func TestMongoDBExecutor_Execute_RunsTheConfiguredCommand(t *testing.T) {
	received := make(chan string, 8)
	addr := startFakeMongo(t, func(command string) bson.M {
		if command != "endSessions" {
			received <- command
		}
		return bson.M{"ok": float64(1)}
	})

	for command, want := range map[string]string{"": "ping", `{"buildInfo": 1}`: "buildInfo"} {
		result := NewMongoDBExecutor(zap.NewNop().Sugar()).Execute(context.Background(), mongoMonitor(t, 5, MongoDBConfig{
			ConnectionString: "mongodb://" + addr + "/admin?directConnection=true",
			Command:          command,
		}), nil)

		require.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		select {
		case got := <-received:
			assert.Equal(t, want, got)
		case <-time.After(5 * time.Second):
			t.Fatal("the server got no command")
		}
	}
}
