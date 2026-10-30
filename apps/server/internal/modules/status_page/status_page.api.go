package status_page

import (
	"net/http"

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

type TLSAskParams struct {
	Domain string `query:"domain" doc:"The host a certificate is asked for" required:"true"`
}
type TLSAskInput = httpx.Input[httpx.NoBody, TLSAskParams]

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

type FindDomainsParams struct {
	Id string `path:"id" doc:"Status Page ID" required:"true"`
}
type FindDomainsInput = httpx.Input[httpx.NoBody, FindDomainsParams]

type VerifyDomainParams struct {
	Id     string `path:"id" doc:"Status Page ID" required:"true"`
	Domain string `path:"domain" doc:"A custom domain of the status page" required:"true"`
}
type VerifyDomainInput = httpx.Input[httpx.NoBody, VerifyDomainParams]

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

// UnavailableErrorBody is the code of an UnavailableResponse
type UnavailableErrorBody struct {
	Code string `json:"code" example:"STATUS_PAGE_UNAVAILABLE"`
}

// UnavailableResponse is the answer of a public status page, or of a badge of a monitor on one,
// whose workspace is suspended (PP-372): 503 with a code, and no word on why. A visitor is not told
// that an instance admin suspended the workspace, or that it waits for its owner's email
// verification.
type UnavailableResponse struct {
	Message string               `json:"message" example:"This status page is unavailable"`
	Data    httpx.Null           `json:"data"`
	Error   UnavailableErrorBody `json:"error"`
}

// Unavailable is the error that makes a route answer with an UnavailableResponse
func Unavailable() error {
	return httpx.Fail(http.StatusServiceUnavailable, UnavailableResponse{
		Message: "This status page is unavailable",
		Error:   UnavailableErrorBody{Code: "STATUS_PAGE_UNAVAILABLE"},
	})
}
