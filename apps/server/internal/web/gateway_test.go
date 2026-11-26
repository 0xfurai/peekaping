package web_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"peekaping/internal/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	index     = "<!doctype html><title>Peekaping</title>"
	immutable = "public, max-age=31536000, immutable"
	noStore   = "no-store, no-cache, must-revalidate, proxy-revalidate, max-age=0"
	secret    = "outside the web build"
)

// assetExtensions are the files that are cached for a year, as the Caddyfile cached them
var assetExtensions = []string{".js", ".css", ".mjs", ".woff", ".woff2", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico"}

// webBuild writes a web build like the one vite writes, beside a file that isn't part of it,
// and returns its directory
func webBuild(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "web")
	files := map[string]string{
		"index.html":          index,
		"env.js":              `window.__CONFIG__ = { API_URL: "http://stale.example" };`,
		"robots.txt":          "User-agent: *",
		"assets/index-a1.js":  "console.log('app')",
		"assets/index-b2.css": "body{}",
		"LOGO.PNG":            "png",
		"nested/index.html":   "<title>nested</title>",
	}
	for _, ext := range assetExtensions {
		files["assets/file"+ext] = "asset" + ext
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.txt"), []byte(secret), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.js"), []byte(secret), 0o644))
	// A link out of the build is not followed
	require.NoError(t, os.Symlink(filepath.Join(root, "secret.js"), filepath.Join(dir, "leak.js")))
	require.NoError(t, os.Symlink(filepath.Join(root, "secret.txt"), filepath.Join(dir, "leak.txt")))
	return dir
}

// apiAnswer is what the fake API answers, so a test sees which requests reached it
func apiAnswer(r *http.Request) string { return "api " + r.Method + " " + r.URL.RequestURI() }

func newGateway(t *testing.T, cfg web.Config) http.Handler {
	t.Helper()
	if cfg.Dir == "" {
		cfg.Dir = webBuild(t)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(299)
		_, _ = fmt.Fprint(w, apiAnswer(r))
	})
	gateway, err := web.NewGateway(cfg, api, zap.NewNop().Sugar())
	require.NoError(t, err)
	return gateway
}

func serve(handler http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func TestGateway_ServesIndexForAPathThatIsNotAFile(t *testing.T) {
	gateway := newGateway(t, web.Config{})

	for _, target := range []string{
		"/",
		"/monitors/x",
		"/monitors/x/edit?tab=history",
		"/status/my-page",
		"/assets/",       // a directory without an index.html: no listing
		"/assets",        // the same, without the slash
		"/robots.txt/x",  // under a file
		"/settings.json", // not an asset
		"/assets/index-a1.js/",
		"/env.js/",
	} {
		t.Run(target, func(t *testing.T) {
			w := serve(gateway, http.MethodGet, target)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, index, w.Body.String())
			assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
			assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"), "the browser checks for a new build on every load")
		})
	}
}

func TestGateway_ServesIndexHTMLWithoutLongCache(t *testing.T) {
	gateway := newGateway(t, web.Config{})

	for target, body := range map[string]string{
		"/index.html":        index,
		"/nested/":           "<title>nested</title>",
		"/nested":            "<title>nested</title>",
		"/nested/index.html": "<title>nested</title>",
	} {
		t.Run(target, func(t *testing.T) {
			w := serve(gateway, http.MethodGet, target)

			assert.Equal(t, http.StatusOK, w.Code, "no redirect")
			assert.Equal(t, body, w.Body.String())
			assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
		})
	}

	t.Run("HEAD", func(t *testing.T) {
		w := serve(gateway, http.MethodHead, "/")

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Body.String())
		assert.Equal(t, fmt.Sprint(len(index)), w.Header().Get("Content-Length"))
	})
}

