package monitor

import (
	"peekaping/internal/httpx"
)

type FindAllParams struct {
	Q      string `query:"q" doc:"Search query"`
	Page   int    `query:"page" doc:"Page number" default:"0" minimum:"0"`
	Limit  int    `query:"limit" doc:"Items per page" default:"10" minimum:"1" maximum:"1000"`
	Active bool   `query:"active" doc:"Active status"`
	Status int    `query:"status" doc:"Status"`
	TagIds string `query:"tag_ids" doc:"Comma-separated list of tag IDs to filter by"`
}
type FindAllInput = httpx.Input[httpx.NoBody, FindAllParams]

type CreateParams struct {
}
type CreateInput = httpx.Input[*CreateUpdateDto, CreateParams]

type FindByIDParams struct {
	Id string `path:"id" doc:"Monitor ID" required:"true"`
}
type FindByIDInput = httpx.Input[httpx.NoBody, FindByIDParams]

type UpdateFullParams struct {
	Id string `path:"id" doc:"Monitor ID" required:"true"`
}
type UpdateFullInput = httpx.Input[CreateUpdateDto, UpdateFullParams]

type UpdatePartialParams struct {
	Id string `path:"id" doc:"Monitor ID" required:"true"`
}
type UpdatePartialInput = httpx.Input[PartialUpdateDto, UpdatePartialParams]

type DeleteParams struct {
	Id string `path:"id" doc:"Monitor ID" required:"true"`
}
type DeleteInput = httpx.Input[httpx.NoBody, DeleteParams]

type FindByMonitorIDPaginatedParams struct {
	Id        string `path:"id" doc:"Monitor ID" required:"true"`
	Limit     int    `query:"limit" doc:"Number of heartbeats per page" default:"50" minimum:"1" maximum:"1000"`
	Page      int    `query:"page" doc:"Page number (default 0)" default:"0" minimum:"0"`
	Important bool   `query:"important" doc:"Filter by important heartbeats only"`
	Reverse   bool   `query:"reverse" doc:"Reverse the order of heartbeats"`
	ProbeID   string `query:"probe_id" doc:"Only the heartbeats this probe made, by probe id. Omitted, the heartbeats of every probe." example:"local"`
}
type FindByMonitorIDPaginatedInput = httpx.Input[httpx.NoBody, FindByMonitorIDPaginatedParams]

type GetStatPointsParams struct {
	Id          string `path:"id" doc:"Monitor ID" required:"true"`
	Since       string `query:"since" doc:"Start time (RFC3339)" required:"true" format:"date-time"`
	Until       string `query:"until" doc:"End time (RFC3339, default now)" format:"date-time"`
	Granularity string `query:"granularity" doc:"Granularity (minute, hour, day)"`
	ProbeID     string `query:"probe_id" doc:"Only the checks this probe ran, by probe id. Omitted, the checks of every probe count." example:"local"`
}
type GetStatPointsInput = httpx.Input[httpx.NoBody, GetStatPointsParams]

type GetUptimeStatsParams struct {
	Id      string `path:"id" doc:"Monitor ID" required:"true"`
	ProbeID string `query:"probe_id" doc:"Only the checks this probe ran, by probe id. Omitted, the checks of every probe count." example:"local"`
}
type GetUptimeStatsInput = httpx.Input[httpx.NoBody, GetUptimeStatsParams]

type FindByIDsParams struct {
	Ids string `query:"ids" doc:"Comma-separated list of monitor IDs" required:"true"`
}
type FindByIDsInput = httpx.Input[httpx.NoBody, FindByIDsParams]

type ResetMonitorDataParams struct {
	Id string `path:"id" doc:"Monitor ID" required:"true"`
}
type ResetMonitorDataInput = httpx.Input[httpx.NoBody, ResetMonitorDataParams]

type GetTLSInfoParams struct {
	Id string `path:"id" doc:"Monitor ID" required:"true"`
}
type GetTLSInfoInput = httpx.Input[httpx.NoBody, GetTLSInfoParams]
