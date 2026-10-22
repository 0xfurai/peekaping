package telemetry

import "peekaping/internal/httpx"

type GetInput = httpx.Input[httpx.NoBody, struct{}]

type SetInput = httpx.Input[UpdateDto, struct{}]

// UpdateDto is the body of the telemetry update
type UpdateDto struct {
	// Enabled is a pointer so that a body without it is rejected instead of opting out. The schema
	// stays non-nullable: a null is rejected too, so a client should see a plain boolean.
	Enabled *bool `json:"enabled" validate:"required" nullable:"false"`
}

// StateResponse is what the telemetry API returns
type StateResponse struct {
	Enabled   bool    `json:"enabled" doc:"Whether an admin opted in to the usage ping"`
	ForcedOff bool    `json:"forced_off" doc:"Whether the TELEMETRY or DO_NOT_TRACK environment variable forces the ping off, whatever enabled says"`
	Payload   Payload `json:"payload" doc:"The exact JSON the next usage ping would send; its instance_id is empty until an admin opts in"`
}

func newStateResponse(state State) StateResponse {
	return StateResponse{Enabled: state.Enabled, ForcedOff: state.ForcedOff, Payload: state.Payload}
}
