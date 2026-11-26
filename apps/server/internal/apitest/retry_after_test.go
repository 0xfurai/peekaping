package apitest

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-159: every 429 the API answers tells the client how long to wait

// assertRetryAfterWithin checks the Retry-After of a 429: whole seconds, at least one and at most
// the window of the limit that answered
func assertRetryAfterWithin(t *testing.T, w *httpRecorder, window time.Duration) {
	t.Helper()
	seconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
	require.NoError(t, err, "Retry-After is whole seconds: %q", w.Header().Get("Retry-After"))
	assert.GreaterOrEqual(t, seconds, 1)
	assert.LessOrEqual(t, time.Duration(seconds)*time.Second, window)
}
