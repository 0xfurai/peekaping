package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func sendTelegramTo(t *testing.T, handler http.HandlerFunc) error {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	cfg := `{"bot_token": "123456:secret-bot-token", "chat_id": "42", "server_url": "` + server.URL + `"}`

	return NewTelegramSender(zap.NewNop().Sugar(), NewHTTPClient()).Send(context.Background(), cfg, "Monitor is down", testMonitor(), testHeartbeat())
}

func TestTelegramSender_APIErrorReportsTheResponseBody(t *testing.T) {
	err := sendTelegramTo(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	})

	require.Error(t, err)
	assert.Equal(t, `telegram API returned status: 400 Bad Request, body: {"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, err.Error())
}

func TestTelegramSender_APIErrorReportsOnlyTheStartOfALongBody(t *testing.T) {
	err := sendTelegramTo(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("x", 100_000)))
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "telegram API returned status: 502 Bad Gateway, body: xxx")
	assert.Less(t, len(err.Error()), 2_000, "an error message must stay short however long the response is")
}

func TestTelegramSender_APIErrorKeepsTheBotTokenOutOfAnEchoedBody(t *testing.T) {
	// A self-hosted Bot API server (server_url) may echo the request path, which holds the token
	err := sendTelegramTo(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no route for " + r.URL.Path))
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no route for /bot")
	assert.NotContains(t, err.Error(), "secret-bot-token")
}
