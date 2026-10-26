package protocol

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/utils"
)

// MessageBodyTooLarge is the message of a 413
const MessageBodyTooLarge = "request body too large"

// decodeBody reads the request's JSON body into into. Gzip has limited the body to MaxBytes, so
// a body that is too large fails the read, and the error is a 413. A body that can't be read
// (a broken gzip stream) or isn't JSON is a 400. The returned error is ready to be a handler's.
func decodeBody(r *http.Request, into any) error {
	raw, err := io.ReadAll(r.Body)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return httpx.Fail(http.StatusRequestEntityTooLarge, utils.NewFailResponse(MessageBodyTooLarge))
	case err != nil:
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("failed to read the request body: "+err.Error()))
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return httpx.Fail(http.StatusBadRequest, utils.NewFailResponse("invalid request body: "+err.Error()))
	}
	return nil
}
