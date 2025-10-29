package main

import (
	"fmt"
	"time"

	"peekaping/internal/config"

	"github.com/go-playground/validator/v10"
)

// Config defines the configuration schema for the API service
type Config struct {
	// Server configuration
	Port      string `env:"SERVER_PORT" validate:"required,port" default:"8034"`
	ClientURL string `env:"CLIENT_URL" validate:"url" default:"http://localhost:3000"`

	// Database configuration
	DBHost string `env:"DB_HOST"`                           // validated in Validate()
	DBPort string `env:"DB_PORT"`                           // validated in Validate()
	DBName string `env:"DB_NAME" validate:"required,min=1"` // validated in Validate()
	DBUser string `env:"DB_USER"`                           // validated in Validate()
	DBPass string `env:"DB_PASS"`                           // validated in Validate()
	DBType string `env:"DB_TYPE" validate:"required,db_type"`

	// Common settings
	Mode     string `env:"MODE" validate:"required,oneof=dev prod test" default:"dev"`
	LogLevel string `env:"LOG_LEVEL" validate:"omitempty,log_level" default:"info"`
	Timezone string `env:"TZ" validate:"required" default:"UTC"`

	// Redis configuration
	RedisHost     string `env:"REDIS_HOST" validate:"required" default:"redis"`
	RedisPort     string `env:"REDIS_PORT" validate:"required,port" default:"6379"`
	RedisPassword string `env:"REDIS_PASSWORD" default:""`
	RedisDB       int    `env:"REDIS_DB" validate:"min=0,max=15" default:"0"`

	// Queue configuration
	QueueConcurrency int `env:"QUEUE_CONCURRENCY" validate:"min=1" default:"128"`

	// Producer configuration (for push endpoint)
	ProducerConcurrency int `env:"PRODUCER_CONCURRENCY" validate:"min=1,max=128" default:"10"`

	// Bruteforce protection settings
	BruteforceMaxAttempts int           `env:"BRUTEFORCE_MAX_ATTEMPTS" default:"20"`
	BruteforceWindow      time.Duration `env:"BRUTEFORCE_WINDOW" default:"1m"`
	BruteforceLockout     time.Duration `env:"BRUTEFORCE_LOCKOUT" default:"1m"`

	ServiceName string `env:"SERVICE_NAME" validate:"required,min=1" default:"peekaping:api"`
}

// LoadAndValidate loads and validates the API service configuration
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

// Validate validates the API service configuration
func Validate(cfg *Config) error {
	// Validate using struct tags
	v := validator.New()
	config.RegisterCustomValidators(v)

	if err := v.Struct(cfg); err != nil {
		return err
	}

	// Validate database-specific requirements
	dbConfig := &config.DBConfig{
		DBHost: cfg.DBHost,
		DBPort: cfg.DBPort,
		DBName: cfg.DBName,
		DBUser: cfg.DBUser,
		DBPass: cfg.DBPass,
		DBType: cfg.DBType,
	}

	if err := config.ValidateDatabaseCustomRules(dbConfig); err != nil {
		return fmt.Errorf("database validation failed: %w", err)
	}

	// Validate bruteforce settings
	if cfg.BruteforceMaxAttempts <= 0 {
		return fmt.Errorf("BRUTEFORCE_MAX_ATTEMPTS must be greater than 0")
	}
	if cfg.BruteforceWindow <= 0 {
		return fmt.Errorf("BRUTEFORCE_WINDOW must be a positive duration")
	}
	if cfg.BruteforceLockout <= 0 {
		return fmt.Errorf("BRUTEFORCE_LOCKOUT must be a positive duration")
	}

	return nil
}

// ToInternalConfig converts API config to internal config format
// This is needed for backward compatibility with existing code
func (c *Config) ToInternalConfig() *config.Config {
	return &config.Config{
		Port:                  c.Port,
		ClientURL:             c.ClientURL,
		DBHost:                c.DBHost,
		DBPort:                c.DBPort,
		DBName:                c.DBName,
		DBUser:                c.DBUser,
		DBPass:                c.DBPass,
		DBType:                c.DBType,
		Mode:                  c.Mode,
		LogLevel:              c.LogLevel,
		Timezone:              c.Timezone,
		RedisHost:             c.RedisHost,
		RedisPort:             c.RedisPort,
		RedisPassword:         c.RedisPassword,
		RedisDB:               c.RedisDB,
		QueueConcurrency:      c.QueueConcurrency,
		ProducerConcurrency:   c.ProducerConcurrency,
		BruteforceMaxAttempts: c.BruteforceMaxAttempts,
		BruteforceWindow:      c.BruteforceWindow,
		BruteforceLockout:     c.BruteforceLockout,
		ServiceName:           c.ServiceName,
	}
}
