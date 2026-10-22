package probe

import (
	"peekaping/internal/httpx"
)

type CreateProbeParams struct {
}
type CreateProbeInput = httpx.Input[CreateProbeDto, CreateProbeParams]

type GetProbesParams struct {
}
type GetProbesInput = httpx.Input[httpx.NoBody, GetProbesParams]

type GetProbeParams struct {
	Id string `path:"id" doc:"Probe ID, or local for the local probe" required:"true"`
}
type GetProbeInput = httpx.Input[httpx.NoBody, GetProbeParams]

type UpdateProbeParams struct {
	Id string `path:"id" doc:"Probe ID, or local for the local probe" required:"true"`
}
type UpdateProbeInput = httpx.Input[UpdateProbeDto, UpdateProbeParams]

type DeleteProbeParams struct {
	Id string `path:"id" doc:"Probe ID" required:"true"`
}
type DeleteProbeInput = httpx.Input[httpx.NoBody, DeleteProbeParams]

type RotateProbeTokenParams struct {
	Id string `path:"id" doc:"Probe ID" required:"true"`
}
type RotateProbeTokenInput = httpx.Input[httpx.NoBody, RotateProbeTokenParams]
