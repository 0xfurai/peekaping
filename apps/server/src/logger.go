package main

import (
	"peekaping/src/config"

	"go.uber.org/zap"
)

func ProvideLogger(cfg *config.Config) (*zap.SugaredLogger, error) {
	zapConfig := zap.NewProductionConfig()
	var logger *zap.Logger
	var err error

	if cfg.Mode == "prod" {
		logger, err = zapConfig.Build()
	} else {
		logger, err = zap.NewDevelopment()
	}
	if err != nil {
		return nil, err
	}

	return logger.Sugar(), nil
}
