package error_reporting

import "peekaping/internal/httpx"

type GetInput = httpx.Input[httpx.NoBody, struct{}]

type SetInput = httpx.Input[UpdateDto, struct{}]

// UpdateDto is the body of the error reporting update
type UpdateDto struct {
	// Enabled is a pointer so that a body without it is rejected instead of opting out
	Enabled *bool `json:"enabled" validate:"required"`
}

// StateResponse is what the error reporting API returns
type StateResponse struct {
	Enabled   bool `json:"enabled" doc:"Whether an admin opted in to error reports"`
	ForcedOff bool `json:"forced_off" doc:"Whether the ERROR_REPORTING or DO_NOT_TRACK environment variable forces error reports off, whatever enabled says"`
}

func newStateResponse(state State) StateResponse {
	return StateResponse{Enabled: state.Enabled, ForcedOff: state.ForcedOff}
}
