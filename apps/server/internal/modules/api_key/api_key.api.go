package api_key

import (
	"peekaping/internal/httpx"
)

type CreateAPIKeyParams struct {
}
type CreateAPIKeyInput = httpx.Input[CreateAPIKeyDto, CreateAPIKeyParams]

type GetAPIKeysParams struct {
}
type GetAPIKeysInput = httpx.Input[httpx.NoBody, GetAPIKeysParams]

type GetAPIKeyParams struct {
	Id string `path:"id" doc:"API key ID" required:"true"`
}
type GetAPIKeyInput = httpx.Input[httpx.NoBody, GetAPIKeyParams]

type UpdateAPIKeyParams struct {
	Id string `path:"id" doc:"API key ID" required:"true"`
}
type UpdateAPIKeyInput = httpx.Input[UpdateAPIKeyDto, UpdateAPIKeyParams]

type DeleteAPIKeyParams struct {
	Id string `path:"id" doc:"API key ID" required:"true"`
}
type DeleteAPIKeyInput = httpx.Input[httpx.NoBody, DeleteAPIKeyParams]

type GetAPIKeyConfigParams struct {
}
type GetAPIKeyConfigInput = httpx.Input[httpx.NoBody, GetAPIKeyConfigParams]
