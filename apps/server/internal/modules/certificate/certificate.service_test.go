package certificate_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"peekaping/internal/modules/workspace/workspacetest"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/monitor_tls_info"
	"peekaping/internal/modules/notification_sent_history"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// recordingNotificationService stands in for the notification channels. It
// keeps the threshold of every certificate expiry notification it was asked
// to send.
type recordingNotificationService struct {
	targets []int
	err     error // returned from every send when set
}

func (r *recordingNotificationService) SendCertificateExpiryNotification(_ context.Context, _ string, _ string, _ *certificate.CertificateInfo, _ int, targetDays int) error {
	r.targets = append(r.targets, targetDays)
	return r.err
}

// harness is the certificate service over a migrated SQLite database, with
// real settings, sent-history and TLS info services.
type harness struct {
	svc           certificate.Service
	notifications *recordingNotificationService
	settings      shared.WorkspaceSettingService
	db            *bun.DB
	workspaceID   workspace.ID
	monitorID     string
	logs          *observer.ObservedLogs
}

// addMonitorIn stores a monitor in a new workspace and returns the workspace and the monitor
func (h *harness) addMonitorIn(t *testing.T) (workspace.ID, string) {
	t.Helper()
	id := workspace.ID(uuid.NewString())
	workspacetest.Add(t, h.db, id, "Other")
	monitorID := uuid.NewString()
	_, err := h.db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, ?, 'http', 'Other monitor', 60, 10, 1, 10, 0)`, monitorID, string(id))
	require.NoError(t, err)
	return id, monitorID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "certificate.db"))
	require.NoError(t, err)
	// As the server opens SQLite: the foreign keys hold, on the one connection this sets them on
	sqlDB.SetMaxOpenConns(1)
	_, err = sqlDB.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	db := bun.NewDB(sqlDB, sqlitedialect.New())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)
	monitorID := uuid.NewString()
	workspacetest.OldestOrAdd(t, db)
	_, err = db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, (SELECT id FROM workspaces ORDER BY created_at, id LIMIT 1), 'http', 'Monitor', 60, 10, 1, 10, 0)`, monitorID)
	require.NoError(t, err)

	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core).Sugar()
	settings := setting.NewWorkspaceService(setting.NewSQLWorkspaceRepository(db))
	notifications := &recordingNotificationService{}
	svc := certificate.NewService(
		settings,
		notifications,
		notification_sent_history.NewService(notification_sent_history.NewSQLRepository(db), logger),
		monitor_tls_info.NewService(monitor_tls_info.NewSQLRepository(db), logger),
		logger,
	)
	var workspaceID string
	require.NoError(t, db.NewSelect().Table("monitors").Column("workspace_id").Where("id = ?", monitorID).Scan(ctx, &workspaceID))
	return &harness{svc, notifications, settings, db, workspace.ID(workspaceID), monitorID, logs}
}

// check does what the ingester does after a TLS check: store the TLS info,
// then check expiry. It returns the thresholds notified by this check.
func (h *harness) check(t *testing.T, fingerprint string, daysRemaining int) []int {
	t.Helper()
	return h.checkValidTo(t, fingerprint, daysRemaining, time.Time{})
}

// checkValidTo is check for a certificate whose expiry time is known.
func (h *harness) checkValidTo(t *testing.T, fingerprint string, daysRemaining int, validTo time.Time) []int {
	t.Helper()
	ctx := context.Background()
	tlsInfo := &certificate.TLSInfo{Valid: true, CertInfo: &certificate.CertificateInfo{
		Subject:        "CN=example.com",
		Fingerprint256: fingerprint,
		DaysRemaining:  daysRemaining,
		ValidTo:        validTo,
		CertType:       "server",
	}}
	before := len(h.notifications.targets)
	require.NoError(t, h.svc.UpdateTLSInfo(ctx, h.monitorID, tlsInfo))
	require.NoError(t, h.svc.CheckCertificateExpiry(ctx, h.workspaceID, tlsInfo, h.monitorID, "Monitor"))
	return h.notifications.targets[before:]
}

const certA, certB = "AA:AA", "BB:BB"

