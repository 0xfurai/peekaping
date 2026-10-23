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

type ByeParams struct {
}
type ByeInput = httpx.Input[httpx.NoBody, ByeParams]
