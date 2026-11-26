package certificate_test

import (
	"context"
	"errors"
	"testing"

	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/notification_sent_history"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// scriptedHistory is a sent-history whose answers the test sets, and that keeps what it was asked
type scriptedHistory struct {
	notification_sent_history.Service
	sent      bool
	checkErr  error
	recordErr error
	checked   []int
	recorded  []int
}

func (h *scriptedHistory) CheckIfNotificationSent(_ context.Context, _ string, _ string, targetDays int) (bool, error) {
	h.checked = append(h.checked, targetDays)
	return h.sent, h.checkErr
}

func (h *scriptedHistory) RecordNotificationSent(_ context.Context, _ string, _ string, targetDays int) error {
	h.recorded = append(h.recorded, targetDays)
	return h.recordErr
}

// checkWith checks a certificate with 5 days left, against the workspace's days 7, 14 and 21 and
// the scripted history
func checkWith(t *testing.T, h *harness, history *scriptedHistory) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zap.DebugLevel)
	svc := certificate.NewService(h.settings, h.notifications, history, nil, zap.New(core).Sugar())
	tlsInfo := &certificate.TLSInfo{Valid: true, CertInfo: &certificate.CertificateInfo{
		Subject: "CN=example.com", Fingerprint256: certA, DaysRemaining: 5, CertType: "server",
	}}
	require.NoError(t, svc.CheckCertificateExpiry(context.Background(), h.workspaceID, tlsInfo, h.monitorID, "Monitor"))
	return logs
}

func TestCheckCertificateExpiry_SkipsAThresholdWhoseHistoryCannotBeRead(t *testing.T) {
	h := newHarness(t)
	history := &scriptedHistory{checkErr: errors.New("db down")}

	logs := checkWith(t, h, history)

	assert.Equal(t, []int{7}, history.checked, "only the nearest threshold is looked up")
	assert.Empty(t, h.notifications.targets, "nothing is sent")
	assert.Empty(t, history.recorded)
	entries := logs.FilterMessageSnippet("Failed to check notification history").All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "db down")
}

func TestCheckCertificateExpiry_SendsNothingForAThresholdAlreadySent(t *testing.T) {
	h := newHarness(t)
	history := &scriptedHistory{sent: true}

	logs := checkWith(t, h, history)

	assert.Equal(t, []int{7}, history.checked)
	assert.Empty(t, h.notifications.targets)
	assert.Empty(t, history.recorded)
	entries := logs.FilterMessageSnippet("Certificate notification already sent").All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.DebugLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "threshold 7 days")
}

func TestCheckCertificateExpiry_RecordsNothingForASendThatFailed(t *testing.T) {
	h := newHarness(t)
	h.notifications.err = errors.New("channel unreachable")
	history := &scriptedHistory{}

	logs := checkWith(t, h, history)

	assert.Equal(t, []int{7}, h.notifications.targets)
	assert.Empty(t, history.recorded)
	entries := logs.FilterMessageSnippet("Failed to send certificate expiry notification").All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	assert.Contains(t, entries[0].Message, "channel unreachable")
}

func TestCheckCertificateExpiry_LogsASentNotificationThatCannotBeRecorded(t *testing.T) {
	h := newHarness(t)
	history := &scriptedHistory{recordErr: errors.New("db down")}

	logs := checkWith(t, h, history)

	assert.Equal(t, []int{7}, h.notifications.targets, "the notification was sent")
	assert.Equal(t, []int{7}, history.recorded)
	assert.Equal(t, 1, logs.FilterMessageSnippet("Failed to record notification sent: db down").Len())
	assert.Equal(t, 1, logs.FilterMessageSnippet("Sending certificate expiry notification: example.com expires in 5 days (threshold: 7)").Len())
}
