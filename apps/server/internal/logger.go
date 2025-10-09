package internal

import (
	"fmt"
	"peekaping/internal/config"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func ProvideLogger(cfg *config.Config) (*zap.SugaredLogger, error) {
	// Parse the log level from config
	logLevel, err := parseLogLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}

	// Choose base configuration based on mode
	var zapConfig zap.Config
	if cfg.Mode == "prod" {
		zapConfig = zap.NewProductionConfig()
	} else {
		zapConfig = zap.NewDevelopmentConfig()
		zapConfig.EncoderConfig.TimeKey = ""  // ⟵ no time
		zapConfig.EncoderConfig.LevelKey = "" // ⟵ no level
	}

	// Override the log level with the one from LOG_LEVEL environment variable
	zapConfig.Level = zap.NewAtomicLevelAt(logLevel)

	var logger *zap.Logger
	logger, err = zapConfig.Build()
	if err != nil {
		return nil, err
	}

	return logger.Sugar(), nil
}

// parseLogLevel converts string log level to zapcore.Level
func parseLogLevel(level string) (zapcore.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	case "dpanic":
		return zapcore.DPanicLevel, nil
	case "panic":
		return zapcore.PanicLevel, nil
	case "fatal":
		return zapcore.FatalLevel, nil
	default:
		return zapcore.InfoLevel, fmt.Errorf("invalid log level: %s", level)
	}
}
