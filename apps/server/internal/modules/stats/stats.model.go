package stats

import (
	"time"
)

type StatPeriod string

const (
	StatMinutely StatPeriod = "minutely"
	StatHourly   StatPeriod = "hourly"
	StatDaily    StatPeriod = "daily"
)

type Stat struct {
	ID          string    `json:"id"`
	MonitorID   string    `json:"monitor_id"`
	Timestamp   time.Time `json:"timestamp"`
	Ping        float64   `json:"ping"` // average response time over PingCount checks
	PingMin     float64   `json:"ping_min"`
	PingMax     float64   `json:"ping_max"`
	PingCount   int       `json:"ping_count"` // checks with status Up; Up also counts maintenance checks, which have no response time
	Up          int       `json:"up"`
	Down        int       `json:"down"`
	Maintenance int       `json:"maintenance"`
}
