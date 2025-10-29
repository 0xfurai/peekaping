package heartbeat

import (
	"time"
)

type CreateUpdateDto struct {
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
