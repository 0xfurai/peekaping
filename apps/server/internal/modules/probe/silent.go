package probe

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// A cluster on SQLite or PostgreSQL has no process of its own that runs checks: peekaping-probe
// instances started with the local probe's token do. One that was upgraded without deploying them has
// a local probe that was never online, so the sweep, which reports a probe that goes quiet, has
// nothing to report, and the monitors are not checked and nothing says so. SilentWatch is what makes
// the sweep say so.

// silentAnnouncedKeyPrefix is the start of the Redis key that records that a probe's offline period
// was reported: the key is this and the probe's id
const silentAnnouncedKeyPrefix = "peekaping:probe:offline-announced:"

// Announcements remembers, for all the api replicas, which probes were reported offline for the
// offline period they are in. The report of a probe that never had an instance is made by whichever
// replica holds the leader lock, and the lock changes hands; the record is what keeps each new leader
// from reporting the same probe again.
type Announcements interface {
	// Announced reports whether the probe's offline period was reported
	Announced(ctx context.Context, probeID string) (bool, error)
	// Announce records that the probe's offline period is reported, and returns true when it was the
	// first to: of two replicas that announce at once, one hears true
	Announce(ctx context.Context, probeID string) (first bool, err error)
	// Forget clears the record, once the probe is online: its next offline period is a new one
	Forget(ctx context.Context, probeID string) error
}

type redisAnnouncements struct {
	client *redis.Client
}

// NewRedisAnnouncements keeps the record in the Redis the api replicas share
func NewRedisAnnouncements(client *redis.Client) Announcements {
	return &redisAnnouncements{client: client}
}

func (a *redisAnnouncements) key(probeID string) string { return silentAnnouncedKeyPrefix + probeID }

func (a *redisAnnouncements) Announced(ctx context.Context, probeID string) (bool, error) {
	n, err := a.client.Exists(ctx, a.key(probeID)).Result()
	return n > 0, err
}

func (a *redisAnnouncements) Announce(ctx context.Context, probeID string) (bool, error) {
	return a.client.SetNX(ctx, a.key(probeID), 1, 0).Result()
}

func (a *redisAnnouncements) Forget(ctx context.Context, probeID string) error {
	return a.client.Del(ctx, a.key(probeID)).Err()
}

// SilentWatch has the sweep report the local probe offline when the monitors it has to run have no
// instance to run them. It is for cluster mode only: in single mode the process itself is the local
// probe.
type SilentWatch struct {
	// Monitors counts the active monitors of the local probe that need an instance to be checked:
	// those that run checks, which a push monitor does not
	Monitors func(ctx context.Context) (int, error)
	// Announced records which probes were reported
	Announced Announcements
}

// SilentWarning is what is said of a local probe that has monitors to run and no instance, in the
// log of the api at startup and of the leader that reports it
func SilentWarning(monitors int) string {
	return fmt.Sprintf("the local probe has %d active monitors and no instance: deploy peekaping-probe with PROBE_TOKEN set to PROBE_BOOTSTRAP_TOKEN", monitors)
}

// silent reports whether the local probe has monitors to run and no instance, and how many monitors
func (w *SilentWatch) silent(ctx context.Context, repo Repository) (monitors int, silent bool, err error) {
	instances, err := repo.FindInstances(ctx, LocalID)
	if err != nil {
		return 0, false, err
	}
	if len(instances) > 0 {
		return 0, false, nil
	}
	monitors, err = w.Monitors(ctx)
	return monitors, monitors > 0, err
}

// WarnIfSilent logs SilentWarning when the local probe has monitors to run and no instance, and
// reports whether it did. The api calls it at startup, so that an upgrade that left out the probes
// is in the log of the replica the operator just started, whether or not it is the leader.
func (w *SilentWatch) WarnIfSilent(ctx context.Context, repo Repository, logger *zap.SugaredLogger) (bool, error) {
	monitors, silent, err := w.silent(ctx, repo)
	if err != nil || !silent {
		return false, err
	}
	logger.Warn(SilentWarning(monitors))
	return true, nil
}

// checkSilent forgets a report once the local probe is online, and reports the local probe offline,
// once, when it has monitors to run and no instance has said hello within OfflineAfter of this
// replica taking the leader lock. It runs before checkStatus, which records the report of a probe
// that goes quiet after it was online.
func (s *Sweep) checkSilent(ctx context.Context, p *Model, instances []*Instance, now time.Time) {
	if s.silent == nil || p.ID != LocalID {
		return
	}
	announced := s.silent.Announced
	switch {
	case p.Status == StatusOnline:
		if err := announced.Forget(ctx, p.ID); err != nil {
			s.logger.Warnw("Failed to forget that the local probe was reported offline", "error", err)
		}
	case p.Status == StatusOffline && len(instances) == 0 && !s.termStart.IsZero() && now.Sub(s.termStart) >= s.offlineAfter:
		done, err := announced.Announced(ctx, p.ID)
		if err != nil || done {
			if err != nil {
				s.logger.Warnw("Failed to find out whether the local probe was reported offline", "error", err)
			}
			return
		}
		monitors, silent, err := s.silent.silent(ctx, s.repo)
		if err != nil || !silent {
			if err != nil {
				s.logger.Warnw("Failed to count the monitors of the local probe", "error", err)
			}
			return
		}
		first, err := announced.Announce(ctx, p.ID)
		if err != nil || !first {
			if err != nil {
				s.logger.Warnw("Failed to record that the local probe was reported offline", "error", err)
			}
			return
		}
		s.logger.Warn(SilentWarning(monitors))
		publish(ctx, s, ProbeOffline, Event{ProbeID: p.ID, ProbeName: p.Name, Since: now.UTC()})
	}
}

// markAnnounced records that the probe was reported offline by the sweep, so that having no instance
// later, once they are forgotten, is not reported again. It is for the local probe watched for it.
func (s *Sweep) markAnnounced(ctx context.Context, p *Model) {
	if s.silent == nil || p.ID != LocalID {
		return
	}
	if _, err := s.silent.Announced.Announce(ctx, p.ID); err != nil {
		s.logger.Warnw("Failed to record that the local probe was reported offline", "error", err)
	}
}
