package protocol

import (
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/probeproto"

	"github.com/danielgtaylor/huma/v2"
)

// probeTag groups the protocol's operations in the OpenAPI document, apart from the admin's
const probeTag = "probe-protocol"

// instanceHeader documents the header every call carries, which the middleware reads
var instanceHeaderParam = &huma.Param{
	Name:        probeproto.HeaderInstanceID,
	In:          "header",
	Description: "The id of the probe instance making the call: a random id the probe process chooses when it starts. 1 to 255 visible characters, no spaces.",
	Required:    true,
	Schema:      &huma.Schema{Type: "string", MinLength: ptr(1), MaxLength: ptr(maxInstanceIDLength)},
}

func ptr[T any](v T) *T { return &v }

type Route struct {
	auth    *Auth
	limiter *RateLimiter
}

func NewRoute(auth *Auth, limiter *RateLimiter) *Route {
	return &Route{auth: auth, limiter: limiter}
}

// ConnectRoute registers the probe protocol's calls on router, which is the group they live
// under: /probe/v1, a sibling of /api/v1. The admin's JWT and API keys do not open them: every
// call carries the probe's token, which Authenticate checks.
//
// Every route takes the same three middleware, in this order, from the group: the token and
// instance id check, the rate limit (which keys on the instance the check found), and gzip. A
// route other than hello adds KnownInstance, which stamps the instance and refuses one that never
// said hello. A route added for the protocol takes the group's middleware and, unless it is hello,
// KnownInstance.
func (r *Route) ConnectRoute(router *httpx.Router, controller *Controller) {
	calls := router.Group("")
	calls.Use(r.auth.Authenticate(), r.limiter.Middleware(), Gzip)

	httpx.Register(calls, "POST", "hello", huma.Operation{
		Summary:       "Say hello",
		Description:   "The first call of every probe instance. It records the instance and the monitor types its probe can run, and answers with the server's version and time, the probe's name and location, and the limits the probe works within. A protocol version below the server's minimum is answered 426. The body may be gzipped (Content-Encoding: gzip), up to 1 MB once decompressed, and the reply is gzipped when Accept-Encoding allows it.",
		Tags:          []string{probeTag},
		DefaultStatus: http.StatusOK,
		Security:      httpx.ProbeAuth,
		Parameters:    []*huma.Param{instanceHeaderParam},
		Errors: []int{
			http.StatusBadRequest, http.StatusUnauthorized, http.StatusConflict, http.StatusRequestEntityTooLarge,
			http.StatusUpgradeRequired, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusNotImplemented,
		},
	}, controller.Hello)

	httpx.Register(calls, "POST", "bye", huma.Operation{
		Summary:       "Say bye",
		Description:   "Tells the server that this instance is stopping, so that it removes the instance. An instance that never said hello is answered 409.",
		Tags:          []string{probeTag},
		DefaultStatus: http.StatusNoContent,
		Security:      httpx.ProbeAuth,
		Parameters:    []*huma.Param{instanceHeaderParam},
		Errors: []int{
			http.StatusBadRequest, http.StatusUnauthorized, http.StatusConflict,
			http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusNotImplemented,
		},
	}, controller.Bye, r.auth.KnownInstance())
}
