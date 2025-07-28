package cleanup

import (
	"context"
	"strconv"
	"time"

	"peekaping/src/modules/heartbeat"
	"peekaping/src/modules/monitor_tls_info"
	"peekaping/src/modules/notification_sent_history"
	"peekaping/src/modules/setting"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

func cleanupHeartbeats(heartbeatService heartbeat.Service, settingService setting.Service, logger *zap.SugaredLogger) {
	keepDays := 365 // default fallback
	settingModel, err := settingService.GetByKey(context.Background(), "KEEP_DATA_PERIOD_DAYS")
	if err != nil {
		logger.Errorw("Failed to fetch KEEP_DATA_PERIOD_DAYS setting", "error", err)
	} else if settingModel != nil {
		if v, err := strconv.Atoi(settingModel.Value); err == nil {
			keepDays = v
		} else {
			logger.Errorw("Invalid KEEP_DATA_PERIOD_DAYS value", "value", settingModel.Value, "error", err)
		}
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -keepDays)
	deleted, err := heartbeatService.DeleteOlderThan(context.Background(), cutoff)
	if err != nil {
		logger.Errorw("Failed to delete old heartbeats", "error", err)
		return
	}
	logger.Infow("Deleted old heartbeats", "count", deleted, "cutoff", cutoff)
}

func cleanupNotificationHistory(notificationHistoryService notification_sent_history.Service, logger *zap.SugaredLogger) {
	logger.Info("Cleaning up old notification history records...")

	// Clean up records older than 90 days
	olderThanDays := 90
	err := notificationHistoryService.CleanupOldRecords(context.Background(), olderThanDays)
	if err != nil {
		logger.Errorw("Failed to cleanup notification history", "error", err)
		return
	}

	logger.Infow("Successfully cleaned up notification history records", "older_than_days", olderThanDays)
}

func cleanupMonitorTLSInfo(tlsInfoService monitor_tls_info.Service, logger *zap.SugaredLogger) {
	logger.Info("Cleaning up old monitor TLS info records...")

	// Clean up records older than 30 days (shorter than notification history)
	olderThanDays := 30
	err := tlsInfoService.CleanupOldRecords(context.Background(), olderThanDays)
	if err != nil {
		logger.Errorw("Failed to cleanup monitor TLS info", "error", err)
		return
	}

	logger.Infow("Successfully cleaned up monitor TLS info records", "older_than_days", olderThanDays)
}

// StartCleanupCron starts the general cleanup cron job(s).
func StartCleanupCron(
	heartbeatService heartbeat.Service,
	settingService setting.Service,
	notificationHistoryService notification_sent_history.Service,
	tlsInfoService monitor_tls_info.Service,
	logger *zap.SugaredLogger,
) {
	c := cron.New()

	c.AddFunc("0 * * * *", func() {
		cleanupHeartbeats(heartbeatService, settingService, logger)
	})

	c.AddFunc("0 * * * *", func() {
		cleanupNotificationHistory(notificationHistoryService, logger)
	})

	c.AddFunc("0 * * * *", func() {
		cleanupMonitorTLSInfo(tlsInfoService, logger)
	})

	c.Start()
}
