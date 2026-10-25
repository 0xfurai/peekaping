package monitor

import (
	"encoding/json"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"
	"peekaping/internal/utils"
)

type CreateUpdateDto struct {
	Type            string   `json:"type" validate:"required" example:"http"`
	Name            string   `json:"name" validate:"required,min=3" example:"My Monitor"`
	Interval        int      `json:"interval" validate:"min=20" example:"60"`
	MaxRetries      int      `json:"max_retries" validate:"min=0" example:"3"`
	RetryInterval   int      `json:"retry_interval" validate:"min=20" example:"60"`
	Timeout         int      `json:"timeout" validate:"min=16" example:"16"`
	ResendInterval  int      `json:"resend_interval" validate:"min=0" example:"10"`
	Active          bool     `json:"active" example:"true"`
	NotificationIds []string `json:"notification_ids" validate:"required" example:"6830ad485361f19c598d6d90"`
	TagIds          []string `json:"tag_ids" example:"6830ad485361f19c598d6d90,6830ad485361f19c598d6d91"`
	ProxyId         string   `json:"proxy_id" example:"6830ad485361f19c598d6d90"`
	Config          string   `json:"config"`
	PushToken       string   `json:"push_token"`
	// ProbeIDs are the probes that run the monitor, one or more. A create without it runs on the
	// local probe, and an update without it keeps the monitor's probes.
	ProbeIDs []string `json:"probe_ids" doc:"The probes that run the monitor's checks, by id: one or more, none twice, and each must run the monitor's type. Omitted on create, the monitor runs on local; omitted on update, it keeps its probes. A push monitor runs on local only." example:"local"`
	// LocationRule and LocationThreshold are how many of the monitor's locations must be Down for
	// it to be Down. Without a rule a create is any and an update keeps the monitor's rule.
	LocationRule      shared.LocationRule `json:"location_rule,omitempty" enum:"any,all,at_least" doc:"How many of the monitor's locations must be Down for the monitor to be Down: any (one is enough), all (every location that counts) or at_least (location_threshold of them). Omitted on create, the rule is any; omitted on update, the monitor keeps its rule. Locations whose probe is offline, or that haven't reported, don't count." example:"any"`
	LocationThreshold *int                `json:"location_threshold,omitempty" doc:"With the at_least rule, how many locations must be Down: from 1 to the number of probes the monitor runs on. Required with at_least and not allowed with any other rule." example:"2"`
	activeSet         bool
}

// UnmarshalJSON records whether active was present so create requests can apply
// the historical default without changing full-update semantics.
func (dto *CreateUpdateDto) UnmarshalJSON(data []byte) error {
	type plain CreateUpdateDto
	*dto = CreateUpdateDto{}
	if err := json.Unmarshal(data, (*plain)(dto)); err != nil {
		return err
	}
	var err error
	dto.activeSet, err = utils.HasJSONKey(data, "active")
	return err
}

// ApplyCreateDefaults keeps omitted active fields active for create requests.
// Full updates keep their existing zero-value semantics.
func (dto *CreateUpdateDto) ApplyCreateDefaults() {
	if dto == nil {
		return
	}
	if !dto.activeSet {
		dto.Active = true
	}
}

type PartialUpdateDto struct {
	Name            *string                  `json:"name,omitempty" example:"My Monitor"`
	Interval        *int                     `json:"interval,omitempty" example:"60"`
	Timeout         *int                     `json:"timeout,omitempty" example:"16"`
	Type            *string                  `json:"type,omitempty" example:"http"`
	MaxRetries      *int                     `json:"max_retries,omitempty" example:"3"`
	RetryInterval   *int                     `json:"retry_interval,omitempty" example:"60"`
	ResendInterval  *int                     `json:"resend_interval,omitempty" example:"10"`
	Active          *bool                    `json:"active,omitempty" example:"true"`
	NotificationIds []string                 `json:"notification_ids,omitempty" example:"6830ad485361f19c598d6d90"`
	TagIds          []string                 `json:"tag_ids,omitempty" example:"6830ad485361f19c598d6d90,6830ad485361f19c598d6d91"`
	ProxyId         *string                  `json:"proxy_id,omitempty" example:"6830ad485361f19c598d6d90"`
	Status          *heartbeat.MonitorStatus `json:"status,omitempty" example:"1"`
	Config          *string                  `json:"config,omitempty"`
	PushToken       *string                  `json:"push_token,omitempty"`
	// ProbeIDs are the probes that run the monitor, one or more. Without it the monitor keeps its
	// probes.
	ProbeIDs []string `json:"probe_ids,omitempty" doc:"The probes that run the monitor's checks, by id: one or more, none twice, and each must run the monitor's type. Omitted, the monitor keeps its probes. A push monitor runs on local only." example:"local"`
	// LocationRule and LocationThreshold replace the monitor's location rule. Sent alone, a
	// threshold changes the one the monitor's at_least rule has. Without either, the monitor keeps
	// its rule, which is checked against the probes it has after the update.
	LocationRule      *shared.LocationRule `json:"location_rule,omitempty" enum:"any,all,at_least" doc:"How many of the monitor's locations must be Down for the monitor to be Down: any, all or at_least. Omitted, the monitor keeps its rule." example:"any"`
	LocationThreshold *int                 `json:"location_threshold,omitempty" doc:"With the at_least rule, how many locations must be Down: from 1 to the number of probes the monitor runs on. Sent with a rule other than at_least it is rejected; a rule sent without it has none." example:"2"`
}

// UptimeStatsDto represents uptime percentages for various periods
// All values are percentages (0-100)
type UptimeStatsDto struct {
	Uptime24h  float64 `json:"24h"`
	Uptime7d   float64 `json:"7d"`
	Uptime30d  float64 `json:"30d"`
	Uptime365d float64 `json:"365d"`
}

// MonitorResponseDto is a monitor as the API returns it: the monitor itself and the IDs of the
// notification channels and tags linked to it. A monitor with none gets empty lists.
type MonitorResponseDto struct {
	shared.Monitor
	NotificationIds []string `json:"notification_ids" nullable:"false" example:"6830ad485361f19c598d6d90"`
	TagIds          []string `json:"tag_ids" nullable:"false" example:"6830ad485361f19c598d6d90,6830ad485361f19c598d6d91"`
}

// StatPointsSummaryDto represents stat points and summary for a period
type StatPointsSummaryDto struct {
	ProbeID string       `json:"probe_id,omitempty" doc:"The probe the figures count. Omitted when they count every probe's checks." example:"local"`
	Points  []*StatPoint `json:"points"`
	MaxPing *float64     `json:"maxPing"`
	MinPing *float64     `json:"minPing"`
	AvgPing *float64     `json:"avgPing"`
	Uptime  *float64     `json:"uptime"`
}

// CustomUptimeStatsDto represents uptime percentages for 24h, 30d, 365d
// All values are percentages (0-100)
type CustomUptimeStatsDto struct {
	ProbeID    string  `json:"probe_id,omitempty" doc:"The probe the percentages count. Omitted when they count every probe's checks." example:"local"`
	Uptime24h  float64 `json:"24h"`
	Uptime7d   float64 `json:"7d"`
	Uptime30d  float64 `json:"30d"`
	Uptime365d float64 `json:"365d"`
}
