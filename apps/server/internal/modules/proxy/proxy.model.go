package proxy

import (
	"errors"

	"peekaping/internal/modules/shared"
)

type Model = shared.Proxy
type UpdateModel = shared.UpdateProxy

// ErrNotFound is a proxy that isn't in the workspace of the request: it doesn't exist, or it
// belongs to another workspace, and the caller can't tell which (docs/adr/0005 section 8)
var ErrNotFound = errors.New("proxy not found")
