package setting

import (
	"peekaping/internal/httpx"
)

type GetByKeyParams struct {
	Key string `path:"key" doc:"Setting Key" required:"true"`
}
type GetByKeyInput = httpx.Input[httpx.NoBody, GetByKeyParams]

type SetByKeyParams struct {
	Key string `path:"key" doc:"Setting Key" required:"true"`
}
type SetByKeyInput = httpx.Input[CreateUpdateDto, SetByKeyParams]

type DeleteByKeyParams struct {
	Key string `path:"key" doc:"Setting Key" required:"true"`
}
type DeleteByKeyInput = httpx.Input[httpx.NoBody, DeleteByKeyParams]
