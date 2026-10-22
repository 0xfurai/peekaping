package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"

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

type telegramTransport func(*http.Request) (*http.Response, error)

func (f telegramTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// A Telegram config without a server_url sends to the Telegram API. The endpoint check treats an
// empty server_url as that server, using the endpointDefault tag, so the tag has to name the server
// the sender falls back to.
func TestTelegramEmptyServerURLSendsToTheEndpointDefault(t *testing.T) {
	var requested *url.URL
	client := &http.Client{Transport: telegramTransport(func(req *http.Request) (*http.Response, error) {
		requested = req.URL
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: http.Header{}}, nil
	})}
	sender := NewTelegramSender(zap.NewNop().Sugar(), client)

	err := sender.Send(context.Background(), `{"bot_token":"token","chat_id":"1"}`, "message", &monitor.Model{}, &heartbeat.Model{})

	require.NoError(t, err)
	require.NotNil(t, requested)
	field, ok := reflect.TypeOf(TelegramConfig{}).FieldByName("ServerUrl")
	require.True(t, ok)
	assert.Equal(t, field.Tag.Get("endpointDefault"), requested.Scheme+"://"+requested.Host)
	assert.Equal(t, "/bottoken/sendMessage", requested.Path)
}
