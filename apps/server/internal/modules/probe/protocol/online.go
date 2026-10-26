package protocol

import (
	"context"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/probe"

	"go.uber.org/zap"
)

// cameBack sets a probe online once one of its instances has been heard from, if the probe was
// offline when the request began, and announces it. It costs an online probe nothing: the probe
// the bearer middleware loaded says what it was.
//
// MarkOnline changes the status in one conditional update, so when several requests find the probe
// offline at once, on this replica or on another, exactly one changes it and publishes
// probe.online. The channels hear about the probe's return once. A probe that an admin disabled
// after the request authenticated is not changed, and so not announced: the update leaves a
// disabled probe offline.
//
// A probe that can't be marked online doesn't fail the request that found it: the request is
// fine, and the sweep sets the probe online within its interval.
func cameBack(ctx context.Context, probes probe.Service, bus events.EventBus, logger *zap.SugaredLogger, p *probe.Model, at time.Time) {
	if p.Status != probe.StatusOffline {
		return
	}
	changed, err := probes.MarkOnline(ctx, p.ID, at)
	if err != nil {
		logger.Warnw("Failed to set a probe online, the sweep will", "probeId", p.ID, "error", err)
		return
	}
	if !changed {
		return
	}
	logger.Infow("Probe is back online", "probeId", p.ID, "name", p.Name)
	if bus == nil {
		return
	}
	if err := events.Publish(ctx, bus, probe.ProbeOnline, probe.Event{ProbeID: p.ID, ProbeName: p.Name, Since: at.UTC()}); err != nil {
		logger.Warnw("Failed to publish that a probe is back online", "probeId", p.ID, "error", err)
	}
}
