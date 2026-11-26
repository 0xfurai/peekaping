package app

import (
	"context"

	"peekaping/internal/config"
	"peekaping/internal/modules/probe"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// BootstrapLocalProbe makes PROBE_BOOTSTRAP_TOKEN the token of the local probe, so that a compose
// or Helm stack comes up with a probe token and no UI step. The api runs it at startup, after the
// settings initialise. With no token set it leaves the local probe as it is, and doesn't read the
// database. A failure is returned, since a token that wasn't stored would leave probes that expect
// it unable to connect.
func BootstrapLocalProbe(ctx context.Context, container *dig.Container) error {
	return container.Invoke(func(cfg *config.Config, service probe.Service, logger *zap.SugaredLogger) error {
		return BootstrapLocalProbeToken(ctx, service, cfg.ProbeBootstrapToken, logger)
	})
}

// BootstrapLocalProbeToken is BootstrapLocalProbe for a process that has the probe service and the
// token at hand: cmd/api, which starts its jobs from the services it resolved. Single mode's
// peekaping doesn't call it: its process checks the local probe's monitors, and its probe
// protocol refuses the local probe's token.
func BootstrapLocalProbeToken(ctx context.Context, service probe.Service, token string, logger *zap.SugaredLogger) error {
	if token == "" {
		return nil
	}
	if err := service.Bootstrap(ctx, token); err != nil {
		return err
	}
	// Neither the token nor its prefix is logged
	logger.Infow("Stored PROBE_BOOTSTRAP_TOKEN as the token of the local probe")
	return nil
}
