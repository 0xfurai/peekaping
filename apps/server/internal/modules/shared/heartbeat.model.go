package shared

import (
	"time"
)

type MonitorStatus int

const (
	MonitorStatusDown MonitorStatus = iota
	MonitorStatusUp
	MonitorStatusPending
	MonitorStatusMaintenance
)

type HeartBeatModel struct {
	ID        string        `json:"id"`
	MonitorID string        `json:"monitor_id"`
	Status    MonitorStatus `json:"status"`
	Msg       string        `json:"msg"`
	Ping      int           `json:"ping"`
	Duration  int           `json:"duration"`
	DownCount int           `json:"down_count"`
	Retries   int           `json:"retries"`
	Important bool          `json:"important"`
	Time      time.Time     `json:"time"`
	EndTime   time.Time     `json:"end_time"`
	Notified  bool          `json:"notified"`
}

type HeartBeatChartPoint struct {
	Up        int     `json:"up"`
	Down      int     `json:"down"`
	AvgPing   float64 `json:"avgPing"`
	MinPing   int     `json:"minPing"`
	MaxPing   int     `json:"maxPing"`
	Timestamp int64   `json:"timestamp"`
}
