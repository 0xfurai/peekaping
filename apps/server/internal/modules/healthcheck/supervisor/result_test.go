package supervisor

import (
	"testing"
	"time"

	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/ingester/ingest"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTickResult_IngestResultCarriesTheCheckAndTheMonitorSettings(t *testing.T) {
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	tlsInfo := &shared.TLSInfo{Valid: true}
	tick := &TickResult{
		Monitor: &shared.Monitor{
			ID:             "mon-1",
			WorkspaceID:    "workspace-1",
			Name:           "Test",
			Type:           "http",
			Interval:       60,
			Timeout:        48,
			MaxRetries:     3,
			RetryInterval:  30,
			ResendInterval: 5,
			Config:         `{"url":"https://example.com"}`,
		},
		ExecutionResult: &executor.Result{
			Status:    shared.MonitorStatusDown,
			Message:   "503 - Service Unavailable",
			StartTime: start,
			EndTime:   start.Add(12 * time.Millisecond),
			TLSInfo:   tlsInfo,
		},
		PingMs:             12,
		IsUnderMaintenance: true,
	}

	assert.Equal(t, &ingest.Result{
		ProbeID:            "local",
		MonitorID:          "mon-1",
		WorkspaceID:        "workspace-1",
		MonitorName:        "Test",
		MonitorType:        "http",
		MonitorInterval:    60,
		MonitorTimeout:     48,
		MonitorMaxRetries:  3,
		MonitorRetryInt:    30,
		MonitorResendInt:   5,
		MonitorConfig:      `{"url":"https://example.com"}`,
		Status:             shared.MonitorStatusDown,
		Message:            "503 - Service Unavailable",
		PingMs:             12,
		StartTime:          start,
		EndTime:            start.Add(12 * time.Millisecond),
		IsUnderMaintenance: true,
		TLSInfo:            tlsInfo,
		CheckCertExpiry:    true,
	}, tick.IngestResult(true))
}

func TestTickResult_IngestResultTakesTheCertificateExpiryFlagFromTheCaller(t *testing.T) {
	tick := &TickResult{
		Monitor:         &shared.Monitor{ID: "mon-1", Config: `{"check_cert_expiry":true}`},
		ExecutionResult: &executor.Result{Status: shared.MonitorStatusUp},
	}

	assert.False(t, tick.IngestResult(false).CheckCertExpiry)
	assert.True(t, tick.IngestResult(true).CheckCertExpiry)
}

func TestTickResult_IngestResultGivesTheTimesInUTC(t *testing.T) {
	kyiv := time.FixedZone("EEST", 3*60*60)
	start := time.Date(2026, 10, 17, 15, 0, 0, 0, kyiv)
	tick := &TickResult{
		Monitor: &shared.Monitor{ID: "mon-1"},
		ExecutionResult: &executor.Result{
			Status:    shared.MonitorStatusUp,
			StartTime: start,
			EndTime:   start.Add(12 * time.Millisecond),
		},
	}

	got := tick.IngestResult(false)

	assert.Equal(t, time.UTC, got.StartTime.Location())
	assert.Equal(t, time.UTC, got.EndTime.Location())
	assert.Equal(t, time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC), got.StartTime)
	assert.Equal(t, time.Date(2026, 10, 17, 12, 0, 0, 12_000_000, time.UTC), got.EndTime)
}

// The times of a check carry the monotonic clock, so their difference is the time that passed even
// when the wall clock stepped. Converting them to UTC drops that reading, so the end is made from
// the start and the difference: it can't come out before the start.
func TestTickResult_IngestResultEndsWhereTheMeasuredDurationEnds(t *testing.T) {
	start := time.Now()
	end := start.Add(250 * time.Millisecond)
	require.Contains(t, end.String(), "m=", "the end time needs a monotonic reading for this test")
	tick := &TickResult{
		Monitor:         &shared.Monitor{ID: "mon-1"},
		ExecutionResult: &executor.Result{Status: shared.MonitorStatusUp, StartTime: start, EndTime: end},
	}

	got := tick.IngestResult(false)

	assert.Equal(t, 250*time.Millisecond, got.EndTime.Sub(got.StartTime))
	assert.True(t, got.StartTime.Equal(start))
}
