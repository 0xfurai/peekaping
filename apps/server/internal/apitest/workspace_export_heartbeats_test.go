package apitest

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/workspaceport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-170: an owner downloads the heartbeat history of a workspace as a file of its own, which the
// server streams

const exportHeartbeatsPath = exportPath + "/heartbeats"

// seedHeartbeats stores n heartbeats of the monitor, with the msg "<label> a", "<label> b" and so
// on. A minute apart from start, in groups of perTime that share a time, so that a page boundary
// can fall between two heartbeats of the same time.
func (a *app) seedHeartbeats(t *testing.T, monitorID, label string, start time.Time, n, perTime int) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(heartbeats heartbeat.Repository) error {
		for i := 0; i < n; i++ {
			at := start.Add(time.Duration(i/perTime) * time.Minute)
			_, err := heartbeats.Create(context.Background(), &heartbeat.Model{
				MonitorID: monitorID, Status: heartbeat.MonitorStatus(i % 2), Msg: label + " " + string(rune('a'+i)),
				Ping: 10 + i, Duration: 1000 + i, DownCount: i, Retries: 1, Important: i == 0, Time: at, EndTime: at.Add(time.Second),
			})
			if err != nil {
				return err
			}
		}
		return nil
	}))
}

// lines splits an NDJSON body into its lines, each of which must be one JSON value
func ndjsonLines(t *testing.T, body string) []string {
	t.Helper()
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		require.True(t, json.Valid(scanner.Bytes()), scanner.Text())
		lines = append(lines, scanner.Text())
	}
	require.NoError(t, scanner.Err())
	return lines
}

// heartbeatFile reads a heartbeat file: its header and its lines
func heartbeatFile(t *testing.T, body string) (workspaceport.HeartbeatHeader, []workspaceport.HeartbeatLine) {
	t.Helper()
	lines := ndjsonLines(t, body)
	require.NotEmpty(t, lines, "a file has a header")
	var header workspaceport.HeartbeatHeader
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &header))
	var heartbeats []workspaceport.HeartbeatLine
	for _, line := range lines[1:] {
		var parsed workspaceport.HeartbeatLine
		require.NoError(t, json.Unmarshal([]byte(line), &parsed))
		heartbeats = append(heartbeats, parsed)
	}
	return header, heartbeats
}

func msgs(lines []workspaceport.HeartbeatLine) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.Msg
	}
	return out
}

func TestWorkspaceExportHeartbeats_AHeaderThenEachMonitorsHeartbeatsOldestFirst(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.seedExportable(t)
		other := a.seedWorkspace(t, "Other", "other")
		start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		// Stored out of order, and the monitor with the later name first
		a.seedHeartbeats(t, w.httpMonitor, "site", start, 3, 1)
		a.seedHeartbeats(t, w.pushMonitor, "cron", start.Add(time.Hour), 2, 1)
		a.seedHeartbeats(t, other.monitors[0], "other", start, 4, 1)

		res := a.request(t, http.MethodGet, exportHeartbeatsPath, nil, w.owner)
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		assert.Equal(t, "application/x-ndjson", res.Header().Get("Content-Type"))
		assert.Regexp(t, `^attachment; filename="peekaping-heartbeats-\d{4}-\d{2}-\d{2}\.ndjson"$`, res.Header().Get("Content-Disposition"))
		assert.Equal(t, "no-store", res.Header().Get("Cache-Control"))

		header, lines := heartbeatFile(t, res.Body.String())
		assert.Equal(t, "peekaping-heartbeats", header.Format)
		assert.Equal(t, 1, header.Version)
		assert.False(t, header.ExportedAt.IsZero())
		assert.Equal(t, []string{"cron a", "cron b", "site a", "site b", "site c"}, msgs(lines), "by monitor name, then by time")
		assert.NotContains(t, res.Body.String(), "other")

		first := lines[0]
		assert.Equal(t, w.pushMonitor, first.MonitorRef)
		assert.NotEmpty(t, first.Probe, "the name of the probe that ran the check, the local one")
		assert.Equal(t, 0, first.Status)
		assert.Equal(t, 10, first.Ping)
		assert.Equal(t, 1000, first.Duration)
		assert.Equal(t, 0, first.DownCount)
		assert.Equal(t, 1, first.Retries)
		assert.True(t, first.Important)
		assert.True(t, start.Add(time.Hour).Equal(first.Time), first.Time)
		require.NotNil(t, first.EndTime)
		assert.True(t, start.Add(time.Hour+time.Second).Equal(*first.EndTime), first.EndTime)
		assert.Equal(t, w.httpMonitor, lines[2].MonitorRef)
		assert.Equal(t, 1, lines[3].Status)
	})
}

