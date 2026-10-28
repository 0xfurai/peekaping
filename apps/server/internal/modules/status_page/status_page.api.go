package status_page

import (
	"peekaping/internal/httpx"
)

type CreateParams struct {
}
type CreateInput = httpx.Input[CreateStatusPageDTO, CreateParams]

type FindByIDParams struct {
	Id string `path:"id" doc:"Status Page ID" required:"true"`
}
type FindByIDInput = httpx.Input[httpx.NoBody, FindByIDParams]

type FindBySlugParams struct {
	Slug string `path:"slug" doc:"Status Page Slug" required:"true"`
}
type FindBySlugInput = httpx.Input[httpx.NoBody, FindBySlugParams]

type FindByDomainParams struct {
	Domain string `path:"domain" doc:"Domain Name" required:"true"`
}
type FindByDomainInput = httpx.Input[httpx.NoBody, FindByDomainParams]

type FindAllParams struct {
	Q     string `query:"q" doc:"Search query"`
	Page  int    `query:"page" doc:"Page number" default:"0" minimum:"0"`
	Limit int    `query:"limit" doc:"Items per page" default:"10" minimum:"1" maximum:"1000"`
}
type FindAllInput = httpx.Input[httpx.NoBody, FindAllParams]

type UpdateParams struct {
	Id string `path:"id" doc:"Status Page ID" required:"true"`
}
type UpdateInput = httpx.Input[UpdateStatusPageDTO, UpdateParams]

type DeleteParams struct {
	Id string `path:"id" doc:"Status Page ID" required:"true"`
}
type DeleteInput = httpx.Input[httpx.NoBody, DeleteParams]

type GetMonitorsBySlugParams struct {
	Slug string `path:"slug" doc:"Status Page Slug" required:"true"`
}
type GetMonitorsBySlugInput = httpx.Input[httpx.NoBody, GetMonitorsBySlugParams]

type GetMonitorsBySlugForHomepageParams struct {
	Slug string `path:"slug" doc:"Status Page Slug" required:"true"`
}
type GetMonitorsBySlugForHomepageInput = httpx.Input[httpx.NoBody, GetMonitorsBySlugForHomepageParams]

// DomainErrorResponse preserves the custom-domain conflict response.
type DomainErrorResponse struct {
	Error *DomainAlreadyUsedError `json:"error"`
}

// SlugErrorResponse is the response to a slug another status page holds, in the shape of the
// custom-domain conflict's.
type SlugErrorResponse struct {
	Error *SlugAlreadyUsedError `json:"error"`
}
