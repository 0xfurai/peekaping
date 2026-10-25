package executor

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func rabbitMQMonitor(t *testing.T, timeout int, cfg RabbitMQConfig) *Monitor {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &Monitor{
		ID:      "rabbitmq-test",
		Name:    "RabbitMQ test",
		Type:    "rabbitmq",
		Timeout: timeout,
		Config:  string(raw),
	}
}

// refusedAddr returns the base URL of a port nothing listens on.
func refusedAddr(t *testing.T) string {
	t.Helper()
	// Listen to get a free port, then close the listener so the port refuses.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return "http://" + addr
}

func TestRabbitMQExecutor_NodeNeverAnswers(t *testing.T) {
	// A node that accepts the connection and then says nothing.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})

	exec := NewRabbitMQExecutor(zap.NewNop().Sugar())
	start := time.Now()
	result := exec.Execute(context.Background(), rabbitMQMonitor(t, 2, RabbitMQConfig{
		Nodes:    []string{"http://" + l.Addr().String()},
		Username: "user",
		Password: "password",
	}), nil)
	elapsed := time.Since(start)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "context deadline exceeded")
	assert.Less(t, elapsed, 3*time.Second, "the check must end within the monitor's timeout plus 1s")
}

func TestRabbitMQExecutor_ErrorResponses(t *testing.T) {
	// The alarm body is what RabbitMQ 4.4.0 answers with its memory alarm raised
	// (rabbitmqctl set_vm_memory_high_watermark absolute 1).
	const alarmBody = `{"reason":"There are alarms in effect in the cluster","status":"failed","alarms":[{"node":"rabbit@f195632add83","resource":"memory"}]}`

	tests := []struct {
		name        string
		status      int
		body        string
		wantMessage string
	}{
		{
			name:        "503 with the alarm body gives the alarm's reason",
			status:      http.StatusServiceUnavailable,
			body:        alarmBody,
			wantMessage: "All RabbitMQ nodes failed: RabbitMQ health check failed: There are alarms in effect in the cluster",
		},
		{
			name:        "503 without JSON is service unavailable",
			status:      http.StatusServiceUnavailable,
			body:        "<html>upstream down</html>",
			wantMessage: "All RabbitMQ nodes failed: RabbitMQ health check failed: service unavailable",
		},
		{
			name:        "503 with JSON but no reason is service unavailable",
			status:      http.StatusServiceUnavailable,
			body:        `{"status":"failed"}`,
			wantMessage: "All RabbitMQ nodes failed: RabbitMQ health check failed: service unavailable",
		},
		{
			name:        "another status is unexpected",
			status:      http.StatusInternalServerError,
			body:        `{"error":"boom"}`,
			wantMessage: "All RabbitMQ nodes failed: unexpected status code: 500",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/health/checks/alarms/", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(node.Close)

			exec := NewRabbitMQExecutor(zap.NewNop().Sugar())
			result := exec.Execute(context.Background(), rabbitMQMonitor(t, 5, RabbitMQConfig{
				Nodes:    []string{node.URL},
				Username: "user",
				Password: "password",
			}), nil)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Equal(t, tt.wantMessage, result.Message)
		})
	}
}