func TestCheckCertificateExpiry_NotifiesOnceAtEachThreshold(t *testing.T) {
	h := newHarness(t)

	assert.Empty(t, h.check(t, certA, 25), "25 days is outside every threshold")
	assert.Equal(t, []int{21}, h.check(t, certA, 20))
	assert.Empty(t, h.check(t, certA, 19), "21 was already sent")
	assert.Equal(t, []int{14}, h.check(t, certA, 13), "a newly crossed threshold notifies")
	assert.Empty(t, h.check(t, certA, 13), "14 was already sent")
	assert.Equal(t, []int{7}, h.check(t, certA, 6))
	assert.Empty(t, h.check(t, certA, 5), "7 was already sent")
	assert.Equal(t, []int{21, 14, 7}, h.notifications.targets)
}

func TestCheckCertificateExpiry_FirstSeenInsideSeveralThresholdsNotifiesTheNearest(t *testing.T) {
	h := newHarness(t)

	assert.Equal(t, []int{7}, h.check(t, certA, 5))
	assert.Empty(t, h.check(t, certA, 4))
}

// A failed send is retried on the next check for the same threshold; it
// doesn't fall back to the farther thresholds.
func TestCheckCertificateExpiry_FailedSendDoesNotFallBackToFartherThresholds(t *testing.T) {
	h := newHarness(t)
	h.notifications.err = errors.New("channel unreachable")

	assert.Equal(t, []int{7}, h.check(t, certA, 5), "only the nearest threshold is attempted")

	h.notifications.err = nil
	assert.Equal(t, []int{7}, h.check(t, certA, 5), "the next check retries it")
	assert.Empty(t, h.check(t, certA, 5))
}

// The thresholds setting can hold the days in any order.
func TestCheckCertificateExpiry_NearestThresholdWinsInAnySettingOrder(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.svc.SetNotificationDays(context.Background(), h.workspaceID, []int{21, 14, 7}))

	assert.Equal(t, []int{7}, h.check(t, certA, 5))
	assert.Empty(t, h.check(t, certA, 4))
}

func TestUpdateTLSInfo_NewCertificateClearsTheSentHistory(t *testing.T) {
	h := newHarness(t)

	assert.Equal(t, []int{14}, h.check(t, certA, 13))
	assert.Empty(t, h.check(t, certA, 13), "the same certificate keeps its history")
	assert.Equal(t, []int{14}, h.check(t, certB, 13), "a new certificate starts over")
	assert.Empty(t, h.check(t, certB, 12))
}

func TestCheckCertificateExpiry_SendsNothing(t *testing.T) {
	tests := []struct {
		name          string
		notifyDays    []int
		daysRemaining int
		validTo       time.Time
	}{
		{name: "expired certificate", notifyDays: []int{7, 14, 21}, daysRemaining: -1},
		// Days remaining are truncated toward zero, so this one still has 0.
		{name: "expired less than a day ago", notifyDays: []int{7, 14, 21}, daysRemaining: 0, validTo: time.Now().Add(-time.Hour)},
		{name: "outside every threshold", notifyDays: []int{7, 14, 21}, daysRemaining: 22},
		{name: "no thresholds configured", notifyDays: []int{}, daysRemaining: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			require.NoError(t, h.svc.SetNotificationDays(context.Background(), h.workspaceID, tt.notifyDays))

			assert.Empty(t, h.checkValidTo(t, certA, tt.daysRemaining, tt.validTo))
		})
	}
}

func TestCheckCertificateExpiry_NotifiesForACertificateExpiringLaterToday(t *testing.T) {
	h := newHarness(t)

	assert.Equal(t, []int{7}, h.checkValidTo(t, certA, 0, time.Now().Add(time.Hour)))
}

func TestCheckCertificateExpiry_IgnoresMissingCertificateInfo(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	require.NoError(t, h.svc.CheckCertificateExpiry(ctx, h.workspaceID, nil, h.monitorID, "Monitor"))
	require.NoError(t, h.svc.CheckCertificateExpiry(ctx, h.workspaceID, &certificate.TLSInfo{Valid: false}, h.monitorID, "Monitor"))
	assert.Empty(t, h.notifications.targets)
}

