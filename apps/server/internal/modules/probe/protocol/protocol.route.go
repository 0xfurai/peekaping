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
// Every route takes the same middleware, in this order, from the group: the token and instance id
// check, the rate limit (which keys on the instance the check found), gzip, and Bodyless, which
// takes the Content-Type off a 204 or 304. A route other than hello adds KnownInstance, which
// stamps the instance and refuses one that never said hello. A route added for the protocol takes
// the group's middleware and, unless it is hello, KnownInstance.
func (r *Route) ConnectRoute(router *httpx.Router, controller *Controller) {
	calls := router.Group("")
	calls.Use(r.auth.Authenticate(), r.limiter.Middleware(), Gzip, Bodyless)

	httpx.Register(calls, "POST", "hello", huma.Operation{
		Summary:       "Say hello",
		Description:   "The first call of every probe instance. It records the instance and the monitor types its probe can run, and answers with the server's version and time, the probe's name and location, and the limits the probe works within. A protocol version below the server's minimum is answered 426. A probe holds at most 64 instances. A hello from a new instance past that replaces the probe's oldest instance that has not been seen for a minute, and is answered 429 with Retry-After: 60 when all 64 are running. A hello from an instance the probe already holds is never refused for this. The body may be gzipped (Content-Encoding: gzip), up to 1 MB once decompressed, and the reply is gzipped when Accept-Encoding allows it.",
		Tags:          []string{probeTag},
		DefaultStatus: http.StatusOK,
		Security:      httpx.ProbeAuth,
		Parameters:    []*huma.Param{instanceHeaderParam},
		Errors: []int{
			http.StatusBadRequest, http.StatusUnauthorized, http.StatusConflict, http.StatusRequestEntityTooLarge,
			http.StatusUpgradeRequired, http.StatusTooManyRequests, http.StatusInternalServerError,
		},
	}, controller.Hello)

	httpx.Register(calls, "GET", "assignments", huma.Operation{
		Summary:       "Get the monitors this instance runs",
		Description:   "A long poll. The answer is the monitors this instance of the probe runs, and a version that names the probe's assignment: its active monitors that are not push monitors and are of a type the probe supports, with their proxies, dealt among the probe's instances seen in the last 60 seconds so that each monitor runs on exactly one of them. Send the version you hold. If it is not the current one, or you send none, the server answers 200 at once. If it is, the server holds the request until a monitor of the probe is created, updated or deleted, or an instance of the probe joins or leaves, and answers 200 then; after 25 seconds without a change it answers 304 with no body, and the instance polls again. The reply is gzipped when Accept-Encoding allows it.",
		Tags:          []string{probeTag},
		DefaultStatus: http.StatusOK,
		Security:      httpx.ProbeAuth,
		Parameters:    []*huma.Param{instanceHeaderParam},
		Responses: map[string]*huma.Response{
			"304": {Description: "Not modified: nothing changed in the time the server held the request. There is no body."},
		},
		Errors: []int{
			http.StatusBadRequest, http.StatusUnauthorized, http.StatusConflict,
			http.StatusTooManyRequests, http.StatusInternalServerError,
		},
	}, controller.Assignments, r.auth.KnownInstance())

	httpx.Register(calls, "POST", "results", huma.Operation{
		Summary:       "Send check results",
		Description:   "Sends the results of the checks this instance ran, at most 500 in one request. The answer holds one outcome for each result, in the order sent: accepted, or rejected with a reason. A rejected result is not stored and is not worth sending again, with one exception: the answer to a request that failed tells nothing, so the instance sends the whole batch again, and the results of it that were stored the first time are answered duplicate. A request fails with 500, or with 503 when the server could not take it just now, and also when another request holds the id of one of the results and has not stored it yet (it may still be at it, or it died), or when storing took longer than 90 seconds; that 503 carries a Retry-After of 5 seconds, which the instance waits before it sends the batch again. The reasons: unknown_monitor (there is no such monitor), not_assigned (the monitor is not one of this probe's active monitors that it can run), too_old (the check started more than an hour ago), duplicate (the server stored a result with this id from this probe in the last hour) and invalid (the result is malformed, or its start is more than a minute ahead of the server's clock). An accepted result is stored with the monitor's settings and maintenance state as they are now, in the order of the checks' start times for each monitor. The answer comes after the results are stored, by the API replica that took the request. The body may be gzipped (Content-Encoding: gzip), up to 1 MB once decompressed, and the reply is gzipped when Accept-Encoding allows it.",
		Tags:          []string{probeTag},
		DefaultStatus: http.StatusOK,
		Security:      httpx.ProbeAuth,
		Parameters:    []*huma.Param{instanceHeaderParam},
		Errors: []int{
			http.StatusBadRequest, http.StatusUnauthorized, http.StatusConflict, http.StatusRequestEntityTooLarge,
			http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable,
		},
	}, controller.Results, r.auth.KnownInstance())

	httpx.Register(calls, "POST", "bye", huma.Operation{
		Summary:       "Say bye",
		Description:   "Tells the server that this instance is stopping, so that it takes the instance out of the dealing at once. The instance stays listed, and the probe counts as offline only after PROBE_OFFLINE_AFTER without any instance, so a restart is not an outage. An instance that never said hello is answered 409.",
		Tags:          []string{probeTag},
		DefaultStatus: http.StatusNoContent,
		Security:      httpx.ProbeAuth,
		Parameters:    []*huma.Param{instanceHeaderParam},
		Errors: []int{
			http.StatusBadRequest, http.StatusUnauthorized, http.StatusConflict,
			http.StatusTooManyRequests, http.StatusInternalServerError,
		},
	}, controller.Bye, r.auth.KnownInstance())
}
