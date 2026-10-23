package main

import (
	"fmt"
	"strings"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/web"

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

	// A second login lockout counter per account, whatever the client's address
	BruteforceAccountMaxAttempts int           `env:"BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS" default:"100"`
	BruteforceAccountWindow      time.Duration `env:"BRUTEFORCE_ACCOUNT_WINDOW" default:"10m"`
	BruteforceAccountLockout     time.Duration `env:"BRUTEFORCE_ACCOUNT_LOCKOUT" default:"1m"`

	// Proxies whose X-Forwarded-For and X-Real-IP headers count: IP addresses and CIDR
	// ranges, comma-separated. Empty trusts nobody. Validated in Validate().
	TrustedProxies string `env:"TRUSTED_PROXIES"`

	ServiceName string `env:"SERVICE_NAME" validate:"required,min=1" default:"peekaping"`

	// Prometheus metrics
	// Set to true to expose an unauthenticated /metrics endpoint for Prometheus
	// to scrape. Off by default since it exposes monitor names and status.
	MetricsEnabled bool `env:"METRICS_ENABLED" default:"false"`

	// Usage ping (ADR 0003): on until an admin opts out. TELEMETRY=off forces
	// it off, and TELEMETRY_URL moves where it is sent.
	Telemetry    string `env:"TELEMETRY"`
	TelemetryURL string `env:"TELEMETRY_URL" default:"https://telemetry.peekaping.com/v1/ping"`

	// Error reports (ADR 0003): on until an admin opts out. ERROR_REPORTING=off
	// forces them off, and ERROR_REPORTING_DSN replaces the Sentry DSN built into the binary.
	ErrorReporting    string `env:"ERROR_REPORTING"`
	ErrorReportingDSN string `env:"ERROR_REPORTING_DSN"`

	// The web app (ADR 0002, section 7). With WEB_DIR set to the web build, peekaping serves it
	// on SERVER_PORT beside the API. Unset, it serves the API alone.
	WebDir string `env:"WEB_DIR"` // validated in Validate()
	// Where the web app reaches the API, written to the /env.js peekaping generates. Empty means
	// the origin the web app is served from.
	APIURL string `env:"API_URL"`
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
	// MongoDB storage is deprecated and frozen (ADR 0001), and single mode doesn't support it. The
	// db_type validator ignores case, so this does too.
	if strings.EqualFold(cfg.DBType, "mongo") || strings.EqualFold(cfg.DBType, "mongodb") {
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
	if cfg.BruteforceAccountMaxAttempts <= 0 {
		return fmt.Errorf("BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS must be greater than 0")
	}
	if cfg.BruteforceAccountWindow <= 0 {
		return fmt.Errorf("BRUTEFORCE_ACCOUNT_WINDOW must be a positive duration")
	}
	if cfg.BruteforceAccountLockout <= 0 {
		return fmt.Errorf("BRUTEFORCE_ACCOUNT_LOCKOUT must be a positive duration")
	}

	// A mistyped TRUSTED_PROXIES stops startup: ignoring it would hide a setup that doesn't
	// protect the login lockout the way its operator thinks it does.
	if _, err := httpx.ParseTrustedProxies(cfg.TrustedProxies); err != nil {
		return err
	}

	// A WEB_DIR without a web build fails at startup, not on every page load
	if cfg.WebDir != "" {
		if err := web.CheckDir(cfg.WebDir); err != nil {
			return fmt.Errorf("WEB_DIR=%s can't be served: %w. Set it to the web app's build, or unset it to serve the API alone", cfg.WebDir, err)
		}
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
		Port:                         c.Port,
		ClientURL:                    c.ClientURL,
		DBHost:                       c.DBHost,
		DBPort:                       c.DBPort,
		DBName:                       c.DBName,
		DBUser:                       c.DBUser,
		DBPass:                       c.DBPass,
		DBType:                       c.DBType,
		Mode:                         c.Mode,
		LogLevel:                     c.LogLevel,
		Timezone:                     c.Timezone,
		CheckConcurrency:             c.CheckConcurrency,
		BruteforceMaxAttempts:        c.BruteforceMaxAttempts,
		BruteforceWindow:             c.BruteforceWindow,
		BruteforceLockout:            c.BruteforceLockout,
		BruteforceAccountMaxAttempts: c.BruteforceAccountMaxAttempts,
		BruteforceAccountWindow:      c.BruteforceAccountWindow,
		BruteforceAccountLockout:     c.BruteforceAccountLockout,
		TrustedProxies:               c.TrustedProxies,
		ServiceName:                  c.ServiceName,
		MetricsEnabled:               c.MetricsEnabled,
		Telemetry:                    c.Telemetry,
		TelemetryURL:                 c.TelemetryURL,
		ErrorReporting:               c.ErrorReporting,
		ErrorReportingDSN:            c.ErrorReportingDSN,
	}
}
