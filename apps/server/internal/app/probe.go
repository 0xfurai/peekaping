package app

import (
	"context"
	"errors"

	"peekaping/internal/config"
	"peekaping/internal/modules/probe"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// BootstrapLocalProbe makes PROBE_BOOTSTRAP_TOKEN the token of the local probe, so that a compose
// or Helm stack comes up with a probe token and no UI step. The api runs it at startup, after the
// settings initialise. With no token set it leaves the local probe as it is, and doesn't read the
// database. A MongoDB install has no probes (ADR 0001): it logs a warning and skips. Any other
// failure is returned, since a token that wasn't stored would leave probes that expect it unable
// to connect.
func BootstrapLocalProbe(ctx context.Context, container *dig.Container) error {
	return container.Invoke(func(cfg *config.Config, service probe.Service, logger *zap.SugaredLogger) error {
		return BootstrapLocalProbeToken(ctx, service, cfg.ProbeBootstrapToken, logger)
	})
}

// BootstrapLocalProbeToken is BootstrapLocalProbe for a process that has the probe service and the
// token at hand: peekaping, which starts its jobs from the services it resolved.
func BootstrapLocalProbeToken(ctx context.Context, service probe.Service, token string, logger *zap.SugaredLogger) error {
	if token == "" {
		return nil
	}
	err := service.Bootstrap(ctx, token)
	if errors.Is(err, probe.ErrUnsupportedStorage) {
		logger.Warnw("PROBE_BOOTSTRAP_TOKEN is set, but probes need SQLite or PostgreSQL storage: skipping it")
		return nil
	}
	if err != nil {
		return err
	}
	// Neither the token nor its prefix is logged
	logger.Infow("Stored PROBE_BOOTSTRAP_TOKEN as the token of the local probe")
	return nil
}
