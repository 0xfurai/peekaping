package cleanup

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"
	"peekaping/internal/reporting"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// unsetDataRetentionDays is the data retention period of an install that never
// stored one. It was the default for every install before newInstallDataRetentionDays,
// so upgrading keeps their heartbeats as long as before.
const unsetDataRetentionDays = 365

// newInstallDataRetentionDays is the data retention period a new install starts with.
const newInstallDataRetentionDays = 180

// initDataRetentionPeriod stores newInstallDataRetentionDays when no period is
// stored and the install has neither an admin nor a heartbeat yet, which is
// only true before its first setup. An install set up before that default, even
// one whose heartbeats were all deleted since, keeps unsetDataRetentionDays.
func initDataRetentionPeriod(heartbeatService heartbeat.Service, userRepository auth.Repository, settingService setting.Service, logger *zap.SugaredLogger) {
	ctx := context.Background()
	stored, err := settingService.GetByKey(ctx, setting.DataRetentionPeriodKey)
	if err != nil {
		logger.Errorw("Failed to read the data retention period", "error", err)
		return
	}
	if stored != nil {
		return
	}
	users, err := userRepository.FindAllCount(ctx)
	if err != nil {
		logger.Errorw("Failed to count users, not storing a data retention period", "error", err)
		return
	}
	if users > 0 {
		return
	}
	hasHeartbeats, err := heartbeatService.HasAny(ctx)
	if err != nil {
		logger.Errorw("Failed to check for heartbeats, not storing a data retention period", "error", err)
		return
	}
	if hasHeartbeats {
		return
	}
	_, err = settingService.SetByKey(ctx, setting.DataRetentionPeriodKey, &setting.Model{
		Value: strconv.Itoa(newInstallDataRetentionDays),
		Type:  "int",
	})
	if err != nil {
		logger.Errorw("Failed to store the data retention period", "error", err)
		return
	}
	logger.Infow("Stored the data retention period for a new install", "days", newInstallDataRetentionDays)
}

// dataRetentionCutoff is the start of the data retention period: what is older is past it. It
// returns false, and logs why, when the period can't be read; what names the data that is then
// left alone.
func dataRetentionCutoff(settingService setting.Service, what string, logger *zap.SugaredLogger) (time.Time, bool) {
	keepDays := unsetDataRetentionDays
	settingModel, err := settingService.GetByKey(context.Background(), setting.DataRetentionPeriodKey)
	if err != nil {
		logger.Errorw("Failed to fetch KEEP_DATA_PERIOD_DAYS setting, not deleting "+what, "error", err)
		return time.Time{}, false
	}
	if settingModel != nil {
		keepDays, err = setting.ParseDataRetentionPeriod(settingModel.Value)
		if err != nil {
			// A value stored before the settings API checked it. Falling back to
			// a default could delete data the user meant to keep.
			logger.Errorw("Invalid KEEP_DATA_PERIOD_DAYS value, not deleting "+what, "value", settingModel.Value, "error", err)
			return time.Time{}, false
		}
	}
	return time.Now().UTC().AddDate(0, 0, -keepDays), true
}

func cleanupHeartbeats(heartbeatService heartbeat.Service, settingService setting.Service, logger *zap.SugaredLogger) {
	cutoff, ok := dataRetentionCutoff(settingService, "heartbeats", logger)
	if !ok {
		return
	}
	deleted, err := heartbeatService.DeleteOlderThan(context.Background(), cutoff)
	if err != nil {
		logger.Errorw("Failed to delete old heartbeats", "error", err)
		return
	}
	logger.Infow("Deleted old heartbeats", "count", deleted, "cutoff", cutoff)
}

// cleanupProbeStatusChanges deletes the probes' status changes past the data retention period,
// except each probe's latest one before it, so that the status of a probe at the start of any
// range is still known.
func cleanupProbeStatusChanges(probeService probe.Service, settingService setting.Service, logger *zap.SugaredLogger) {
	cutoff, ok := dataRetentionCutoff(settingService, "probe status changes", logger)
	if !ok {
		return
	}
	deleted, err := probeService.DeleteStatusChangesOlderThan(context.Background(), cutoff)
	if err != nil {
		logger.Errorw("Failed to delete old probe status changes", "error", err)
		return
	}
	logger.Infow("Deleted old probe status changes", "count", deleted, "cutoff", cutoff)
}

