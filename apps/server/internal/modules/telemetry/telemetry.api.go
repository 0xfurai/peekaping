package telemetry

import "peekaping/internal/httpx"

type GetInput = httpx.Input[httpx.NoBody, struct{}]

type SetInput = httpx.Input[UpdateDto, struct{}]

type MarkNoticeInput = httpx.Input[httpx.NoBody, struct{}]

// UpdateDto is the body of the telemetry update
type UpdateDto struct {
	// Enabled is a pointer so that a body without it is rejected instead of opting out. The schema
	// stays non-nullable: a null is rejected too, so a client should see a plain boolean.
	Enabled *bool `json:"enabled" validate:"required" nullable:"false"`
}

// StateResponse is what the telemetry API returns
type StateResponse struct {
	Enabled   bool `json:"enabled" doc:"Whether the usage ping is on: the admin's stored choice, which is on until they choose"`
	ForcedOff bool `json:"forced_off" doc:"Whether the TELEMETRY environment variable forces the ping off, whatever enabled says"`
	// NoticePending is a separate field from the choice: dismissing the notice is not a choice
	NoticePending bool    `json:"notice_pending" doc:"Whether the one-time notice that the usage ping and error reports are on by default is still to be shown: it is not yet dismissed, and the environment does not force both off"`
	Payload       Payload `json:"payload" doc:"The exact JSON the next usage ping would send; its instance_id is empty after an admin opts out"`
}

func newStateResponse(state State) StateResponse {
	return StateResponse{Enabled: state.Enabled, ForcedOff: state.ForcedOff, NoticePending: state.NoticePending, Payload: state.Payload}
}
