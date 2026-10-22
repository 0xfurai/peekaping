package protocol_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peekaping/internal/modules/probe/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gzipped(t *testing.T, plain string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write([]byte(plain))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func gunzipped(t *testing.T, compressed []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	require.NoError(t, err)
	plain, err := io.ReadAll(zr)
	require.NoError(t, err)
	return string(plain)
}

// readBody is a handler that answers with the body it was sent, or 413 when the body is over the limit
func readBody(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if called != nil {
			*called = true
		}
		body, err := io.ReadAll(r.Body)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(body)
	})
}

func post(handler http.Handler, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/probe/v1/hello", bytes.NewReader(body))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestGzip_DecompressesARequestBody(t *testing.T) {
	handler := protocol.Gzip(readBody(nil))

	w := post(handler, gzipped(t, `{"hello":"world"}`), map[string]string{"Content-Encoding": "gzip"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `{"hello":"world"}`, w.Body.String())
}

func TestGzip_HandsOnAPlainBodyAsItIs(t *testing.T) {
	handler := protocol.Gzip(readBody(nil))

	for _, headers := range []map[string]string{nil, {"Content-Encoding": "identity"}} {
		w := post(handler, []byte(`{"plain":true}`), headers)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, `{"plain":true}`, w.Body.String())
	}
}

func TestGzip_TakesTheContentEncodingOffTheRequestItHandsOn(t *testing.T) {
	var seen http.Header
	var length int64
	handler := protocol.Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, length = r.Header.Clone(), r.ContentLength
	}))

	post(handler, gzipped(t, `{}`), map[string]string{"Content-Encoding": "gzip"})

	assert.Empty(t, seen.Get("Content-Encoding"), "the handler gets a plain body, so it is not told otherwise")
	assert.Equal(t, int64(-1), length, "and the length of the compressed body no longer fits it")
}

func TestGzip_AnEmptyBodyWithContentEncodingIsAnEmptyBody(t *testing.T) {
	handler := protocol.Gzip(readBody(nil))

	w := post(handler, nil, map[string]string{"Content-Encoding": "gzip"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Body.String())
}

func TestGzip_AnswersABadGzipHeader400WithTheAPIsMessage(t *testing.T) {
	called := false
	handler := protocol.Gzip(readBody(&called))

	w := post(handler, []byte("this is not gzip"), map[string]string{"Content-Encoding": "gzip"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"message":"the request body is not valid gzip","data":null}`, w.Body.String())
	assert.False(t, called, "the handler never sees the request")
}

func TestGzip_AnswersAnEncodingItDoesNotKnow400(t *testing.T) {
	called := false
	handler := protocol.Gzip(readBody(&called))

	w := post(handler, []byte("{}"), map[string]string{"Content-Encoding": "br"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"message":"unsupported Content-Encoding \"br\": send the body as it is or gzipped","data":null}`, w.Body.String())
	assert.False(t, called)
}

func TestGzip_TheBodyLimitCountsDecompressedBytes(t *testing.T) {
	handler := protocol.Gzip(readBody(nil))
	justUnder := strings.Repeat("a", protocol.MaxBody)
	over := strings.Repeat("a", protocol.MaxBody+1)

	// A megabyte of one letter is a few kilobytes gzipped, so the compressed size says nothing
	compressed := gzipped(t, over)
	require.Less(t, len(compressed), 10_000)

	assert.Equal(t, http.StatusOK, post(handler, gzipped(t, justUnder), map[string]string{"Content-Encoding": "gzip"}).Code)
	assert.Equal(t, http.StatusRequestEntityTooLarge, post(handler, compressed, map[string]string{"Content-Encoding": "gzip"}).Code)
}

func TestGzip_TheBodyLimitAppliesToAPlainBodyToo(t *testing.T) {
	handler := protocol.Gzip(readBody(nil))

	assert.Equal(t, http.StatusOK, post(handler, []byte(strings.Repeat("a", protocol.MaxBody)), nil).Code)
	assert.Equal(t, http.StatusRequestEntityTooLarge, post(handler, []byte(strings.Repeat("a", protocol.MaxBody+1)), nil).Code)
}

func TestGzip_RefusesAPlainBodyThatDeclaresItselfTooLargeWithoutReadingIt(t *testing.T) {
	called := false
	handler := protocol.Gzip(readBody(&called))
	req := httptest.NewRequest(http.MethodPost, "/probe/v1/hello", io.LimitReader(zeroReader{}, protocol.MaxBody+1))
	req.ContentLength = protocol.MaxBody + 1
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.JSONEq(t, `{"message":"request body too large","data":null}`, w.Body.String())
	assert.False(t, called)
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestGzip_GzipsTheReplyWhenTheClientAcceptsIt(t *testing.T) {
	reply := strings.Repeat(`{"monitors":[]}`, 100)
	handler := protocol.Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Length", "1500")
		_, _ = w.Write([]byte(reply))
	}))

	for _, accept := range []string{"gzip", "GZIP", "deflate, gzip", "br;q=1.0, gzip;q=0.5", " gzip , identity", "gzip;q=1"} {
		t.Run(accept, func(t *testing.T) {
			w := post(handler, nil, map[string]string{"Accept-Encoding": accept})

			assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
			assert.Contains(t, w.Header().Values("Vary"), "Accept-Encoding")
			assert.Empty(t, w.Header().Get("Content-Length"), "the length of the plain reply no longer fits")
			assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
			assert.Equal(t, reply, gunzipped(t, w.Body.Bytes()))
		})
	}
}