func TestGateway_GeneratesEnvJSFromConfig(t *testing.T) {
	t.Run("API_URL is empty by default, so the web app reaches the API on its own origin", func(t *testing.T) {
		gateway := newGateway(t, web.Config{})

		w := serve(gateway, http.MethodGet, "/env.js")

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "window.__CONFIG__ = { API_URL: \"\" };\n", w.Body.String(), "generated, not the build's env.js")
		assert.Equal(t, noStore, w.Header().Get("Cache-Control"))
		assert.Equal(t, "text/javascript; charset=utf-8", w.Header().Get("Content-Type"))
	})

	t.Run("API_URL is set", func(t *testing.T) {
		gateway := newGateway(t, web.Config{APIURL: "https://api.example.com"})

		w := serve(gateway, http.MethodGet, "/env.js")

		assert.Equal(t, "window.__CONFIG__ = { API_URL: \"https://api.example.com\" };\n", w.Body.String())
		assert.Equal(t, noStore, w.Header().Get("Cache-Control"))
	})

	t.Run("API_URL can't add a script", func(t *testing.T) {
		apiURL := "x\" }; alert(1); //</script>\u2028\\"
		gateway := newGateway(t, web.Config{APIURL: apiURL})

		w := serve(gateway, http.MethodGet, "/env.js")

		body := w.Body.String()
		assert.Equal(t, `window.__CONFIG__ = { API_URL: "x\" }; alert(1); //\u003c/script\u003e\u2028\\" };`+"\n", body)
		literal := strings.TrimSuffix(strings.TrimPrefix(body, "window.__CONFIG__ = { API_URL: "), " };\n")
		var decoded string
		require.NoError(t, json.Unmarshal([]byte(literal), &decoded), "API_URL is one string literal")
		assert.Equal(t, apiURL, decoded)
	})

	t.Run("HEAD", func(t *testing.T) {
		gateway := newGateway(t, web.Config{})

		w := serve(gateway, http.MethodHead, "/env.js")

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Body.String())
		assert.Equal(t, noStore, w.Header().Get("Cache-Control"))
	})
}

func TestGateway_CachesAssetsForAYear(t *testing.T) {
	gateway := newGateway(t, web.Config{})

	targets := map[string]string{"/assets/index-a1.js": "console.log('app')", "/LOGO.PNG": "png"}
	for _, ext := range assetExtensions {
		targets["/assets/file"+ext] = "asset" + ext
	}
	for target, body := range targets {
		t.Run(target, func(t *testing.T) {
			w := serve(gateway, http.MethodGet, target)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, body, w.Body.String())
			assert.Equal(t, immutable, w.Header().Get("Cache-Control"))
		})
	}
}

func TestGateway_AnswersAMissingAssetWith404(t *testing.T) {
	gateway := newGateway(t, web.Config{})

	targets := []string{"/assets/index-old.js", "/favicon.ico", "/FONT.WOFF2"}
	for _, ext := range assetExtensions {
		targets = append(targets, "/assets/missing"+ext)
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			w := serve(gateway, http.MethodGet, target)

			assert.Equal(t, http.StatusNotFound, w.Code, "not index.html")
			assert.NotEqual(t, index, w.Body.String())
			assert.Empty(t, w.Header().Get("Cache-Control"), "a 404 isn't cached for a year")
		})
	}
}

func TestGateway_ServesOtherFilesWithoutCacheHeaders(t *testing.T) {
	gateway := newGateway(t, web.Config{})

	w := serve(gateway, http.MethodGet, "/robots.txt")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User-agent: *", w.Body.String())
	assert.Empty(t, w.Header().Get("Cache-Control"), "as the Caddyfile served them")
}

func TestGateway_SendsOnlyTheAPIPathsToTheAPI(t *testing.T) {
	t.Run("to the API", func(t *testing.T) {
		gateway := newGateway(t, web.Config{})
		for _, req := range []struct{ method, target string }{
			{http.MethodGet, "/api/v1/health"},
			{http.MethodGet, "/api/v1/monitors?page=0&limit=10"},
			{http.MethodPost, "/api/v1/auth/login"},
			{http.MethodDelete, "/api/v1/monitors/x"},
			{http.MethodGet, "/api/v1/push/token?status=up&msg=OK"},
			{http.MethodGet, "/api/"},
			{http.MethodPost, "/probe/v1/hello"},
			{http.MethodGet, "/probe/v1/assignments?version=v1"},
			{http.MethodPost, "/probe/v1/bye"},
			{http.MethodGet, "/socket.io/?EIO=4&transport=polling"},
			{http.MethodPost, "/socket.io/?EIO=4&transport=polling&sid=abc"},
		} {
			t.Run(req.method+" "+req.target, func(t *testing.T) {
				httpReq := httptest.NewRequest(req.method, req.target, nil)
				w := serve(gateway, req.method, req.target)

				assert.Equal(t, 299, w.Code)
				assert.Equal(t, apiAnswer(httpReq), w.Body.String(), "the request reaches the API unchanged")
			})
		}
	})

	t.Run("not to the API", func(t *testing.T) {
		gateway := newGateway(t, web.Config{})
		for _, target := range []string{
			"/docs",
			"/docs/",
			"/swagger/x",
			"/swagger/index.html",
			"/health",
			"/metrics",
			"/api",
			"/apis",
			"/probe",
			"/probes",
			"/probe/../health",
			"/probe/%2e%2e/health",
			"/socket.io",
			"/api/../health",
			"/api/%2e%2e/docs",
			"/socket.io/../metrics",
		} {
			t.Run(target, func(t *testing.T) {
				w := serve(gateway, http.MethodGet, target)

				assert.Equal(t, http.StatusOK, w.Code)
				assert.Equal(t, index, w.Body.String(), "the web app, not the API")
			})
		}
	})

	t.Run("/metrics reaches the API when it's enabled", func(t *testing.T) {
		gateway := newGateway(t, web.Config{Metrics: true})

		w := serve(gateway, http.MethodGet, "/metrics")

		assert.Equal(t, 299, w.Code)
		assert.Equal(t, "api GET /metrics", w.Body.String())

		w = serve(gateway, http.MethodGet, "/metrics/x")
		assert.Equal(t, index, w.Body.String(), "only /metrics itself")
	})
}

