package main

import (
	"fmt"
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
	DBHost    string `env:"DB_HOST"`                           // validated in Validate()
	DBPort    string `env:"DB_PORT"`                           // validated in Validate()
	DBName    string `env:"DB_NAME" validate:"required,min=1"` // validated in Validate()
	DBUser    string `env:"DB_USER"`                           // validated in Validate()
	DBPass    string `env:"DB_PASS"`                           // validated in Validate()
	DBType    string `env:"DB_TYPE" validate:"required,db_type"`
	DBSSLMode string `env:"DB_SSLMODE" validate:"omitempty,oneof=disable allow prefer require verify-ca verify-full" default:"disable"`

	// Which check the process runs when it opens SQLite: quick (PRAGMA quick_check), full
	// (PRAGMA integrity_check, which reads the whole file) or off. A finding stops startup.
	SQLiteStartupCheck string `env:"SQLITE_STARTUP_CHECK" validate:"oneof=quick full off" default:"quick"`

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

	// Origins whose pages may read the API's responses: comma-separated, "*" alone for any.
	// Empty allows the origin of CLIENT_URL. Validated in Validate().
	CORSOrigins string `env:"CORS_ORIGINS"`

	ServiceName string `env:"SERVICE_NAME" validate:"required,min=1" default:"peekaping"`

	// Prometheus metrics
	// Set to true to expose an unauthenticated /metrics endpoint for Prometheus
	// to scrape. Off by default since it exposes monitor names and status.
	MetricsEnabled bool `env:"METRICS_ENABLED" default:"false"`

	// How many days the hourly cleanup keeps notification history. The history stops a
	// certificate expiry notification from being sent twice, so it must outlast the
	// default thresholds (the largest is 21 days): the minimum is 30. 0 counts as unset
	// and gives the default, as for every int setting.
	NotificationHistoryRetentionDays int `env:"NOTIFICATION_HISTORY_RETENTION_DAYS" validate:"min=30" default:"90"`

	// How many days the hourly cleanup keeps the TLS info that monitors record. 0 counts as
	// unset and gives the default.
	TLSInfoRetentionDays int `env:"TLS_INFO_RETENTION_DAYS" validate:"min=1" default:"30"`

	// Usage ping (ADR 0003): on until an admin opts out. TELEMETRY=off forces
	// it off, and TELEMETRY_URL moves where it is sent.
	Telemetry    string `env:"TELEMETRY"`
	TelemetryURL string `env:"TELEMETRY_URL" default:"https://telemetry.peekaping.com/v1/ping"`

	// Error reports (ADR 0003): on until an admin opts out. ERROR_REPORTING=off
	// forces them off, and ERROR_REPORTING_DSN replaces the Sentry DSN built into the binary.
	ErrorReporting    string `env:"ERROR_REPORTING"`
	ErrorReportingDSN string `env:"ERROR_REPORTING_DSN"`

	// The local probe's token in cluster mode. Single mode doesn't use it: its process checks the
	// local probe's monitors, and refuses a probe instance that uses the local probe's token. It
	// is read only so that peekaping can say it ignores it, whatever its length.
	ProbeBootstrapToken string `env:"PROBE_BOOTSTRAP_TOKEN"`

	// How long an online probe's instances may all go unseen before the probe counts as offline.
	// A Go duration, from config.MinProbeOfflineAfter to config.MaxProbeOfflineAfter. Validated in
	// Validate().
	ProbeOfflineAfter time.Duration `env:"PROBE_OFFLINE_AFTER" default:"3m"`

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
	// MongoDB storage was removed, and the db_type validator below would only call the value invalid
	if err := config.RefuseMongoStorage(cfg.DBType); err != nil {
		return err
	}

	// Validate using struct tags
	v := validator.New()
	config.RegisterCustomValidators(v)

	if err := v.Struct(cfg); err != nil {
		return err
	}

	// Validate database-specific requirements
	dbConfig := &config.DBConfig{
		DBHost:    cfg.DBHost,
		DBPort:    cfg.DBPort,
		DBName:    cfg.DBName,
		DBUser:    cfg.DBUser,
		DBPass:    cfg.DBPass,
		DBType:    cfg.DBType,
		DBSSLMode: cfg.DBSSLMode,
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

	// So does a mistyped CORS_ORIGINS: ignoring it would leave the web app unable to read the
	// API's responses, with no sign of why.
	if _, err := httpx.ParseCORSOrigins(cfg.CORSOrigins, cfg.ClientURL); err != nil {
		return err
	}

	// A shorter limit would announce a probe offline while it restarts gracefully (see
	// probe.RetiredAt)
	if cfg.ProbeOfflineAfter < config.MinProbeOfflineAfter {
		return fmt.Errorf("PROBE_OFFLINE_AFTER must be at least %s", config.MinProbeOfflineAfter)
	}
	// The sweep forgets an instance after that long, so a longer limit would set a probe online on
	// an instance about to be pruned, and offline again (see config.MaxProbeOfflineAfter)
	if cfg.ProbeOfflineAfter > config.MaxProbeOfflineAfter {
		return fmt.Errorf("PROBE_OFFLINE_AFTER must be at most %s", config.MaxProbeOfflineAfter)
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
// no Redis settings and no probe bootstrap token, which single mode doesn't use.
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
		DBSSLMode:                    c.DBSSLMode,
		SQLiteStartupCheck:           c.SQLiteStartupCheck,
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
		CORSOrigins:                  c.CORSOrigins,
		ServiceName:                  c.ServiceName,
		MetricsEnabled:               c.MetricsEnabled,
		Telemetry:                    c.Telemetry,
		TelemetryURL:                 c.TelemetryURL,
		ErrorReporting:               c.ErrorReporting,
		ErrorReportingDSN:            c.ErrorReportingDSN,
		ProbeOfflineAfter:            c.ProbeOfflineAfter,

		NotificationHistoryRetentionDays: c.NotificationHistoryRetentionDays,
		TLSInfoRetentionDays:             c.TLSInfoRetentionDays,
	}
}
