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

	// ProbeIDs are the probes that run the monitor's checks. Until heartbeats record their probe
	// (#36) a monitor has exactly one.
	ProbeIDs []string `json:"probe_ids"`

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
