package monitor_notification

type CreateDto struct {
	MonitorID      string `json:"monitor_id"`
	NotificationID string `json:"notification_id"`
}
