package protocol

import (
	"peekaping/internal/httpx"
	"peekaping/internal/probeproto"
)

// The calls take their body as the protocol's own message. The instance id and the probe's token
// are headers, which the middleware reads, so they are not parameters here.

type HelloParams struct {
}
type HelloInput = httpx.Input[probeproto.HelloRequest, HelloParams]

type AssignmentsParams struct {
	Version string `query:"version" doc:"The version of the assignments the instance holds, as the server last sent it. Leave it out for the first call. When it is the current one, the server holds the request until something changes, for up to 25 seconds, and answers 304 with no body at the timeout; when it is not, the server answers at once."`
}
type AssignmentsInput = httpx.Input[httpx.NoBody, AssignmentsParams]

type ResultsParams struct {
}
type ResultsInput = httpx.Input[probeproto.ResultsRequest, ResultsParams]

type ByeParams struct {
}
type ByeInput = httpx.Input[httpx.NoBody, ByeParams]
