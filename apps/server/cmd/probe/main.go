// Command probe runs the checks of a remote probe, over the probe protocol designed in ticket #31.
// It connects out to a Peekaping server with its token, runs the checks of the monitors assigned
// to it, and sends their results back. It has no database and no Redis. Error reports stay off:
// they follow an admin's choice that the probe has no way to read.
package main

import (
	"log"
	"os"
	_ "time/tzdata" // embed the zone database so TZ works in images without tzdata

	"peekaping/internal/probeclient"
	"peekaping/internal/shutdown/lifecycle"
	"peekaping/internal/version"

	"go.uber.org/zap"
)

func main() {
	cfg, err := LoadAndValidate("../..")
	if err != nil {
		log.Fatalf("The probe can't start:\n%v", err)
	}

	logger, err := newLogger(cfg)
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	ctx, stop := lifecycle.NotifySignals(logger, os.Exit)
	defer stop()

	if err := run(ctx, cfg, logger, newRuntime); err != nil {
		logger.Fatalw("The probe stopped with an error", "error", err)
	}
}

// newLogger creates the logger the probe's parts log through: development logs, or JSON ones in
// prod mode, at LOG_LEVEL
func newLogger(cfg *Config) (*zap.SugaredLogger, error) {
	level, err := parseLogLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}
	var zapConfig zap.Config
	if cfg.Mode == "prod" {
		zapConfig = zap.NewProductionConfig()
		zapConfig.InitialFields = map[string]any{"svc": "peekaping:probe"}
	} else {
		zapConfig = zap.NewDevelopmentConfig()
	}
	zapConfig.Level = zap.NewAtomicLevelAt(level)
	logger, err := zapConfig.Build()
	if err != nil {
		return nil, err
	}
	return logger.Sugar(), nil
}

// newRuntime builds the probe's runtime from the configuration
func newRuntime(cfg *Config, logger *zap.SugaredLogger) (runtime, error) {
	return probeclient.New(probeclient.Config{
		URL:         cfg.URL,
		Token:       cfg.Token,
		Version:     version.Version,
		Concurrency: cfg.Concurrency,
		Logger:      logger,
	}), nil
}
