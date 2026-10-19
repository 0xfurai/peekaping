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
