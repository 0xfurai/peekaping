package apitest

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const oneMiB = 1 << 20

// monitorBodyOfSize is a valid monitor body of exactly size bytes: the API ignores the field
// that pads it.
func monitorBodyOfSize(t *testing.T, size int) string {
	t.Helper()
	body := with(pushMonitorBody("Padded", "padded-token"), "padding", "")
	unpadded, err := json.Marshal(body)
	require.NoError(t, err)
	require.Less(t, len(unpadded), size)
	body["padding"] = strings.Repeat("a", size-len(unpadded))
	padded, err := json.Marshal(body)
	require.NoError(t, err)
	require.Len(t, padded, size)
	return string(padded)
}

// chunked sends the body without a Content-Length, the way a client that streams it does.
func chunked(req *http.Request) { req.ContentLength = -1 }

func TestBodyLimitAPI_RefusesABodyOver1MiBBeforeAuthOrTheHandler(t *testing.T) {
	a := newAppWithAdmin(t)

	for _, size := range []int{oneMiB + 1, 2 * oneMiB} {
		// No credentials: the 413 comes before the 401
		w := a.request(t, http.MethodPost, "/api/v1/monitors", monitorBodyOfSize(t, size))
		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, "%d bytes: %s", size, w.Body.String())
		assert.JSONEq(t, `{"message":"request body too large","data":null}`, w.Body.String())

		// With credentials, the handler never creates the monitor
		w = a.request(t, http.MethodPost, "/api/v1/monitors", monitorBodyOfSize(t, size), a.asAdmin)
		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, "%d bytes, signed in: %s", size, w.Body.String())
	}
	assert.Empty(t, decode[[]monitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin), http.StatusOK))
}

func TestBodyLimitAPI_HandlesABodyOfUpTo1MiB(t *testing.T) {
	a := newAppWithAdmin(t)

	for _, size := range []int{oneMiB - 1, oneMiB} {
		w := a.request(t, http.MethodPost, "/api/v1/monitors", monitorBodyOfSize(t, size))
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%d bytes without credentials: %s", size, w.Body.String())
	}

	w := a.request(t, http.MethodPost, "/api/v1/monitors", monitorBodyOfSize(t, oneMiB), a.asAdmin)
	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func TestBodyLimitAPI_StopsReadingAChunkedBodyAt1MiB(t *testing.T) {
	a := newAppWithAdmin(t)

	// No Content-Length to refuse it by: the handler's read of the body fails, as any body it
	// can't decode does
	w := a.request(t, http.MethodPost, "/api/v1/monitors", monitorBodyOfSize(t, 2*oneMiB), a.asAdmin, chunked)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Empty(t, decode[[]monitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin), http.StatusOK))

	w = a.request(t, http.MethodPost, "/api/v1/monitors", monitorBodyOfSize(t, oneMiB), a.asAdmin, chunked)
	assert.Equal(t, http.StatusCreated, w.Code, "a chunked body of 1 MiB: %s", w.Body.String())
}