// A read on every certificate check must not write: the default is returned and nothing is stored
func TestGetNotificationDays_ReturnsTheDefaultsWithoutStoringThemWhenTheSettingIsMissing(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	days, err := h.svc.GetNotificationDays(ctx, h.workspaceID)
	require.NoError(t, err)
	assert.Equal(t, []int{7, 14, 21}, days)

	stored, err := h.settings.GetByKey(ctx, h.workspaceID, "cert_expiry_notify_days")
	require.NoError(t, err)
	assert.Nil(t, stored)
}

// The monitor is deleted while its check is handled: that is expected and not worth a warning
func TestUpdateTLSInfo_AMonitorDeletedMidFlightIsLoggedAtDebug(t *testing.T) {
	h := newHarness(t)
	tlsInfo := &certificate.TLSInfo{Valid: true, CertInfo: &certificate.CertificateInfo{Subject: "CN=example.com", Fingerprint256: certA, DaysRemaining: 10, CertType: "server"}}

	require.NoError(t, h.svc.UpdateTLSInfo(context.Background(), uuid.NewString(), tlsInfo))

	assert.Empty(t, h.logs.Filter(func(e observer.LoggedEntry) bool { return e.Level >= zap.WarnLevel }).All())
	assert.Len(t, h.logs.FilterMessageSnippet("no longer exists").FilterLevelExact(zap.DebugLevel).All(), 1)
}

func TestCheckCertificateExpiry_AMonitorDeletedMidFlightIsLoggedAtDebug(t *testing.T) {
	h := newHarness(t)
	tlsInfo := &certificate.TLSInfo{Valid: true, CertInfo: &certificate.CertificateInfo{Subject: "CN=example.com", Fingerprint256: certA, DaysRemaining: 10, CertType: "server"}}
	gone := uuid.NewString()
	_, err := h.db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, ?, 'http', 'Gone', 60, 10, 1, 10, 0)`, gone, string(h.workspaceID))
	require.NoError(t, err)
	_, err = h.db.Exec(`DELETE FROM monitors WHERE id = ?`, gone)
	require.NoError(t, err)

	require.NoError(t, h.svc.CheckCertificateExpiry(context.Background(), h.workspaceID, tlsInfo, gone, "Gone"))

	assert.Empty(t, h.logs.Filter(func(e observer.LoggedEntry) bool { return e.Level >= zap.WarnLevel }).All())
	assert.Len(t, h.logs.FilterMessageSnippet("no longer exists").FilterLevelExact(zap.DebugLevel).All(), 1)
}

func TestGetNotificationDays_ReadsTheSetting(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	require.NoError(t, h.svc.SetNotificationDays(ctx, h.workspaceID, []int{3, 30}))

	days, err := h.svc.GetNotificationDays(ctx, h.workspaceID)
	require.NoError(t, err)
	assert.Equal(t, []int{3, 30}, days)
}

// AC4: the checks use the days of the monitor's workspace
func TestCheckCertificateExpiry_UsesTheDaysOfTheMonitorsWorkspace(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	otherWorkspace, otherMonitor := h.addMonitorIn(t)
	require.NoError(t, h.svc.SetNotificationDays(ctx, otherWorkspace, []int{3}))
	checkOther := func(fingerprint string, daysRemaining int) []int {
		tlsInfo := &certificate.TLSInfo{Valid: true, CertInfo: &certificate.CertificateInfo{
			Subject: "CN=example.com", Fingerprint256: fingerprint, DaysRemaining: daysRemaining, CertType: "server",
		}}
		before := len(h.notifications.targets)
		require.NoError(t, h.svc.UpdateTLSInfo(ctx, otherMonitor, tlsInfo))
		require.NoError(t, h.svc.CheckCertificateExpiry(ctx, otherWorkspace, tlsInfo, otherMonitor, "Other monitor"))
		return h.notifications.targets[before:]
	}

	assert.Empty(t, checkOther(certA, 10), "10 days is inside the install's 14, but the workspace asks for 3")
	assert.Equal(t, []int{3}, checkOther(certA, 2), "it is notified at 3 days")
	assert.Equal(t, []int{14}, h.check(t, certA, 10), "and the other workspace keeps its own days")
}
