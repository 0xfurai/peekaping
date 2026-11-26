package cleanup

import (
	"context"
	"fmt"
	"time"

	"peekaping/internal/limits"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/reporting"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// readCaps is every workspace's limits, for one run of the cleanup: the run reads them once, so
// that its heartbeat and probe status change cleanups go by the same caps. When they can't be read
// it logs why, and the run goes by the workspaces' own periods alone: a cap only ever shortens a period, so
// that keeps everything the caps would. A nil source is no caps.
func readCaps(ctx context.Context, caps limits.Source, logger *zap.SugaredLogger) limits.Snapshot {
	if caps == nil {
		return limits.Snapshot{}
	}
	snapshot, err := caps.Snapshot(ctx)
	if err != nil {
		logger.Errorw("Failed to read the workspaces' limits, going by their data retention periods alone", "error", err)
		return limits.Snapshot{}
	}
	return snapshot
}

// workspaceCutoff is the start of the workspace's data retention period: what is older is past
// it. The workspace's own period decides, or its plan's cap (limit retention_days, PP-161) when that
// is shorter (docs/adr/0005 section 4). A workspace that has none stored keeps 365 days, as an
// install does that never stored one. It returns false, and logs why, when the period can't be read
// or holds a value the settings API would have refused; what names the data that is then left
// alone: falling back to a default could delete data its users meant to keep.
func workspaceCutoff(ctx context.Context, workspaceSettings setting.WorkspaceService, caps limits.Snapshot, id workspace.ID, what string, logger *zap.SugaredLogger) (time.Time, bool) {
	stored, err := workspaceSettings.GetByKey(ctx, id, setting.DataRetentionPeriodKey)
	if err != nil {
		logger.Errorw("Failed to fetch KEEP_DATA_PERIOD_DAYS setting, not deleting "+what, "workspace", id, "error", err)
		return time.Time{}, false
	}
	keepDays := setting.DefaultDataRetentionDays
	if stored != nil {
		keepDays, err = setting.ParseDataRetentionPeriod(stored.Value)
		if err != nil {
			// A value stored before the settings API checked it.
			logger.Errorw("Invalid KEEP_DATA_PERIOD_DAYS value, not deleting "+what, "workspace", id, "value", stored.Value, "error", err)
			return time.Time{}, false
		}
	}
	if l, _ := caps.For(id); l.RetentionDays != nil && *l.RetentionDays < keepDays {
		keepDays = *l.RetentionDays
	}
	return time.Now().UTC().AddDate(0, 0, -keepDays), true
}

// cleanupHeartbeats goes through the workspaces and deletes each one's heartbeats older than its own
// data retention period, or its cap when that is shorter, so a lowered cap trims the history on the
// next run. caps are the run's, from readCaps. A workspace whose period can't be used is skipped,
// and the others go on.
func cleanupHeartbeats(heartbeatService heartbeat.Service, workspaces workspace.Repository, workspaceSettings setting.WorkspaceService, caps limits.Snapshot, logger *zap.SugaredLogger) {
	ctx := context.Background()
	ids, err := workspaces.ListIDsAnyWorkspace(ctx)
	if err != nil {
		logger.Errorw("Failed to list the workspaces, not deleting heartbeats", "error", err)
		return
	}
	for _, id := range ids {
		cutoff, ok := workspaceCutoff(ctx, workspaceSettings, caps, id, "heartbeats", logger)
		if !ok {
			continue
		}
		deleted, err := heartbeatService.DeleteOlderThan(ctx, id, cutoff)
		if err != nil {
			logger.Errorw("Failed to delete old heartbeats", "workspace", id, "error", err)
			continue
		}
		logger.Infow("Deleted old heartbeats", "workspace", id, "count", deleted, "cutoff", cutoff)
	}
}

// cleanupProbeStatusChanges deletes the probes' status changes past the longest data retention
// period the workspaces keep, each capped as its heartbeats are, except each probe's latest one
// before it, so that the status of a probe at the start of any range is still known. Probes belong to the install, so no one workspace's
// period decides. One with none stored counts as 365 days. If any workspace's period can't be used,
// the run deletes nothing: that period might be the longest, so no cutoff is a safe guess. caps are
// the run's, from readCaps.
func cleanupProbeStatusChanges(probeService probe.Service, workspaces workspace.Repository, workspaceSettings setting.WorkspaceService, caps limits.Snapshot, logger *zap.SugaredLogger) {
	ctx := context.Background()
	ids, err := workspaces.ListIDsAnyWorkspace(ctx)
	if err != nil {
		logger.Errorw("Failed to list the workspaces, not deleting probe status changes", "error", err)
		return
	}
	var cutoff time.Time
	for i, id := range ids {
		workspaceCut, ok := workspaceCutoff(ctx, workspaceSettings, caps, id, "probe status changes", logger)
		if !ok {
			return
		}
		if i == 0 || workspaceCut.Before(cutoff) {
			cutoff = workspaceCut
		}
	}
	if len(ids) == 0 {
		return
	}
	deleted, err := probeService.DeleteStatusChangesOlderThan(ctx, cutoff)
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

// cleanupAccountTokens deletes the account tokens that were used or have expired for more than a day
func cleanupAccountTokens(tokens AccountTokens, now time.Time, logger *zap.SugaredLogger) {
	cutoff := now.UTC().Add(-spentTokenRetention)
	deleted, err := tokens.DeleteSpentTokens(context.Background(), cutoff)
	if err != nil {
		logger.Errorw("Failed to delete spent account tokens", "error", err)
		return
	}
	logger.Infow("Deleted spent account tokens", "count", deleted, "cutoff", cutoff)
}

// hourlySpec is when the cleanup runs: at minute 0 of every hour. It is a variable so that a
// test can make the scheduling fail.
var hourlySpec = "0 * * * *"

// StartCleanupCron schedules the hourly cleanup: the heartbeats past the data retention period of
// their workspace, capped by its limits (caps, nil for none), the probes' status changes past the longest of those periods, the expired
// stats, the old notification history, the old TLS info, the account tokens spent for more than
// a day and the workspaces whose scheduled deletion is due. It returns an error if the cleanup
// can't be scheduled, and then nothing runs.
//
// Every process that calls it fires at the top of the hour, so in cluster mode, where several
// api replicas do, the cleanup runs under lock: the first replica to claim the hour runs it and
// the others skip it. Single mode passes a lock that always grants the run.
//
// Stop the returned cron to stop the cleanup: its Stop returns a context that is done once the
// job running then has finished.
func StartCleanupCron(
	heartbeatService heartbeat.Service,
	workspaces workspace.Repository,
	workspaceSettings setting.WorkspaceService,
	caps limits.Source,
	statsService stats.Service,
	notificationHistoryService notification_sent_history.Service,
	tlsInfoService monitor_tls_info.Service,
	probeService probe.Service,
	accountTokens AccountTokens,
	workspaceDeleter WorkspaceDeleter,
	lock RunLock,
	retention Retention,
	logger *zap.SugaredLogger,
) (*cron.Cron, error) {
	hourly := &hourlyCleanup{
		heartbeats:          heartbeatService,
		workspaces:          workspaces,
		workspaceSettings:   workspaceSettings,
		caps:                caps,
		stats:               statsService,
		notificationHistory: notificationHistoryService,
		tlsInfo:             tlsInfoService,
		probes:              probeService,
		accountTokens:       accountTokens,
		workspaceDeleter:    workspaceDeleter,
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

// WorkspaceDeleter deletes a workspace and everything in it, if its scheduled deletion is still due
// when it runs. workspace.Service is one.
type WorkspaceDeleter interface {
	DeleteDueAnyWorkspace(ctx context.Context, id workspace.ID, now time.Time) (bool, error)
}

// PurgeScheduledWorkspaces deletes the workspaces whose scheduled deletion is due at now (PP-170),
// each with everything in it. A workspace whose deletion was cancelled or put off since the list
// was read is kept. One that can't be deleted is logged and left for the next hour, and the others
// go on. It is exported so that a test can run it at a time of its own.
func PurgeScheduledWorkspaces(ctx context.Context, workspaces workspace.Repository, deleter WorkspaceDeleter, now time.Time, logger *zap.SugaredLogger) {
	due, err := workspaces.ListDeletionDueAnyWorkspace(ctx, now)
	if err != nil {
		logger.Errorw("Failed to list the workspaces scheduled for deletion, not deleting any", "error", err)
		return
	}
	for _, id := range due {
		deleted, err := deleter.DeleteDueAnyWorkspace(ctx, id, now)
		if err != nil {
			logger.Errorw("Failed to delete a workspace scheduled for deletion", "workspace", id, "error", err)
			continue
		}
		if !deleted {
			// Its owner cancelled the deletion, or scheduled it again, since the list was read
			logger.Infow("Kept a workspace whose scheduled deletion is no longer due", "workspace", id)
			continue
		}
		logger.Infow("Deleted a workspace scheduled for deletion", "workspace", id)
	}
}
