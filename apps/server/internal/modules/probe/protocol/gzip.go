package protocol

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"peekaping/internal/httpx"
	"peekaping/internal/utils"
)

// maxCompressedBody is how many bytes of a gzipped request body are read off the wire. A body
// that inflates to MaxBody can't take more than that, apart from gzip's own few bytes of framing,
// so this stops a stream that never produces output from being read without end.
const maxCompressedBody = MaxBody + 4<<10

// Gzip lets a probe send and receive gzip, which a batch of results gains a lot from. A request
// with Content-Encoding: gzip has its body replaced by the decompressed stream, and a reply is
// gzipped when the request's Accept-Encoding allows it.
//
// MaxBody limits the body the handler reads, counted after decompression, whether it was gzipped
// or not: reading past it fails with *http.MaxBytesError, which the handler answers 413 to. A
// plain body whose Content-Length is already over the limit is refused here, unread. A body that
// isn't gzip, or is in an encoding other than gzip, is refused with 400.
func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The reply depends on the request's Accept-Encoding, whichever way it goes
		w.Header().Add("Vary", "Accept-Encoding")

		r, ok := decodeRequestBody(w, r)
		if !ok {
			return
		}
		if acceptsGzip(r.Header) {
			gz := &gzipResponseWriter{ResponseWriter: w}
			defer gz.finish()
			w = gz
		}
		next.ServeHTTP(w, r)
	})
}

// decodeRequestBody limits the request's body to MaxBody and decompresses it if it is gzipped. It
// answers 400 or 413 itself and returns false when the request can't go on.
func decodeRequestBody(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	encoding := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding")))
	if r.Body == nil || r.Body == http.NoBody {
		return r, true
	}
	switch encoding {
	case "", "identity":
		if r.ContentLength > MaxBody {
			httpx.JSON(w, http.StatusRequestEntityTooLarge, utils.NewFailResponse(MessageBodyTooLarge))
			return nil, false
		}
		decoded := r.WithContext(r.Context())
		decoded.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		return decoded, true
	case "gzip", "x-gzip":
		decoded := r.WithContext(r.Context())
		// The stream is no longer the compressed one the headers describe
		decoded.Header = r.Header.Clone()
		decoded.Header.Del("Content-Encoding")
		decoded.Header.Del("Content-Length")
		decoded.ContentLength = -1
		decoded.GetBody = nil
		if r.ContentLength == 0 {
			decoded.Body = http.NoBody
			return decoded, true
		}
		zr, err := gzip.NewReader(http.MaxBytesReader(w, r.Body, maxCompressedBody))
		var tooLarge *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			// Chunked, and nothing in the chunks: an empty body
			decoded.Body = http.NoBody
			return decoded, true
		case errors.As(err, &tooLarge):
			httpx.JSON(w, http.StatusRequestEntityTooLarge, utils.NewFailResponse(MessageBodyTooLarge))
			return nil, false
		case err != nil:
			httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse("the request body is not valid gzip"))
			return nil, false
		}
		decoded.Body = http.MaxBytesReader(w, zr, MaxBody)
		return decoded, true
	default:
		httpx.JSON(w, http.StatusBadRequest, utils.NewFailResponse(
			"unsupported Content-Encoding "+strconv.Quote(encoding)+": send the body as it is or gzipped"))
		return nil, false
	}
}

// acceptsGzip reports whether the request's Accept-Encoding names gzip, with a quality above zero
func acceptsGzip(header http.Header) bool {
	for _, value := range header.Values("Accept-Encoding") {
		for _, coding := range strings.Split(value, ",") {
			name, params, _ := strings.Cut(strings.TrimSpace(coding), ";")
			if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
				continue
			}
			for _, param := range strings.Split(params, ";") {
				key, quality, _ := strings.Cut(strings.TrimSpace(param), "=")
				if !strings.EqualFold(strings.TrimSpace(key), "q") {
					continue
				}
				if q, err := strconv.ParseFloat(strings.TrimSpace(quality), 64); err == nil && q <= 0 {
					return false
				}
			}
			return true
		}
	}
	return false
}

// gzipWriters keeps the gzip writers, which hold a few hundred kilobytes each, between replies
var gzipWriters = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

// gzipResponseWriter gzips what the handler writes. It holds back the status the handler sets
// until it knows whether there is a body to compress: a reply with no body (204, 304, or nothing
// written) is sent as it is, since an empty body with Content-Encoding: gzip would not be a gzip
// stream.
type gzipResponseWriter struct {
	http.ResponseWriter
	// status is what the handler asked for, 0 until it asks
	status int
	// started is whether the header went out
	started bool
	// gz is the gzip stream of the body, nil when the reply goes out as it is
	gz *gzip.Writer
}

func (g *gzipResponseWriter) WriteHeader(status int) {
	if g.started || g.status != 0 {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		// Informational responses come before the real one
		g.ResponseWriter.WriteHeader(status)
		return
	}
	g.status = status
}

func (g *gzipResponseWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if !g.started {
		g.start(p)
	}
	if g.gz == nil {
		return g.ResponseWriter.Write(p)
	}
	return g.gz.Write(p)
}

// start sends the header, and begins the gzip stream unless the reply must go out as it is
func (g *gzipResponseWriter) start(first []byte) {
	status := g.status
	if status == 0 {
		status = http.StatusOK
	}
	header := g.Header()
	bodyless := status == http.StatusNoContent || status == http.StatusNotModified || status < 200
	if !bodyless && header.Get("Content-Encoding") == "" {
		if header.Get("Content-Type") == "" {
			// net/http would sniff the gzip magic number, and call the reply application/x-gzip
			header.Set("Content-Type", http.DetectContentType(first))
		}
		header.Set("Content-Encoding", "gzip")
		header.Del("Content-Length")
		g.gz = gzipWriters.Get().(*gzip.Writer)
		g.gz.Reset(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(status)
	g.started = true
}

// finish ends the gzip stream, or sends the header of a reply that had no body
func (g *gzipResponseWriter) finish() {
	if !g.started {
		if g.status != 0 {
			g.ResponseWriter.WriteHeader(g.status)
		}
		return
	}
	if g.gz != nil {
		_ = g.gz.Close()
		g.gz.Reset(io.Discard)
		gzipWriters.Put(g.gz)
		g.gz = nil
	}
}

func (g *gzipResponseWriter) Flush() {
	if !g.started {
		g.start(nil)
	}
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if flusher, ok := g.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer
func (g *gzipResponseWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }
