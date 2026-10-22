package notification_channel

import (
	"peekaping/internal/httpx"
)

type FindAllParams struct {
	Q     string `query:"q" doc:"Search query"`
	Page  int    `query:"page" doc:"Page number" default:"0" minimum:"0"`
	Limit int    `query:"limit" doc:"Items per page" default:"10" minimum:"1" maximum:"1000"`
}
type FindAllInput = httpx.Input[httpx.NoBody, FindAllParams]

type CreateParams struct {
}
type CreateInput = httpx.Input[*CreateUpdateDto, CreateParams]

type FindByIDParams struct {
	Id string `path:"id" doc:"Notification ID" required:"true"`
}
type FindByIDInput = httpx.Input[httpx.NoBody, FindByIDParams]

type UpdateFullParams struct {
	Id string `path:"id" doc:"Notification ID" required:"true"`
}
type UpdateFullInput = httpx.Input[CreateUpdateDto, UpdateFullParams]

type UpdatePartialParams struct {
	Id string `path:"id" doc:"Notification ID" required:"true"`
}
type UpdatePartialInput = httpx.Input[PartialUpdateDto, UpdatePartialParams]

type DeleteParams struct {
	Id string `path:"id" doc:"Notification ID" required:"true"`
}
type DeleteInput = httpx.Input[httpx.NoBody, DeleteParams]

type TestParams struct {
}
type TestInput = httpx.Input[*TestDto, TestParams]
