package ingester

import (
	"errors"
	"testing"

	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// loggedIngester is a test ingester whose logger keeps what it is told, at every level
func loggedIngester() (*testIngester, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	ti := newTestIngester()
	ti.service = NewService(ti.heartbeats, ti.certs, ti.locations, ti.bus, zap.New(core).Sugar())
	return ti, logs
}

// messages are the level and message of each entry, in the order they were logged
func messages(logs *observer.ObservedLogs) []string {
	var got []string
	for _, entry := range logs.All() {
		got = append(got, entry.Level.String()+" "+entry.Message)
	}
	return got
}

// An Ingest logs the status of the check, with the monitor's name, ping, interval and type, after
// the decision to notify and before the writes.
func TestIngester_LogsTheCheckStatus(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status shared.MonitorStatus
		want   []string
	}{
		{"up", up, []string{"debug Marking for notification", "debug Monitor up"}},
		{"down", down, []string{"debug Marking for notification", "debug Monitor down"}},
		{"pending", pending, []string{"debug Monitor pending"}},
		{"maintenance", maintenance, []string{"debug Monitor under maintenance"}},
		{"a status that is none of them", shared.MonitorStatus(9), nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ti, logs := loggedIngester()
			payload := checkResult(monitorSettings{}, tt.status)
			payload.MonitorName = "Checkout"
			payload.PingMs = 42
			payload.MonitorInterval = 30
			payload.MonitorType = "tcp"

			require.NoError(t, ti.ingest(t, payload))

			assert.Equal(t, tt.want, messages(logs))
			if len(tt.want) == 0 {
				return
			}
			last := logs.All()[len(tt.want)-1].ContextMap()
			assert.Equal(t, "Checkout", last["monitor_name"])
			if tt.want[len(tt.want)-1] != "debug Marking for notification" {
				assert.EqualValues(t, 42, last["ping_ms"])
				assert.EqualValues(t, 30, last["interval"])
				assert.Equal(t, "tcp", last["type"])
			}
		})
	}
}

func TestIngester_LogsItsFailures(t *testing.T) {
	t.Run("a failed read of the previous heartbeat", func(t *testing.T) {
		ti, logs := loggedIngester()
		ti.heartbeats.findErr = errors.New("db down")

		require.NoError(t, ti.ingest(t, checkResult(monitorSettings{}, up)))

		assert.Equal(t, []string{"error Failed to get previous heartbeat for monitor", "debug Marking for notification", "debug Monitor up"}, messages(logs))
		fields := logs.All()[0].ContextMap()
		assert.Equal(t, monitorID, fields["monitor_id"])
		assert.Equal(t, shared.LocalProbeID, fields["probe_id"])
		assert.Equal(t, "db down", fields["error"])
	})

	t.Run("a failed write", func(t *testing.T) {
		ti, logs := loggedIngester()
		ti.heartbeats.createErr = errors.New("db down")

		require.Error(t, ti.ingest(t, checkResult(monitorSettings{}, up)))

		assert.Equal(t, []string{"debug Marking for notification", "debug Monitor up", "error Failed to create heartbeat"}, messages(logs))
	})

	t.Run("a failed record of the location", func(t *testing.T) {
		ti, logs := loggedIngester()
		ti.locations.err = errors.New("db down")

		require.Error(t, ti.ingest(t, checkResult(monitorSettings{}, up)))

		assert.Equal(t, []string{"error Failed to record the location of monitor"}, messages(logs))
	})

	t.Run("events that can't be published are logged and the ingest goes on", func(t *testing.T) {
		ti, logs := loggedIngester()
		require.NoError(t, ti.bus.Close(t.Context()))

		require.NoError(t, ti.ingest(t, checkResult(monitorSettings{}, down)))

		assert.Equal(t, []string{
			"debug Marking for notification",
			"debug Monitor down",
			"error Failed to publish monitor status changed event",
			"error Failed to publish important heartbeat event",
		}, messages(logs))
		assert.Len(t, ti.heartbeats.stored, 1)
	})

	t.Run("a result for a suspended workspace is dropped quietly", func(t *testing.T) {
		ti, logs := loggedIngester()
		ti.locations.err = shared.ErrWorkspaceSuspended

		require.NoError(t, ti.ingest(t, checkResult(monitorSettings{}, up)))

		assert.Equal(t, []string{"debug Dropping a result for a monitor of a suspended workspace"}, messages(logs))
		assert.Empty(t, ti.heartbeats.stored)
	})
}

func TestIngester_LogsTheCertificateWork(t *testing.T) {
	tlsInfo := &certificate.TLSInfo{Valid: true}

	t.Run("expiry checks off", func(t *testing.T) {
		ti, logs := loggedIngester()
		payload := checkResult(monitorSettings{}, pending)
		payload.TLSInfo = tlsInfo

		require.NoError(t, ti.ingest(t, payload))

		assert.Equal(t, []string{"debug Monitor pending", "debug Certificate expiry checking disabled for monitor"}, messages(logs))
	})

	t.Run("certificate errors", func(t *testing.T) {
		ti, logs := loggedIngester()
		ti.certs.err = errors.New("tls store down")
		payload := checkResult(monitorSettings{}, pending)
		payload.TLSInfo = tlsInfo
		payload.CheckCertExpiry = true

		require.NoError(t, ti.ingest(t, payload))

		assert.Equal(t, []string{
			"debug Monitor pending",
			"error Failed to update TLS info for monitor",
			"error Failed to check certificate expiry for monitor",
		}, messages(logs))
		assert.Equal(t, "tls store down", logs.All()[1].ContextMap()["error"])
	})
}
