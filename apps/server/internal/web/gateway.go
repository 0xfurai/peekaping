// Package web serves the web app's build beside the API on one port, as Caddy did in front of
// the API in the bundle image (ADR 0002, section 7).
package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"
)

// The Cache-Control headers the gateway sets
const (
	// assetCache is for the build's assets, whose names change with their content
	assetCache = "public, max-age=31536000, immutable"
	// envJSCache keeps /env.js out of every cache, so that a new API_URL applies on the next load
	envJSCache = "no-store, no-cache, must-revalidate, proxy-revalidate, max-age=0"
	// indexCache has the browser check for a new build on every load
	indexCache = "no-cache"
)

// assetExtensions are the build's assets: cached for a year, and a 404 rather than index.html
// when one is missing
var assetExtensions = []string{".js", ".css", ".mjs", ".woff", ".woff2", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico"}

// Config is what the gateway serves
type Config struct {
	// Dir is the web app's build: index.html and the assets it loads
	Dir string
	// APIURL is where the web app reaches the API, written to /env.js. Empty means the origin
	// the web app is served from.
	APIURL string
	// Metrics sends /metrics to the API, which serves it when METRICS_ENABLED is true
	Metrics bool
}

// gateway serves the API's paths with the API, and every other path from the web build
type gateway struct {
	dir     string
	files   fs.FS
	api     http.Handler
	metrics bool
	envJS   []byte
	logger  *zap.SugaredLogger
}

// CheckDir returns an error unless dir holds a web build, with an index.html file
func CheckDir(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("failed to open the web build: %w", err)
	}
	defer root.Close()
	return checkIndex(root.FS(), dir)
}

// checkIndex returns an error unless the build has an index.html file
func checkIndex(files fs.FS, dir string) error {
	info, err := fs.Stat(files, "index.html")
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s has no index.html file, so it isn't a web build", dir)
	}
	return nil
}

// NewGateway returns the handler of the one port that serves both the API and the web build in
// cfg.Dir. /api/*, /probe/* (the probe protocol), /socket.io/* and, with cfg.Metrics, /metrics go
// to the API as they are, so /docs, /swagger/* and /health don't. /env.js is generated from
// cfg.APIURL. Every other GET or HEAD gets a file of the build: an asset (see assetExtensions)
// with a year's cache, or a 404
// when it's missing, and index.html for any other path that isn't a file, so that the web app
// routes it. Nothing outside cfg.Dir is served, even through a link, and directories aren't
// listed.
//
// The gateway reads the build's files from cfg.Dir, which stays open for as long as the
// process runs.
func NewGateway(cfg Config, api http.Handler, logger *zap.SugaredLogger) (http.Handler, error) {
	root, err := os.OpenRoot(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open the web build: %w", err)
	}
	files := root.FS()
	if err := checkIndex(files, cfg.Dir); err != nil {
		_ = root.Close()
		return nil, err
	}

	// json.Marshal escapes quotes, backslashes, <, >, & and the line separators, so API_URL
	// stays one string literal. It never fails on a string.
	apiURL, _ := json.Marshal(cfg.APIURL)

	return &gateway{
		dir:     cfg.Dir,
		files:   files,
		api:     api,
		metrics: cfg.Metrics,
		envJS:   []byte("window.__CONFIG__ = { API_URL: " + string(apiURL) + " };\n"),
		logger:  logger,
	}, nil
}

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath := cleanPath(r.URL.Path)
	if strings.HasPrefix(urlPath, "/api/") || strings.HasPrefix(urlPath, "/probe/") || strings.HasPrefix(urlPath, "/socket.io/") || (g.metrics && urlPath == "/metrics") {
		// The API gets the ResponseWriter itself, so that the websocket can take over the
		// connection and the long polls can flush
		g.api.ServeHTTP(w, r)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if urlPath == "/env.js" {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", envJSCache)
		http.ServeContent(w, r, "env.js", time.Time{}, bytes.NewReader(g.envJS))
		return
	}
	g.serveBuild(w, r, urlPath)
}

// cleanPath cleans the request's path, keeping a trailing slash. The gateway picks where a
// request goes by the cleaned path, as the build's files are found by it, so /api/../health is
// /health.
func cleanPath(p string) string {
	cleaned := path.Clean("/" + p)
	if strings.HasSuffix(p, "/") && cleaned != "/" {
		cleaned += "/"
	}
	return cleaned
}

// serveBuild serves the build's file at urlPath, or the index.html of a directory. A missing
// asset is a 404. Any other path gets the build's index.html.
func (g *gateway) serveBuild(w http.ResponseWriter, r *http.Request, urlPath string) {
	name := strings.TrimPrefix(urlPath, "/")
	if name == "" || strings.HasSuffix(name, "/") {
		name += "index.html"
	}
	file, info, ok := g.open(name)
	if ok && info.IsDir() {
		_ = file.Close()
		name = path.Join(name, "index.html")
		file, info, ok = g.open(name)
	}
	if ok {
		defer file.Close()
		if info.Mode().IsRegular() {
			g.serveFile(w, r, name, file, info)
			return
		}
	}

	if isAsset(urlPath) {
		http.NotFound(w, r)
		return
	}
	index, info, ok := g.open("index.html")
	if !ok || !info.Mode().IsRegular() {
		g.logger.Errorw("Failed to open the web build's index.html", "web_dir", g.dir)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer index.Close()
	g.serveFile(w, r, "index.html", index, info)
}

// open opens the build's file at name. A file that can't be opened, such as a missing one or a
// link out of the build, counts as missing.
func (g *gateway) open(name string) (fs.File, fs.FileInfo, bool) {
	file, err := g.files.Open(name)
	if err != nil {
		return nil, nil, false
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, false
	}
	return file, info, true
}

// serveFile serves the build's file at name, with the cache its kind gets. It answers ranges
// and conditional requests.
func (g *gateway) serveFile(w http.ResponseWriter, r *http.Request, name string, file fs.File, info fs.FileInfo) {
	content, ok := file.(io.ReadSeeker)
	if !ok {
		// The files of an os.Root are *os.File, which seek
		g.logger.Errorw("A file of the web build can't seek", "file", name)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	switch {
	case isAsset(name):
		w.Header().Set("Cache-Control", assetCache)
	case path.Base(name) == "index.html":
		w.Header().Set("Cache-Control", indexCache)
	}
	http.ServeContent(w, r, name, info.ModTime(), content)
}

// isAsset reports whether the path ends with an asset's extension, in any case
func isAsset(p string) bool {
	return slices.Contains(assetExtensions, strings.ToLower(path.Ext(p)))
}
