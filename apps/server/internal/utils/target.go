package utils

import (
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/netguard"
)

// TargetNotAllowedResponse is the answer to a monitor or proxy that names a host the install blocks
// (BLOCK_PRIVATE_TARGETS, PP-156). It is a 400 with the usual message and the reason in a code, so
// that a client can tell it from an invalid configuration.
type TargetNotAllowedResponse struct {
	Message string                `json:"message" example:"target address is not allowed: 169.254.169.254"`
	Data    httpx.Null            `json:"data"`
	Error   *netguard.TargetError `json:"error"`
}

// TargetNotAllowed is the 400 for a save that names a host the install blocks
func TargetNotAllowed(err *netguard.TargetError) error {
	return httpx.Fail(http.StatusBadRequest, TargetNotAllowedResponse{Message: err.Error(), Error: err})
}
