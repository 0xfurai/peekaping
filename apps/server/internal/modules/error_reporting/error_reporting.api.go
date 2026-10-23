package error_reporting

import "peekaping/internal/httpx"

type GetInput = httpx.Input[httpx.NoBody, struct{}]

type SetInput = httpx.Input[UpdateDto, struct{}]

// UpdateDto is the body of the error reporting update
type UpdateDto struct {
	// Enabled is a pointer so that a body without it is rejected instead of opting out. The schema
	// stays non-nullable: a null is rejected too, so a client should see a plain boolean.
	Enabled *bool `json:"enabled" validate:"required" nullable:"false"`
}

// StateResponse is what the error reporting API returns
type StateResponse struct {
	Enabled   bool `json:"enabled" doc:"Whether error reports are on: the admin's stored choice, which is on until they choose"`
	ForcedOff bool `json:"forced_off" doc:"Whether the ERROR_REPORTING environment variable forces error reports off, whatever enabled says"`
}

func newStateResponse(state State) StateResponse {
	return StateResponse{Enabled: state.Enabled, ForcedOff: state.ForcedOff}
}