func TestGzip_LeavesTheReplyAloneWhenTheClientDoesNotAcceptGzip(t *testing.T) {
	handler := protocol.Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plain":true}`))
	}))

	for _, accept := range []string{"", "identity", "deflate, br", "gzip;q=0", "gzip; q=0.0", "gzipped"} {
		t.Run(accept, func(t *testing.T) {
			headers := map[string]string{}
			if accept != "" {
				headers["Accept-Encoding"] = accept
			}

			w := post(handler, nil, headers)

			assert.Empty(t, w.Header().Get("Content-Encoding"))
			assert.Equal(t, `{"plain":true}`, w.Body.String())
			assert.Contains(t, w.Header().Values("Vary"), "Accept-Encoding", "a cache has to know the reply depends on the header")
		})
	}
}

func TestGzip_DoesNotGzipAReplyWithNoBody(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"204":             func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) },
		"304":             func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotModified) },
		"200 and nothing": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
		"no write at all": func(w http.ResponseWriter, r *http.Request) {},
		"an empty write":  func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(nil) },
		"500 and nothing": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"a header and a 304": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("ETag", `"v1"`)
			w.WriteHeader(http.StatusNotModified)
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := post(protocol.Gzip(handler), nil, map[string]string{"Accept-Encoding": "gzip"})

			assert.Empty(t, w.Header().Get("Content-Encoding"), "an empty body has no gzip stream to carry")
			assert.Empty(t, w.Body.Bytes(), "and no gzip trailer is written")
			assert.NotZero(t, w.Code)
		})
	}
}

func TestGzip_KeepsTheStatusOfAGzippedReply(t *testing.T) {
	handler := protocol.Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUpgradeRequired)
		_, _ = w.Write([]byte(`{"message":"upgrade the probe"}`))
	}))

	w := post(handler, nil, map[string]string{"Accept-Encoding": "gzip"})

	assert.Equal(t, http.StatusUpgradeRequired, w.Code)
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.JSONEq(t, `{"message":"upgrade the probe"}`, gunzipped(t, w.Body.Bytes()))
}

func TestGzip_ManyWritesMakeOneGzipStream(t *testing.T) {
	handler := protocol.Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		for range 50 {
			_, _ = io.WriteString(w, "0123456789")
		}
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "tail")
	}))

	w := post(handler, nil, map[string]string{"Accept-Encoding": "gzip"})

	assert.Equal(t, strings.Repeat("0123456789", 50)+"tail", gunzipped(t, w.Body.Bytes()))
}

func TestGzip_SetsTheContentTypeBeforeCompressingWhenTheHandlerLeftItOut(t *testing.T) {
	handler := protocol.Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html><body>hello</body></html>")
	}))

	w := post(handler, nil, map[string]string{"Accept-Encoding": "gzip"})

	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"), "sniffed from the plain bytes, not from the gzip magic")
}

func TestGzip_DoesNotGzipAReplyThatIsAlreadyEncoded(t *testing.T) {
	handler := protocol.Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "br")
		_, _ = io.WriteString(w, "already-compressed")
	}))

	w := post(handler, nil, map[string]string{"Accept-Encoding": "gzip"})

	assert.Equal(t, "br", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "already-compressed", w.Body.String())
}
