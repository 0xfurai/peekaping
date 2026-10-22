package main

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/internal/probeproto"
	graceful "peekaping/internal/shutdown"
	"peekaping/internal/version"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const webIndex = "<!doctype html><title>Peekaping</title><script src=\"/env.js\"></script>"

// get sends a GET and returns the response, with its body read
func get(t *testing.T, target string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(target)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(body)
}

func TestRun_ServesTheWebAppAndTheAPIOnOnePort(t *testing.T) {
	webDir := t.TempDir()
	for name, content := range map[string]string{"index.html": webIndex, "assets/index-a1.js": "console.log('app')"} {
		path := filepath.Join(webDir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	cfg := loadConfig(t, map[string]string{"WEB_DIR": webDir, "METRICS_ENABLED": "true"})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)

	t.Run("the web app", func(t *testing.T) {
		for _, tc := range []struct {
			path, body, cacheControl string
		}{
			{"/", webIndex, "no-cache"},
			{"/monitors/x", webIndex, "no-cache"},
			{"/env.js", "window.__CONFIG__ = { API_URL: \"\" };\n", "no-store, no-cache, must-revalidate, proxy-revalidate, max-age=0"},
			{"/assets/index-a1.js", "console.log('app')", "public, max-age=31536000, immutable"},
			// The API's own pages aren't on this port
			{"/health", webIndex, "no-cache"},
			{"/docs", webIndex, "no-cache"},
			{"/swagger/index.html", webIndex, "no-cache"},
		} {
			resp, body := get(t, in.url+tc.path)
			assert.Equal(t, http.StatusOK, resp.StatusCode, tc.path)
			assert.Equal(t, tc.body, body, tc.path)
			assert.Equal(t, tc.cacheControl, resp.Header.Get("Cache-Control"), tc.path)
		}

		resp, body := get(t, in.url+"/assets/index-old.js")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "a missing asset")
		assert.NotEqual(t, webIndex, body)
	})

	t.Run("the API", func(t *testing.T) {
		status, body := in.request(t, http.MethodGet, "/api/v1/version", nil)
		assert.Equal(t, http.StatusOK, status)
		assert.JSONEq(t, `{"version":"`+version.Version+`"}`, string(body))

		resp, metrics := get(t, in.url+"/metrics")
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, metrics, "peekaping_build_info", "METRICS_ENABLED is true")
	})

	t.Run("the websocket, over long polling and over a websocket", func(t *testing.T) {
		in.registerAdmin(t)
		in.dialSocket(t).close()

		query := url.Values{"EIO": {"4"}, "transport": {"websocket"}}
		conn, resp, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(in.url, "http")+"/socket.io/?"+query.Encode(), nil)
		require.NoError(t, err)
		defer conn.Close()
		assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(wait)))

		_, open, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(string(open), "0{") && strings.Contains(string(open), `"sid":`), "Engine.IO OPEN: %s", open)
		require.NoError(t, conn.WriteMessage(gorilla.TextMessage, []byte(in.socketConnect(t))))
		_, connect, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(string(connect), "40"), "socket.io CONNECT: %s", connect)
	})

	t.Run("the probe protocol, over a real connection", func(t *testing.T) {
		if in.accessToken == "" {
			in.registerAdmin(t)
		}
		status, body := in.request(t, http.MethodPost, "/api/v1/probes", map[string]any{"name": "Frankfurt", "location": "eu-central"})
		made := decode[struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		}](t, status, body, http.StatusCreated)
		client := probeproto.NewClient(in.url, made.Token, "single-mode-instance", nil)
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		defer cancel()

		welcome, err := client.Hello(ctx, &probeproto.HelloRequest{
			InstanceID: "single-mode-instance", ProtocolVersion: probeproto.ProtocolVersion, ProbeVersion: "0.1.0",
			SupportedTypes: []string{"http"}, Hostname: "edge-1", OS: "linux", Arch: "arm64",
		})

		require.NoError(t, err, "/probe/v1/ is the API's and not the web app's")
		assert.Equal(t, probeproto.ProbeInfo{ID: made.ID, Name: "Frankfurt", Location: "eu-central"}, welcome.Probe)
		assert.Equal(t, version.Version, welcome.ServerVersion)
		require.NoError(t, client.Bye(ctx))
		stranger := probeproto.NewClient(in.url, made.Token, "never-said-hello", nil)
		var refused *probeproto.Error
		require.ErrorAs(t, stranger.Bye(ctx), &refused, "an instance that never said hello is unknown")
		assert.Equal(t, http.StatusConflict, refused.Status)
	})

	require.NoError(t, in.stop(t))
}

