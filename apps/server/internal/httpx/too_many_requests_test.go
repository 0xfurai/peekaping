package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/httpx"

	"github.com/stretchr/testify/assert"
)

func TestRetryAfterSeconds_RoundsUpToWholeSecondsAndIsAtLeastOne(t *testing.T) {
	tests := []struct {
		wait time.Duration
		want int
	}{
		{0, 1},
		{-time.Second, 1},
		{time.Nanosecond, 1},
		{999 * time.Millisecond, 1},
		{time.Second, 1},
		{1001 * time.Millisecond, 2},
		{59*time.Second + time.Millisecond, 60},
		{time.Minute, 60},
		{time.Hour, 3600},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, httpx.RetryAfterSeconds(tc.wait), tc.wait.String())
	}
}

func TestWriteTooManyRequests_Answers429WithRetryAfterAndTheFailBody(t *testing.T) {
	w := httptest.NewRecorder()

	httpx.WriteTooManyRequests(w, "Too many requests, try again later", 2500*time.Millisecond)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "3", w.Header().Get("Retry-After"))
	assert.JSONEq(t, `{"message":"Too many requests, try again later","data":null}`, w.Body.String())
}
