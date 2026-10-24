package shared

import (
	"time"
)

type Monitor struct {
	ID string `json:"id"`

	// connection type: http, tcp, ping, etc
	Type string `json:"type" validate:"required" example:"http"`

	// monitor name
	Name string `json:"name" example:"Monitor"`

	// monitor url
	// Url string `json:"url" example:"https://example.com"`

	// monitor interval in seconds to do request to url
	Interval int `json:"interval" example:"60"`

	// monitor timeout in seconds to do request otherwise stop request
	Timeout int `json:"timeout" example:"16"`

	// Maximum retries before the service is marked as down and a notification is sent
	MaxRetries int `json:"max_retries" example:"3"`

	// Retry interval in seconds to do request to url
	RetryInterval int `json:"retry_interval" example:"60"`

	// Resend Notification if Down X times consecutively
	ResendInterval int `json:"resend_interval" example:"10"`

	Active bool          `json:"active"`
	Status MonitorStatus `json:"status"`

	Config    string `json:"config"`
	ProxyId   string `json:"proxy_id"`
	PushToken string `json:"push_token"`

	// ProbeIDs are the probes that run the monitor's checks: one or more, in id order.
	ProbeIDs []string `json:"probe_ids" nullable:"false" doc:"The probes that run the monitor's checks, by id: one or more. local is Peekaping itself."`

	// Locations are the probes in ProbeIDs, in the same order, with how the monitor is doing on
	// each. Only a repository that reads the probes fills them in.
	Locations []MonitorLocation `json:"locations" nullable:"false" doc:"Where the monitor runs and how it is doing there, one entry for each probe in probe_ids and in the same order"`

	// Names of the monitor's tags. Only the notification listener fills them in,
	// for the providers to send; the API returns tag IDs instead.
	Tags []string `json:"-"`

	// Last heartbeat for push monitors
	LastHeartbeat *HeartBeatModel `json:"last_heartbeat,omitempty"`

	// The later of the latest heartbeat and the monitor's last save or resume (so
	// its updated_at when it has no heartbeat), set only while the stale-monitor
	// watchdog counts it as stale. The API fills it in; it isn't stored.
	StaleSince *time.Time `json:"stale_since,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LocalProbeID is the id of the local probe: the checks that Peekaping runs itself
const LocalProbeID = "local"

// RunsOn reports whether the probe runs the monitor's checks. A monitor read without its probes
// runs on the local probe: that is every monitor of an install from before probes, and a
// monitor event from a process that isn't upgraded yet.
func (m *Monitor) RunsOn(probeID string) bool {
	if len(m.ProbeIDs) == 0 {
		return probeID == LocalProbeID
	}
	for _, id := range m.ProbeIDs {
		if id == probeID {
			return true
		}
	}
	return false
}

type UpdateMonitor struct {
	ID             *string        `json:"id"`
	Type           *string        `json:"type"`
	Name           *string        `json:"name"`
	Interval       *int           `json:"interval"`
	Timeout        *int           `json:"timeout"`
	MaxRetries     *int           `json:"max_retries"`
	RetryInterval  *int           `json:"retry_interval"`
	ResendInterval *int           `json:"resend_interval"`
	Active         *bool          `json:"active"`
	Status         *MonitorStatus `json:"status"`
	Config         *string        `json:"config"`
	ProxyId        *string        `json:"proxy_id"`
	PushToken      *string        `json:"push_token"`
	// ProbeIDs replace the probes that run the monitor. Nil leaves them as they are.
	ProbeIDs []string `json:"probe_ids"`

	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// MonitorLocation is where a monitor runs and how it is doing there: one probe it is assigned to,
// and the status of that probe's latest heartbeat.
type MonitorLocation struct {
	ProbeID   string `json:"probe_id" doc:"The probe the monitor runs on" example:"local"`
	ProbeName string `json:"probe_name" doc:"The probe's name" example:"Local"`
	// Online is whether the probe has an instance connected. The local probe is always online.
	Online bool `json:"online" doc:"Whether the probe has an instance connected. The local probe is always online."`
	// Status is the status of the probe's latest heartbeat, nil until the probe has sent one
	Status          *MonitorStatus `json:"status" nullable:"true" doc:"The status of this probe's latest heartbeat: 0 Down, 1 Up, 2 Pending, 3 Maintenance. Null until the probe reports." example:"1"`
	LastPing        *int           `json:"last_ping" nullable:"true" doc:"The response time of this probe's latest heartbeat, in milliseconds. Null until the probe reports." example:"120"`
	LastHeartbeatAt *time.Time     `json:"last_heartbeat_at" nullable:"true" doc:"When this probe's latest heartbeat was made. Null until the probe reports."`
}

// LocationState is what a result leaves on a monitor's row for the probe that ran it
type LocationState struct {
	ProbeID         string
	Status          *MonitorStatus
	LastPing        *int
	LastHeartbeatAt *time.Time
}