// flushCounter counts how often the handler flushes the response, which is how a client sees the
// file arrive in parts
type flushCounter struct {
	*httptest.ResponseRecorder
	flushes int
}

func (f *flushCounter) Flush() {
	f.flushes++
	f.ResponseRecorder.Flush()
}

func TestWorkspaceExportHeartbeats_ItReadsAndFlushesABatchAtATimeAndLosesNoHeartbeat(t *testing.T) {
	serial(t) // lowers the batch size of the process
	previous := workspaceport.HeartbeatBatchSize
	workspaceport.HeartbeatBatchSize = 2
	t.Cleanup(func() { workspaceport.HeartbeatBatchSize = previous })
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		w := a.seedExportable(t)
		start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		a.seedHeartbeats(t, w.pushMonitor, "cron", start, 5, 3)
		a.seedHeartbeats(t, w.httpMonitor, "site", start, 4, 3)

		req := httptest.NewRequest(http.MethodGet, exportHeartbeatsPath, nil)
		w.owner(req)
		recorder := &flushCounter{ResponseRecorder: httptest.NewRecorder()}
		a.router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

		_, lines := heartbeatFile(t, recorder.Body.String())
		// Three heartbeats share a time, so a page of 2 ends between two of the same time: none is
		// skipped or repeated
		assert.ElementsMatch(t, []string{"cron a", "cron b", "cron c", "cron d", "cron e", "site a", "site b", "site c", "site d"}, msgs(lines))
		assert.Len(t, lines, 9)
		assert.Equal(t, w.pushMonitor, lines[4].MonitorRef)
		assert.Equal(t, w.httpMonitor, lines[5].MonitorRef)
		times := make([]time.Time, 0, 5)
		for _, line := range lines[:5] {
			times = append(times, line.Time)
		}
		assert.IsNonDecreasing(t, unixTimes(times))
		// The header, then a flush after every page: 5 heartbeats are pages of 2, 2 and 1; 4 are 2, 2 and none
		assert.Equal(t, 1+3+3, recorder.flushes)
	})
}

func unixTimes(times []time.Time) []int64 {
	out := make([]int64, len(times))
	for i, at := range times {
		out[i] = at.UnixNano()
	}
	return out
}

func TestWorkspaceExportHeartbeats_AWorkspaceWithNoHeartbeatsGetsTheHeaderAlone(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedExportable(t)

	header, lines := heartbeatFile(t, a.request(t, http.MethodGet, exportHeartbeatsPath, nil, w.owner).Body.String())
	assert.Equal(t, "peekaping-heartbeats", header.Format)
	assert.Empty(t, lines)
}

func TestWorkspaceExportHeartbeats_OnlyAnOwnerDownloadsThem(t *testing.T) {
	a := newAppWithAdmin(t)
	w := a.seedExportable(t)
	a.seedHeartbeats(t, w.httpMonitor, "site", time.Now().Add(-time.Hour), 2, 1)
	key := a.createKeyToken(t, w.owner)

	for name, creds := range map[string]credentials{"an admin": w.admin, "a member": w.member} {
		res := a.request(t, http.MethodGet, exportHeartbeatsPath, nil, creds)
		assert.Equal(t, http.StatusForbidden, res.Code, name)
		assert.NotContains(t, res.Body.String(), "site", name)
		assert.NotContains(t, res.Header().Get("Content-Type"), "ndjson", name)
	}
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, exportHeartbeatsPath, nil, withHeader("X-API-Key", key)).Code, "an API key")
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, exportHeartbeatsPath, nil).Code, "no sign-in")
}
