package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"peekaping/internal/httpx"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A path ServeMux would clean or redirect is matched as written: a dot segment is a value, not a
// move up
func TestDotSegmentsAreValuesNotRedirects(t *testing.T) {
	router := httpx.NewRouter()
	router.Handle("GET", "/things/{name}", func(w http.ResponseWriter, r *http.Request) {
		httpx.Text(w, 200, "thing "+r.PathValue("name"))
	})
	router.Handle("GET", "/files/{tail...}", func(w http.ResponseWriter, r *http.Request) {
		httpx.Text(w, 200, "file "+r.PathValue("tail"))
	})
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{"/things/.", 200, "thing ."},
		{"/things/..", 200, "thing .."},
		{"/things/a%20b", 200, "thing a b"},
		{"/things/a/../b", 404, "404 page not found"},
		{"/things/./a", 404, "404 page not found"},
		{"/things//a", 404, "404 page not found"},
		{"/files/..", 200, "file .."},
		{"/files/.", 200, "file ."},
		{"/files/a%2Fb", 200, "file a/b"},
		{"/files/a%20b", 200, "file a b"},
		{"/other//path", 404, "404 page not found"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		assert.Equal(t, tc.status, w.Code, tc.path)
		assert.Equal(t, tc.body, w.Body.String(), tc.path)
		assert.Empty(t, w.Header().Get("Location"), tc.path)
	}
}
