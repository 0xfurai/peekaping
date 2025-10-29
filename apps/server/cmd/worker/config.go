package main

import (
	"fmt"

	"peekaping/internal/config"

	"github.com/go-playground/validator/v10"
)

// Config defines the configuration schema for the Worker service
type Config struct {
	// Common settings
	Mode     string `env:"MODE" validate:"required,oneof=dev prod test" default:"dev"`
	LogLevel string `env:"LOG_LEVEL" validate:"omitempty,log_level" default:"info"`
	Timezone string `env:"TZ" validate:"required" default:"UTC"`

	// Redis configuration (required for queue)
	RedisHost     string `env:"REDIS_HOST" validate:"required" default:"redis"`
	RedisPort     string `env:"REDIS_PORT" validate:"required,port" default:"6379"`
	RedisPassword string `env:"REDIS_PASSWORD" default:""`
	RedisDB       int    `env:"REDIS_DB" validate:"min=0,max=15" default:"0"`

	// Queue configuration
	QueueConcurrency int `env:"QUEUE_CONCURRENCY" validate:"min=1" default:"128"`

	ServiceName string `env:"SERVICE_NAME" validate:"required,min=1" default:"peekaping:worker"`
}

// LoadAndValidate loads and validates the Worker service configuration
func LoadAndValidate(path string) (*Config, error) {
	cfg, err := config.LoadConfig[Config](path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// Validate validates the Worker service configuration
func Validate(cfg *Config) error {
	// Validate using struct tags
	v := validator.New()
	config.RegisterCustomValidators(v)

	if err := v.Struct(cfg); err != nil {
		return err
	}

	// Additional queue validation
	if cfg.QueueConcurrency < 1 {
		return fmt.Errorf("QUEUE_CONCURRENCY must be at least 1")
	}

	return nil
}

// ToInternalConfig converts Worker config to internal config format
// This is needed for backward compatibility with existing code
func (c *Config) ToInternalConfig() *config.Config {
	return &config.Config{
		Mode:             c.Mode,
		LogLevel:         c.LogLevel,
		Timezone:         c.Timezone,
		RedisHost:        c.RedisHost,
		RedisPort:        c.RedisPort,
		RedisPassword:    c.RedisPassword,
		RedisDB:          c.RedisDB,
		QueueConcurrency: c.QueueConcurrency,
		ServiceName:      c.ServiceName,
	}
}
