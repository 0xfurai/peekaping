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

type GetProbesAvailabilityParams struct {
	Range string `query:"range" enum:"24h,7d,30d" default:"24h" doc:"How far back to look: 24h, 7d or 30d. Defaults to 24h."`
}
type GetProbesAvailabilityInput = httpx.Input[httpx.NoBody, GetProbesAvailabilityParams]

type GetProbeAvailabilityParams struct {
	Id    string `path:"id" doc:"Probe ID, or local for the local probe" required:"true"`
	Range string `query:"range" enum:"24h,7d,30d" default:"24h" doc:"How far back to look: 24h, 7d or 30d. Defaults to 24h. The instances' connections always cover the last 24 hours."`
}
type GetProbeAvailabilityInput = httpx.Input[httpx.NoBody, GetProbeAvailabilityParams]
