package workspaceport

import (
	"context"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/workspace"
)

// HeartbeatBatchSize is how many heartbeats the heartbeat file reads, and writes before it
// flushes, at a time. It is a variable so that a test can lower it.
var HeartbeatBatchSize = 1000

// LineSink takes the lines of a heartbeat file. Line writes one value as a line, and Flush sends
// what was written so far to the client.
type LineSink interface {
	Line(value any) error
	Flush() error
}

// HeartbeatExport is the heartbeat history of a workspace's monitors, ready to be written
type HeartbeatExport struct {
	service     *ServiceImpl
	at          time.Time
	workspaceID workspace.ID
	monitors    []heartbeatMonitor
}

type heartbeatMonitor struct{ ref, name string }

func (s *ServiceImpl) PrepareHeartbeatExport(ctx context.Context) (*HeartbeatExport, error) {
	id, err := owner(ctx)
	if err != nil {
		return nil, err
	}
	monitors, err := readAll(func(page, limit int) ([]*monitor.Model, error) {
		return s.deps.Monitors.FindAll(ctx, id, page, limit, "", nil, nil, nil)
	})
	if err != nil {
		return nil, err
	}
	export := &HeartbeatExport{service: s, at: s.now().UTC().Truncate(time.Second), workspaceID: id, monitors: make([]heartbeatMonitor, len(monitors))}
	for i, m := range monitors {
		export.monitors[i] = heartbeatMonitor{ref: m.ID, name: m.Name}
	}
	// Monitor by monitor, in the order of the workspace document
	byNameThenRef(export.monitors, func(m heartbeatMonitor) string { return m.name }, func(m heartbeatMonitor) string { return m.ref })
	return export, nil
}

// At is when the export was prepared, which the header of the file says
func (e *HeartbeatExport) At() time.Time { return e.at }

// Write writes the header and then the heartbeats of each monitor, oldest first, a batch at a time:
// at most HeartbeatBatchSize heartbeats are in memory, whatever the history's size. It flushes
// after the header and after each batch. A failure ends it where it is: the lines already written
// stay, so the caller must not treat the file as complete.
func (e *HeartbeatExport) Write(ctx context.Context, sink LineSink) error {
	probes, err := e.probeNames(ctx)
	if err != nil {
		return err
	}
	header := HeartbeatHeader{Format: HeartbeatsFormatName, Version: HeartbeatsVersion, ExportedAt: e.at}
	if err := sink.Line(header); err != nil {
		return err
	}
	if err := sink.Flush(); err != nil {
		return err
	}
	for _, m := range e.monitors {
		if err := e.writeMonitor(ctx, sink, m.ref, probes); err != nil {
			return err
		}
	}
	return nil
}

// probeNames maps a probe's id to its name
func (e *HeartbeatExport) probeNames(ctx context.Context) (map[string]string, error) {
	probes, err := e.service.deps.Probes.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(probes))
	for _, p := range probes {
		names[p.ID] = p.Name
	}
	return names, nil
}

func (e *HeartbeatExport) writeMonitor(ctx context.Context, sink LineSink, monitorRef string, probes map[string]string) error {
	var after *heartbeat.ExportCursor
	for {
		batch, err := e.service.deps.Heartbeats.FindExportPage(ctx, e.workspaceID, monitorRef, after, HeartbeatBatchSize)
		if err != nil {
			return err
		}
		for _, h := range batch {
			if err := sink.Line(heartbeatLine(h, probes)); err != nil {
				return err
			}
		}
		if err := sink.Flush(); err != nil {
			return err
		}
		if len(batch) < HeartbeatBatchSize {
			return nil
		}
		last := batch[len(batch)-1]
		after = &heartbeat.ExportCursor{Time: last.Time, ID: last.ID}
	}
}

func heartbeatLine(h *heartbeat.Model, probes map[string]string) HeartbeatLine {
	line := HeartbeatLine{
		MonitorRef: h.MonitorID, Probe: probes[h.ProbeID], Status: int(h.Status), Msg: h.Msg, Ping: h.Ping,
		Duration: h.Duration, DownCount: h.DownCount, Retries: h.Retries, Important: h.Important, Time: h.Time.UTC(),
	}
	if !h.EndTime.IsZero() {
		end := h.EndTime.UTC()
		line.EndTime = &end
	}
	return line
}
