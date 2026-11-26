package apitest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"peekaping/internal/httpx"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHTTPRoutingCompatibility(t *testing.T) {
	a := newApp(t)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"HEAD", "/health", 404},
		{"POST", "/health", 404},
		{"GET", "/health/", 404},
		{"GET", "/api//v1/health", 404},
		{"GET", "/api/v1/monitors/", 404},
		{"GET", "/api/v1/monitors/.", 401},
		{"GET", "/api/v1/monitors/..", 401},
		{"GET", "/api/v1/monitors/a%2Fb", 404},
		{"GET", "/api/v1/monitors/%62atch", 401},
		{"GET", "/swagger", 404},
		{"GET", "/socket.io", 404},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := a.request(t, tc.method, tc.path, nil)
			assert.Equal(t, tc.status, w.Code, w.Body.String())
			assert.Empty(t, w.Header().Get("Location"))
		})
	}
}

func TestDocumentationHTTP(t *testing.T) {
	a := newApp(t)
	for _, path := range []string{"/openapi.json", "/swagger/doc.json"} {
		w := a.request(t, "GET", path, nil)
		require.Equal(t, 200, w.Code)
		assert.True(t, json.Valid(w.Body.Bytes()))
		assert.Contains(t, w.Body.String(), `"openapi":"3.1.0"`)
	}
	w := a.request(t, "GET", "/swagger/index.html", nil)
	assert.Equal(t, 307, w.Code)
	assert.Equal(t, "/docs", w.Header().Get("Location"))
	w = a.request(t, "GET", "/docs", nil)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "/openapi")
	assert.Equal(t, 404, a.request(t, "GET", "/swagger/swagger-ui.css", nil).Code)
}

func TestSocketIOThroughAPIRouter(t *testing.T) {
	a := newAppWithAdmin(t)
	// Development logging must retain the writer's upgrade interfaces too.
	srv := httptest.NewServer(httpx.Logger(zap.NewNop().Sugar(), nil)(a.router))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	client := &http.Client{Timeout: 3 * time.Second}
	// The token travels in the auth payload of the CONNECT packet, not in the query string.
	auth, err := json.Marshal(map[string]string{"token": a.accessToken})
	require.NoError(t, err)
	connect := "40" + string(auth)
	base := srv.URL + "/socket.io/?EIO=4&transport=polling"
	resp, err := client.Get(base)
	require.NoError(t, err)
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	require.True(t, strings.HasPrefix(string(data), "0"), string(data))
	var open struct {
		SID string `json:"sid"`
	}
	require.NoError(t, json.Unmarshal(data[1:], &open))
	require.NotEmpty(t, open.SID)
	base += "&sid=" + url.QueryEscape(open.SID)
	resp, err = client.Post(base, "text/plain", strings.NewReader(connect))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	resp, err = client.Get(base)
	require.NoError(t, err)
	data, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(data), "40"), string(data))

	upgradeURL := strings.Replace("ws"+strings.TrimPrefix(base, "http"), "transport=polling", "transport=websocket", 1)
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	upgraded, _, err := dialer.Dial(upgradeURL, nil)
	require.NoError(t, err)
	defer upgraded.Close()
	require.NoError(t, upgraded.SetReadDeadline(time.Now().Add(3*time.Second)))
	require.NoError(t, upgraded.WriteMessage(websocket.TextMessage, []byte("2probe")))
	_, data, err = upgraded.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "3probe", string(data))
	require.NoError(t, upgraded.WriteMessage(websocket.TextMessage, []byte("5")))

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/socket.io/?EIO=4&transport=websocket"
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, data, err = conn.ReadMessage()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(data), "0"), string(data))
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(connect)))
	_, data, err = conn.ReadMessage()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(data), "40"), string(data))
}

func TestPush_HEADDoesNotStoreAHeartbeat(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Backup", "head-push-token")
	w := a.request(t, http.MethodHead, "/api/v1/push/head-push-token", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, a.storedHeartbeats(t, monitorID))
}
