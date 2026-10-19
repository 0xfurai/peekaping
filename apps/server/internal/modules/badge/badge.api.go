package badge

import (
	"peekaping/internal/httpx"
)

type GetStatusBadgeParams struct {
	MonitorId   string `path:"monitorId" doc:"Monitor ID" required:"true"`
	Style       string `query:"style" doc:"Badge style (flat, flat-square, plastic, for-the-badge, social)"`
	UpLabel     string `query:"upLabel" doc:"Label when monitor is up"`
	DownLabel   string `query:"downLabel" doc:"Label when monitor is down"`
	UpColor     string `query:"upColor" doc:"Color when monitor is up"`
	DownColor   string `query:"downColor" doc:"Color when monitor is down"`
	Color       string `query:"color" doc:"Invalid options fall back to defaults."`
	LabelColor  string `query:"labelColor" doc:"Invalid options fall back to defaults."`
	LabelPrefix string `query:"labelPrefix" doc:"Invalid options fall back to defaults."`
	Label       string `query:"label" doc:"Invalid options fall back to defaults."`
	LabelSuffix string `query:"labelSuffix" doc:"Invalid options fall back to defaults."`
	Prefix      string `query:"prefix" doc:"Invalid options fall back to defaults."`
	Suffix      string `query:"suffix" doc:"Invalid options fall back to defaults."`
	WarnDays    string `query:"warnDays" doc:"Invalid options fall back to defaults."`
	DownDays    string `query:"downDays" doc:"Invalid options fall back to defaults."`
}
type GetStatusBadgeInput = httpx.Input[httpx.NoBody, GetStatusBadgeParams]

type GetUptimeBadgeParams struct {
	MonitorId   string `path:"monitorId" doc:"Monitor ID" required:"true"`
	Duration    string `path:"duration" doc:"Hours; absent or invalid values use 24."`
	Style       string `query:"style" doc:"Badge style"`
	Label       string `query:"label" doc:"Custom label"`
	Suffix      string `query:"suffix" doc:"Value suffix"`
	Color       string `query:"color" doc:"Badge color"`
	LabelColor  string `query:"labelColor" doc:"Invalid options fall back to defaults."`
	UpLabel     string `query:"upLabel" doc:"Invalid options fall back to defaults."`
	DownLabel   string `query:"downLabel" doc:"Invalid options fall back to defaults."`
	UpColor     string `query:"upColor" doc:"Invalid options fall back to defaults."`
	DownColor   string `query:"downColor" doc:"Invalid options fall back to defaults."`
	LabelPrefix string `query:"labelPrefix" doc:"Invalid options fall back to defaults."`
	LabelSuffix string `query:"labelSuffix" doc:"Invalid options fall back to defaults."`
	Prefix      string `query:"prefix" doc:"Invalid options fall back to defaults."`
	WarnDays    string `query:"warnDays" doc:"Invalid options fall back to defaults."`
	DownDays    string `query:"downDays" doc:"Invalid options fall back to defaults."`
}
type GetUptimeBadgeInput = httpx.Input[httpx.NoBody, GetUptimeBadgeParams]

type GetPingBadgeParams struct {
	MonitorId   string `path:"monitorId" doc:"Monitor ID" required:"true"`
	Duration    string `path:"duration" doc:"Hours; absent or invalid values use 24."`
	Style       string `query:"style" doc:"Badge style"`
	Label       string `query:"label" doc:"Custom label"`
	Suffix      string `query:"suffix" doc:"Value suffix"`
	Color       string `query:"color" doc:"Badge color"`
	LabelColor  string `query:"labelColor" doc:"Invalid options fall back to defaults."`
	UpLabel     string `query:"upLabel" doc:"Invalid options fall back to defaults."`
	DownLabel   string `query:"downLabel" doc:"Invalid options fall back to defaults."`
	UpColor     string `query:"upColor" doc:"Invalid options fall back to defaults."`
	DownColor   string `query:"downColor" doc:"Invalid options fall back to defaults."`
	LabelPrefix string `query:"labelPrefix" doc:"Invalid options fall back to defaults."`
	LabelSuffix string `query:"labelSuffix" doc:"Invalid options fall back to defaults."`
	Prefix      string `query:"prefix" doc:"Invalid options fall back to defaults."`
	WarnDays    string `query:"warnDays" doc:"Invalid options fall back to defaults."`
	DownDays    string `query:"downDays" doc:"Invalid options fall back to defaults."`
}
type GetPingBadgeInput = httpx.Input[httpx.NoBody, GetPingBadgeParams]

type GetCertExpBadgeParams struct {
	MonitorId   string `path:"monitorId" doc:"Monitor ID" required:"true"`
	Style       string `query:"style" doc:"Badge style"`
	Label       string `query:"label" doc:"Custom label"`
	WarnDays    int    `query:"warnDays" doc:"Days before expiry to show warning"`
	DownDays    int    `query:"downDays" doc:"Days before expiry to show as down"`
	Color       string `query:"color" doc:"Invalid options fall back to defaults."`
	LabelColor  string `query:"labelColor" doc:"Invalid options fall back to defaults."`
	UpLabel     string `query:"upLabel" doc:"Invalid options fall back to defaults."`
	DownLabel   string `query:"downLabel" doc:"Invalid options fall back to defaults."`
	UpColor     string `query:"upColor" doc:"Invalid options fall back to defaults."`
	DownColor   string `query:"downColor" doc:"Invalid options fall back to defaults."`
	LabelPrefix string `query:"labelPrefix" doc:"Invalid options fall back to defaults."`
	LabelSuffix string `query:"labelSuffix" doc:"Invalid options fall back to defaults."`
	Prefix      string `query:"prefix" doc:"Invalid options fall back to defaults."`
	Suffix      string `query:"suffix" doc:"Invalid options fall back to defaults."`
}
type GetCertExpBadgeInput = httpx.Input[httpx.NoBody, GetCertExpBadgeParams]

type GetResponseBadgeParams struct {
	MonitorId   string `path:"monitorId" doc:"Monitor ID" required:"true"`
	Style       string `query:"style" doc:"Badge style"`
	Label       string `query:"label" doc:"Custom label"`
	Suffix      string `query:"suffix" doc:"Value suffix"`
	Color       string `query:"color" doc:"Badge color"`
	LabelColor  string `query:"labelColor" doc:"Invalid options fall back to defaults."`
	UpLabel     string `query:"upLabel" doc:"Invalid options fall back to defaults."`
	DownLabel   string `query:"downLabel" doc:"Invalid options fall back to defaults."`
	UpColor     string `query:"upColor" doc:"Invalid options fall back to defaults."`
	DownColor   string `query:"downColor" doc:"Invalid options fall back to defaults."`
	LabelPrefix string `query:"labelPrefix" doc:"Invalid options fall back to defaults."`
	LabelSuffix string `query:"labelSuffix" doc:"Invalid options fall back to defaults."`
	Prefix      string `query:"prefix" doc:"Invalid options fall back to defaults."`
	WarnDays    string `query:"warnDays" doc:"Invalid options fall back to defaults."`
	DownDays    string `query:"downDays" doc:"Invalid options fall back to defaults."`
}
type GetResponseBadgeInput = httpx.Input[httpx.NoBody, GetResponseBadgeParams]
