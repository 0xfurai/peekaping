package httpx

import (
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// RetryAfterSeconds is a wait as the whole seconds of a Retry-After header: rounded up, so that a
// client who waits that long is not refused again, and at least one
func RetryAfterSeconds(wait time.Duration) int {
	return max(1, int((wait+time.Second-1)/time.Second))
}

// TooManyRequests is the 429 of a handler: the usual {message, data} body and a Retry-After header
// of the wait in whole seconds. The operation lists 429 in its Errors, which documents the header.
func TooManyRequests(message string, retryAfter time.Duration) error {
	return huma.ErrorWithHeaders(
		Fail(http.StatusTooManyRequests, ErrorBody{Message: message}),
		http.Header{"Retry-After": {strconv.Itoa(RetryAfterSeconds(retryAfter))}},
	)
}

// WriteTooManyRequests answers a request with the 429 of TooManyRequests, for a middleware
func WriteTooManyRequests(w http.ResponseWriter, message string, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(RetryAfterSeconds(retryAfter)))
	JSON(w, http.StatusTooManyRequests, ErrorBody{Message: message})
}