func cleanupStats(statsService stats.Service, logger *zap.SugaredLogger) {
	deleted, err := statsService.DeleteExpired(context.Background(), time.Now().UTC())
	if err != nil {
		logger.Errorw("Failed to delete expired stats", "count", deleted, "error", err)
		return
	}
	logger.Infow("Deleted expired stats", "count", deleted)
}

func cleanupNotificationHistory(notificationHistoryService notification_sent_history.Service, olderThanDays int, logger *zap.SugaredLogger) {
	logger.Info("Cleaning up old notification history records...")

	err := notificationHistoryService.CleanupOldRecords(context.Background(), olderThanDays)
	if err != nil {
		logger.Errorw("Failed to cleanup notification history", "error", err)
		return
	}

	logger.Infow("Successfully cleaned up notification history records", "older_than_days", olderThanDays)
}

func cleanupMonitorTLSInfo(tlsInfoService monitor_tls_info.Service, olderThanDays int, logger *zap.SugaredLogger) {
	logger.Info("Cleaning up old monitor TLS info records...")

	err := tlsInfoService.CleanupOldRecords(context.Background(), olderThanDays)
	if err != nil {
		logger.Errorw("Failed to cleanup monitor TLS info", "error", err)
		return
	}

	logger.Infow("Successfully cleaned up monitor TLS info records", "older_than_days", olderThanDays)
}

// hourlySpec is when the cleanup runs: at minute 0 of every hour. It is a variable so that a
// test can make the scheduling fail.
var hourlySpec = "0 * * * *"

// StartCleanupCron stores the data retention period of a new install, then schedules the
// hourly cleanup: the heartbeats and the probes' status changes past the data retention period,
// the expired stats, the old notification history and the old TLS info. It returns an error if the cleanup can't be
// scheduled, and then nothing runs.
//
// Every process that calls it fires at the top of the hour, so in cluster mode, where several
// api replicas do, the cleanup runs under lock: the first replica to claim the hour runs it and
// the others skip it. Single mode passes a lock that always grants the run.
//
// Stop the returned cron to stop the cleanup: its Stop returns a context that is done once the
// job running then has finished.
func StartCleanupCron(
	heartbeatService heartbeat.Service,
	userRepository auth.Repository,
	settingService setting.Service,
	statsService stats.Service,
	notificationHistoryService notification_sent_history.Service,
	tlsInfoService monitor_tls_info.Service,
	probeService probe.Service,
	lock RunLock,
	retention Retention,
	logger *zap.SugaredLogger,
) (*cron.Cron, error) {
	initDataRetentionPeriod(heartbeatService, userRepository, settingService, logger)

	hourly := &hourlyCleanup{
		heartbeats:          heartbeatService,
		settings:            settingService,
		stats:               statsService,
		notificationHistory: notificationHistoryService,
		tlsInfo:             tlsInfoService,
		probes:              probeService,
		lock:                lock,
		retention:           retention,
		logger:              logger,
	}

	// The top of the hour in UTC, whatever TZ is: the claims round the time to the nearest UTC
	// hour, so replicas must fire at the UTC hour, not at the top of a local hour that lies
	// half an hour off it (India's, for one).
	c := cron.New(cron.WithLocation(time.UTC))
	// cron runs each job on a goroutine of its own, and has no recover here: a panic in the job
	// ends the process. The guard reports it first, then lets it crash as before.
	_, err := c.AddFunc(hourlySpec, func() {
		reporting.Guard("cleanup", func() { hourly.runHourly(context.Background(), time.Now()) })
	})
	if err != nil {
		return nil, fmt.Errorf("failed to schedule the hourly cleanup (%q): %w", hourlySpec, err)
	}

	c.Start()
	return c, nil
}
