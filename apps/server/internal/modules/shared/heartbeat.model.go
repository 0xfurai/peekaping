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
	ID         string        `json:"id"`
	MonitorID  string        `json:"monitor_id"`
	ProbeID    string        `json:"probe_id" doc:"The probe that ran the check: local for the local probe, otherwise a remote probe's id" example:"local"`
	InstanceID *string       `json:"instance_id" nullable:"true" doc:"The probe instance that ran the check, as the probe protocol names it. Null for a push, for a check the peekaping process ran itself in single mode, and for a heartbeat stored before instances were recorded." example:"5b0c7d0e-3f4c-4c47-9a5b-2f1d8d8f6a11"`
	Status     MonitorStatus `json:"status"`
	Msg        string        `json:"msg"`
	Ping       int           `json:"ping"`
	Duration   int           `json:"duration"`
	DownCount  int           `json:"down_count"`
	Retries    int           `json:"retries"`
	Important  bool          `json:"important"`
	Time       time.Time     `json:"time"`
	EndTime    time.Time     `json:"end_time"`
	Notified   bool          `json:"notified"`
}

type HeartBeatChartPoint struct {
	Up        int     `json:"up"`
	Down      int     `json:"down"`
	AvgPing   float64 `json:"avgPing"`
	MinPing   int     `json:"minPing"`
	MaxPing   int     `json:"maxPing"`
	Timestamp int64   `json:"timestamp"`
}
