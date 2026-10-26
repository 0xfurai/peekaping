package cleanup

import (
	"context"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"

	"go.uber.org/zap"
)

// Retention is how many days the hourly cleanup keeps what has a period of its own. Heartbeats
// aren't here: their data retention period is a setting that users change in the web app.
type Retention struct {
	// NotificationHistoryDays is how long notification history is kept
	NotificationHistoryDays int
	// TLSInfoDays is how long monitor TLS info is kept
	TLSInfoDays int
}

// hourlyCleanup is the job that runs every hour: it deletes the heartbeats and the probes' status
// changes past the data retention period, the expired stats, the old notification history and the
// old TLS info.
// In cluster mode every api replica has one, and the run lock lets one of them run each hour.
type hourlyCleanup struct {
	heartbeats          heartbeat.Service
	settings            setting.Service
	stats               stats.Service
	notificationHistory notification_sent_history.Service
	tlsInfo             monitor_tls_info.Service
	probes              probe.Service
	lock                RunLock
	retention           Retention
	logger              *zap.SugaredLogger
}

// hourOf is the hour whose cleanup a replica that fires at now takes: now rounded to the
// nearest hour, in UTC. The cron fires at the top of the hour by the replica's clock, so a
// timer that wakes just before the hour doesn't claim the hour before, and a run that starts
// late, up to half an hour, still claims its own hour. Replicas whose clocks are apart claim
// the same hour at different moments; the claim's two hour expiry keeps it until the last of
// them fires, as long as their clocks are less than an hour apart.
func hourOf(now time.Time) time.Time {
	return now.Add(30 * time.Minute).UTC().Truncate(time.Hour)
}

// runHourly runs the cleanup of the hour that now falls in, unless another replica has claimed
// it. When the lock can't be reached it skips the hour, because running unguarded could run the
// cleanup on every replica at once. The next hour tries again.
func (h *hourlyCleanup) runHourly(ctx context.Context, now time.Time) {
	hour := hourOf(now)
	label := hour.Format(time.RFC3339)

	claimed, err := h.lock.Claim(ctx, hour)
	if err != nil {
		h.logger.Errorw("Failed to claim the hourly cleanup, skipping it this hour", "hour", label, "error", err)
		return
	}
	if !claimed {
		h.logger.Debugw("Another replica runs the hourly cleanup", "hour", label)
		return
	}

	h.logger.Infow("Running the hourly cleanup", "hour", label)
	cleanupHeartbeats(h.heartbeats, h.settings, h.logger)
	cleanupStats(h.stats, h.logger)
	cleanupNotificationHistory(h.notificationHistory, h.retention.NotificationHistoryDays, h.logger)
	cleanupMonitorTLSInfo(h.tlsInfo, h.retention.TLSInfoDays, h.logger)
	cleanupProbeStatusChanges(h.probes, h.settings, h.logger)
}