func TestGateway_RefusesToWriteTheWebApp(t *testing.T) {
	gateway := newGateway(t, web.Config{})

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, target := range []string{"/", "/monitors", "/env.js", "/assets/index-a1.js"} {
			w := serve(gateway, method, target)

			assert.Equal(t, http.StatusMethodNotAllowed, w.Code, method+" "+target)
			assert.Equal(t, "GET, HEAD", w.Header().Get("Allow"))
		}
	}
}

func TestGateway_ServesNothingOutsideTheWebBuild(t *testing.T) {
	gateway := newGateway(t, web.Config{})

	for _, target := range []string{
		"/../secret.txt",
		"/%2e%2e/secret.txt",
		"/%2E%2E/secret.txt",
		"/..%2fsecret.txt",
		"/%2e%2e%2fsecret.txt",
		"/assets/../../secret.txt",
		"/assets/%2e%2e/%2e%2e/secret.txt",
		"/..%5csecret.txt",
		"/....//secret.txt",
		"//../secret.txt",
		"/leak.txt", // a link out of the build
	} {
		t.Run(target, func(t *testing.T) {
			w := serve(gateway, http.MethodGet, target)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, index, w.Body.String())
		})
	}

	for _, target := range []string{"/../secret.js", "/%2e%2e/secret.js", "/assets/..%2f..%2fsecret.js", "/leak.js"} {
		t.Run(target, func(t *testing.T) {
			w := serve(gateway, http.MethodGet, target)

			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.NotContains(t, w.Body.String(), secret)
		})
	}
}

func TestGateway_LetsTheAPIUpgradeToAWebsocket(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, flushes := w.(http.Flusher)
		hijacker, ok := w.(http.Hijacker)
		if !ok || !flushes {
			http.Error(w, "the ResponseWriter can't flush or be hijacked", http.StatusInternalServerError)
			return
		}
		conn, buf, err := hijacker.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\nhello")
		_ = buf.Flush()
	})
	gateway, err := web.NewGateway(web.Config{Dir: webBuild(t)}, api, zap.NewNop().Sugar())
	require.NoError(t, err)
	server := httptest.NewServer(gateway)
	t.Cleanup(server.Close)

	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	require.NoError(t, err)
	defer conn.Close()
	_, err = fmt.Fprint(conn, "GET /socket.io/?EIO=4&transport=websocket HTTP/1.1\r\nHost: peekaping\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
	require.NoError(t, err)
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	require.NoError(t, err)

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	rest := make([]byte, len("hello"))
	_, err = io.ReadFull(reader, rest)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(rest), "the API talks on the connection it took over")
}

func TestNewGateway_RefusesADirectoryWithoutAWebBuild(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	empty := t.TempDir()
	indexIsADirectory := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(indexIsADirectory, "index.html"), 0o755))
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, []byte(index), 0o644))

	for name, dir := range map[string]string{
		"missing":                 missing,
		"empty":                   empty,
		"index.html is directory": indexIsADirectory,
		"a file":                  file,
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, web.CheckDir(dir))

			_, err := web.NewGateway(web.Config{Dir: dir}, http.NotFoundHandler(), zap.NewNop().Sugar())

			require.Error(t, err)
			assert.Contains(t, err.Error(), dir)
		})
	}

	require.NoError(t, web.CheckDir(webBuild(t)))
}
