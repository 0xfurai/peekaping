package main

import (
	"fmt"
	"time"

	"peekaping/internal/config"

	"github.com/go-playground/validator/v10"
)

// Config defines the configuration schema of peekaping, single mode's one process (ADR 0002,
// section 5). Its variables keep the names, defaults and meaning they have for the API service,
// and CHECK_CONCURRENCY sizes the scheduler's check pool. It needs no Redis.
type Config struct {
	// Server configuration
	Port      string `env:"SERVER_PORT" validate:"required,port" default:"8034"`
	ClientURL string `env:"CLIENT_URL" validate:"url" default:"http://localhost:3000"`

	// Database configuration. Single mode stores its data in SQLite or PostgreSQL.
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

	// Cluster mode's Redis configuration. Single mode uses no Redis: these are read only so
	// that peekaping can say it ignores them.
	RedisHost     string `env:"REDIS_HOST"`
	RedisPort     string `env:"REDIS_PORT"`
	RedisUsername string `env:"REDIS_USERNAME"`
	RedisPassword string `env:"REDIS_PASSWORD"`
	RedisDB       string `env:"REDIS_DB"`

	// The scheduler runs this many checks at the same time. A check that is due while the
	// pool is full waits for a free slot.
	CheckConcurrency int `env:"CHECK_CONCURRENCY" validate:"min=1" default:"128"`

	// Bruteforce protection settings
	BruteforceMaxAttempts int           `env:"BRUTEFORCE_MAX_ATTEMPTS" default:"20"`
	BruteforceWindow      time.Duration `env:"BRUTEFORCE_WINDOW" default:"1m"`
	BruteforceLockout     time.Duration `env:"BRUTEFORCE_LOCKOUT" default:"1m"`

	ServiceName string `env:"SERVICE_NAME" validate:"required,min=1" default:"peekaping"`

	// Prometheus metrics
	// Set to true to expose an unauthenticated /metrics endpoint for Prometheus
	// to scrape. Off by default since it exposes monitor names and status.
	MetricsEnabled bool `env:"METRICS_ENABLED" default:"false"`
}

// LoadAndValidate loads and validates peekaping's configuration
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

// Validate validates peekaping's configuration
func Validate(cfg *Config) error {
	// MongoDB storage is deprecated and frozen (ADR 0001), and single mode doesn't support it
	if cfg.DBType == "mongo" || cfg.DBType == "mongodb" {
		return fmt.Errorf("DB_TYPE=%s isn't supported by peekaping (single mode), which stores its data in SQLite or PostgreSQL. "+
			"MongoDB storage is deprecated and runs only in cluster mode, with the api, producer, worker and ingester services", cfg.DBType)
	}

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

// RedisSettings returns the names of the REDIS_* variables that are set, which peekaping
// ignores. It never returns their values.
func (c *Config) RedisSettings() []string {
	var set []string
	for _, setting := range []struct{ name, value string }{
		{"REDIS_HOST", c.RedisHost},
		{"REDIS_PORT", c.RedisPort},
		{"REDIS_USERNAME", c.RedisUsername},
		{"REDIS_PASSWORD", c.RedisPassword},
		{"REDIS_DB", c.RedisDB},
	} {
		if setting.value != "" {
			set = append(set, setting.name)
		}
	}
	return set
}

// ToInternalConfig converts peekaping's config to the internal config the modules read. It has
// no Redis settings.
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
		CheckConcurrency:      c.CheckConcurrency,
		BruteforceMaxAttempts: c.BruteforceMaxAttempts,
		BruteforceWindow:      c.BruteforceWindow,
		BruteforceLockout:     c.BruteforceLockout,
		ServiceName:           c.ServiceName,
		MetricsEnabled:        c.MetricsEnabled,
	}
}
