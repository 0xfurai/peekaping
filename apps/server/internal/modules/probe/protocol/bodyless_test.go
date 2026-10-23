package protocol_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/modules/probe/protocol"

	"github.com/stretchr/testify/assert"
)

func TestBodyless_DropsTheContentTypeOfAReplyWithNoBody(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotModified} {
		handler := protocol.Bodyless(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Vary", "Accept-Encoding")
			w.WriteHeader(status)
		}))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

		assert.Equal(t, status, w.Code)
		assert.Empty(t, w.Header().Get("Content-Type"), "%d", status)
		assert.Equal(t, "Accept-Encoding", w.Header().Get("Vary"), "and nothing else")
	}
}

func TestBodyless_LeavesOtherRepliesAlone(t *testing.T) {
	handler := protocol.Bodyless(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"no"}`))
	}))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"message":"no"}`, w.Body.String())
}