// The gateway answers some requests itself and passes the others to the API router, which has a
// request ID and a request log of its own. Each request gets one ID and one log line either way.
func TestRun_GivesEachRequestBehindTheWebAppOneRequestIDAndOneLogLine(t *testing.T) {
	webDir := t.TempDir()
	for name, content := range map[string]string{"index.html": webIndex, "assets/index-a1.js": "console.log('app')"} {
		path := filepath.Join(webDir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	cfg := loadConfig(t, map[string]string{"WEB_DIR": webDir, "METRICS_ENABLED": "true"})
	core, logs := observer.New(zap.DebugLevel)
	in := startPeekaping(t, cfg, zap.New(core).Sugar(), logs)

	for _, tc := range []struct {
		name, method, path, route string
		status                    int
		level                     zapcore.Level
	}{
		{"a page of the web app", http.MethodGet, "/monitors/x?tab=1", "", http.StatusOK, zap.InfoLevel},
		{"an asset", http.MethodGet, "/assets/index-a1.js", "", http.StatusOK, zap.InfoLevel},
		{"a missing asset", http.MethodGet, "/assets/index-old.js", "", http.StatusNotFound, zap.InfoLevel},
		{"env.js", http.MethodGet, "/env.js", "", http.StatusOK, zap.InfoLevel},
		{"a method the gateway refuses", http.MethodPost, "/monitors", "", http.StatusMethodNotAllowed, zap.InfoLevel},
		{"the gateway's /health, which is the web app's", http.MethodGet, "/health", "", http.StatusOK, zap.DebugLevel},
		{"an API route", http.MethodGet, "/api/v1/version", "/api/v1/version", http.StatusOK, zap.InfoLevel},
		{"the API's health endpoint", http.MethodGet, "/api/v1/health", "/api/v1/health", http.StatusOK, zap.DebugLevel},
		{"metrics", http.MethodGet, "/metrics", "/metrics", http.StatusOK, zap.DebugLevel},
		{"an API path with no route", http.MethodGet, "/api/v1/nothing", "", http.StatusNotFound, zap.InfoLevel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, incoming := range []string{"client-request-7", ""} {
				logs.TakeAll()
				req, err := http.NewRequest(tc.method, in.url+tc.path, nil)
				require.NoError(t, err)
				if incoming != "" {
					req.Header.Set("X-Request-ID", incoming)
				}
				resp, err := http.DefaultClient.Do(req)
				require.NoError(t, err)
				_, err = io.Copy(io.Discard, resp.Body)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.Equal(t, tc.status, resp.StatusCode)

				require.Len(t, resp.Header.Values("X-Request-ID"), 1)
				id := resp.Header.Get("X-Request-ID")
				if incoming != "" {
					assert.Equal(t, incoming, id)
				} else {
					assert.Regexp(t, `^[A-Za-z0-9._-]{1,64}$`, id)
				}
				lines := logs.FilterMessage("HTTP request").All()
				require.Len(t, lines, 1)
				assert.Equal(t, tc.level, lines[0].Level)
				entry := lines[0].ContextMap()
				assert.Equal(t, id, entry["request_id"])
				assert.Equal(t, tc.method, entry["method"])
				assert.Equal(t, tc.route, entry["route"])
				assert.EqualValues(t, tc.status, entry["status"])
				assert.Contains(t, entry, "client_ip")
				assert.Contains(t, entry, "size")
				assert.Contains(t, entry, "duration")
				assert.Len(t, entry, 7)
			}
		})
	}

	require.NoError(t, in.stop(t))
}

func TestRun_ServesTheAPIAloneWithoutWebDir(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)

	resp, body := get(t, in.url+"/")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "404 page not found", body, "the API's 404")

	resp, _ = get(t, in.url+"/env.js")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, body = get(t, in.url+"/health")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, `"status":"success"`, "the API's health")

	require.NoError(t, in.stop(t))
}

func TestRun_LeavesTheDatabaseOpenWhenAnAPIHandlerBehindTheWebAppDoesntReturnInTime(t *testing.T) {
	// The gateway passes the request to the API, whose handler outlives the HTTP server's share
	// and the background jobs' share. It counts as running like one the API router gets directly.
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.HTTP = 300 * time.Millisecond
	shutdownTimeouts.Jobs = 300 * time.Millisecond
	webDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(webDir, "index.html"), []byte(webIndex), 0o644))
	cfg := loadConfig(t, map[string]string{"WEB_DIR": webDir})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	unlock := in.createWaitingForTheDatabase(t, cfg)

	err := in.stop(t)
	require.Error(t, err, "peekaping exits 1")
	assert.Contains(t, err.Error(), "the HTTP handlers didn't stop in time")
	left := logs.FilterMessage("Left the database open, since some of what uses it is still running").All()
	require.Len(t, left, 1)
	assert.Equal(t, []any{"HTTP handlers"}, left[0].ContextMap()["running"])
	assert.Zero(t, logs.FilterMessage("Performing graceful SQLite shutdown...").Len(), "the database isn't closed under the handler")

	unlock()
	require.Eventually(t, func() bool { return logs.FilterMessage("Failed to create monitor").Len() == 1 },
		wait, 10*time.Millisecond, "the handler returns")
}
