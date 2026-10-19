package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"peekaping/internal/httpx"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCatchAllPathsAreNotCleaned(t *testing.T) {
	router := httpx.NewRouter()
	router.Handle("GET", "/files/{tail...}", func(w http.ResponseWriter, r *http.Request) {
		httpx.Text(w, 200, r.PathValue("tail"))
	})
	for _, tc := range []struct{ path, tail string }{
		{"/files/a//b", "a//b"},
		{"/files//a", "/a"},
		{"/files/.", "."},
		{"/files/a/../b", "a/../b"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		assert.Equal(t, 200, w.Code, tc.path)
		assert.Equal(t, tc.tail, w.Body.String(), tc.path)
	}
}

func TestBareCatchAllPrefix(t *testing.T) {
	router := httpx.NewRouter()
	handler := func(w http.ResponseWriter, r *http.Request) { httpx.Text(w, 200, "matched") }
	router.Handle("GET", "/files/{tail...}", handler)
	router.Handle("POST", "/files/{tail...}", handler)
	for _, method := range []string{"GET", "POST", "DELETE", "HEAD"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, "/files", nil))
		assert.Equal(t, 404, w.Code)
		assert.Empty(t, w.Header().Get("Location"))
	}
	// An explicitly registered bare route wins over the compatibility fallback.
	router.Handle("GET", "/files", handler)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/files", nil))
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "matched", w.Body.String())
}

func TestRootRouteIsExact(t *testing.T) {
	router := httpx.NewRouter()
	router.Handle("GET", "/", func(w http.ResponseWriter, r *http.Request) { httpx.Text(w, 200, "root") })
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", 200}, {"GET", "/unknown", 404}, {"HEAD", "/", 404}, {"POST", "/", 404},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		assert.Equal(t, tc.status, w.Code)
	}
}
