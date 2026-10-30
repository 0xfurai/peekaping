package main

import (
	"fmt"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/healthcheck/executor"
	"peekaping/internal/modules/probe"

	"github.com/go-playground/validator/v10"
)

// Config defines the configuration schema for the API service
type Config struct {
	// Which variables the environment set: an install setting whose variable is set is locked to it
	config.Environment

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

	// Which check the process runs when it opens SQLite: quick (PRAGMA quick_check), full
	// (PRAGMA integrity_check, which reads the whole file) or off. A finding stops startup.
	SQLiteStartupCheck string `env:"SQLITE_STARTUP_CHECK" validate:"oneof=quick full off" default:"quick"`

	// Common settings
	Mode     string `env:"MODE" validate:"required,oneof=dev prod test" default:"dev"`
	LogLevel string `env:"LOG_LEVEL" validate:"omitempty,log_level" default:"info"`
	Timezone string `env:"TZ" validate:"required" default:"UTC"`

	// Redis configuration
	RedisHost     string `env:"REDIS_HOST" validate:"required" default:"redis"`
	RedisPort     string `env:"REDIS_PORT" validate:"required,port" default:"6379"`
	RedisUsername string `env:"REDIS_USERNAME" default:""`
	RedisPassword string `env:"REDIS_PASSWORD" default:""`
	RedisDB       int    `env:"REDIS_DB" validate:"min=0,max=15" default:"0"`

	// Queue configuration
	QueueConcurrency int `env:"QUEUE_CONCURRENCY" validate:"min=1" default:"128"`

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

	ServiceName string `env:"SERVICE_NAME" validate:"required,min=1" default:"peekaping:api"`

	// Prometheus metrics
	// Set to true to expose an unauthenticated /metrics endpoint for Prometheus
	// to scrape. Off by default since it exposes monitor names and status.
	MetricsEnabled bool `env:"METRICS_ENABLED" default:"false"`

	// A port that serves /metrics alone, on all interfaces, instead of the API port. Unset (the
	// default), /metrics is on the API port. Needs METRICS_ENABLED=true and must differ from
	// SERVER_PORT, which config.ValidateMetricsPort checks.
	MetricsPort string `env:"METRICS_PORT"`

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

	// The local probe's token. Set, it is stored on the local probe at startup. Validated in Validate().
	ProbeBootstrapToken string `env:"PROBE_BOOTSTRAP_TOKEN"`

	// How long an online probe's instances may all go unseen before the probe counts as offline.
	// A Go duration, from config.MinProbeOfflineAfter to config.MaxProbeOfflineAfter. The sweep that
	// sets the probe offline runs on the api replica that holds the leader lock. Validated in
	// Validate().
	ProbeOfflineAfter time.Duration `env:"PROBE_OFFLINE_AFTER" default:"3m"`

	// Account emails: password reset, email verification and invites are sent by one sender for
	// the whole install. With ACCOUNT_SMTP_HOST unset (the default) they are off: the app shows
	// no "Forgot password" link and no verification banner, and invites are copied, not emailed.
	// ACCOUNT_SMTP_FROM is required when the host is set, which ValidateAccountSMTP checks.
	AccountSMTPHost     string `env:"ACCOUNT_SMTP_HOST"`
	AccountSMTPPort     int    `env:"ACCOUNT_SMTP_PORT" validate:"min=1,max=65535" default:"587"`
	AccountSMTPUser     string `env:"ACCOUNT_SMTP_USER"`
	AccountSMTPPassword string `env:"ACCOUNT_SMTP_PASSWORD"`
	AccountSMTPFrom     string `env:"ACCOUNT_SMTP_FROM"`
	// AccountSMTPTLS is how the connection is secured: starttls (the default, and it fails when
	// the server doesn't offer it), tls (TLS from the first byte, usually port 465) or none
	AccountSMTPTLS string `env:"ACCOUNT_SMTP_TLS" validate:"oneof=starttls tls none" default:"starttls"`

	// Who may create an account: invite (the default) or open. Open signup needs a sender for
	// account emails; Turnstile (both keys or neither) guards open signups against bots. Validated
	// in Validate() by config.ValidateSignup.
	SignupMode             string `env:"SIGNUP_MODE" default:"invite"`
	SignupTurnstileSiteKey string `env:"SIGNUP_TURNSTILE_SITE_KEY"`
	SignupTurnstileSecret  string `env:"SIGNUP_TURNSTILE_SECRET"`

	// Monitor types this install refuses and does not run, comma-separated and empty by default.
	// Validated in Validate() against the types the server has an executor for.
	DisabledMonitorTypes string `env:"DISABLED_MONITOR_TYPES"`

	// BlockPrivateTargets keeps monitors from reaching loopback, private and link-local addresses
	// and cloud metadata. Off by default: a self-hosted install monitors its own network.
	BlockPrivateTargets bool `env:"BLOCK_PRIVATE_TARGETS" default:"false"`

	// Requests per minute a workspace, and one of its API keys, may make. 0, the default, is off.
	APIRateLimitPerWorkspace int `env:"API_RATE_LIMIT_PER_WORKSPACE" validate:"min=0" default:"0"`
	APIRateLimitPerAPIKey    int `env:"API_RATE_LIMIT_PER_API_KEY" validate:"min=0" default:"0"`

	// Checks per minute to one target host that one checking process runs, all workspaces together.
	// 0, the default, is off.
	ChecksPerHostPerMinute int `env:"CHECKS_PER_HOST_PER_MINUTE" validate:"min=0" default:"0"`

	// The smallest check interval and retry interval, in seconds, that a monitor can be saved with.
	// Never below 20: the timeout minimum (16) and the 0.8 rule rely on it.
	MinCheckInterval int `env:"MIN_CHECK_INTERVAL" validate:"min=20" default:"20"`

	// The install's default limits of a workspace that has none of its own (PP-160). Unset or 0,
	// the default, is no limit.
	WorkspaceLimitMonitors         int `env:"WORKSPACE_LIMIT_MONITORS" validate:"min=0" default:"0"`
	WorkspaceLimitMinCheckInterval int `env:"WORKSPACE_LIMIT_MIN_CHECK_INTERVAL" validate:"min=0" default:"0"`
	WorkspaceLimitStatusPages      int `env:"WORKSPACE_LIMIT_STATUS_PAGES" validate:"min=0" default:"0"`
	WorkspaceLimitCustomDomains    int `env:"WORKSPACE_LIMIT_CUSTOM_DOMAINS" validate:"min=0" default:"0"`
	WorkspaceLimitMembers          int `env:"WORKSPACE_LIMIT_MEMBERS" validate:"min=0" default:"0"`
	WorkspaceLimitOwnedWorkspaces  int `env:"WORKSPACE_LIMIT_OWNED_WORKSPACES" validate:"min=0" default:"0"`
	WorkspaceLimitProbesPerMonitor int `env:"WORKSPACE_LIMIT_PROBES_PER_MONITOR" validate:"min=0" default:"0"`
	WorkspaceLimitRetentionDays    int `env:"WORKSPACE_LIMIT_RETENTION_DAYS" validate:"min=0" default:"0"`

	// The install's name for its default limits, such as "Free tier", shown on a workspace's Usage
	// page. Unset by default.
	WorkspaceLimitsName string `env:"WORKSPACE_LIMITS_NAME"`

	// How a member of a suspended workspace gets in touch with whoever runs the install: free text,
	// an email address or a URL. Unset by default.
	SupportContact string `env:"SUPPORT_CONTACT"`
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
	for _, check := range []func(*Config) error{validateStorage, validateBruteforce, validateAddresses, validateProbe, validateAccountsAndMonitors} {
		if err := check(cfg); err != nil {
			return err
		}
	}

	return nil
}

// validateStorage checks the database settings
func validateStorage(cfg *Config) error {
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

	return nil
}

// validateBruteforce checks the login lockout settings
func validateBruteforce(cfg *Config) error {
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

	return nil
}

// validateAddresses checks the settings that name a port, a proxy or an origin
func validateAddresses(cfg *Config) error {
	// A METRICS_PORT that isn't a port, equals SERVER_PORT or has no /metrics to serve stops startup
	if err := config.ValidateMetricsPort(cfg.MetricsEnabled, cfg.MetricsPort, cfg.Port); err != nil {
		return err
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

	return nil
}

// validateProbe checks the local probe's settings
func validateProbe(cfg *Config) error {
	// A short token is easy to guess, and it would give a probe's access to the monitors' configs.
	// The message doesn't repeat it.
	if cfg.ProbeBootstrapToken != "" && len(cfg.ProbeBootstrapToken) < probe.MinBootstrapTokenLength {
		return fmt.Errorf("PROBE_BOOTSTRAP_TOKEN must be at least %d characters", probe.MinBootstrapTokenLength)
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

	return nil
}

// validateAccountsAndMonitors checks the account email, signup and disabled monitor type settings
func validateAccountsAndMonitors(cfg *Config) error {
	// A sender host without a sender address stops startup, not the first password reset
	if err := config.ValidateAccountSMTP(cfg.AccountSMTPHost, cfg.AccountSMTPFrom, cfg.AccountSMTPTLS); err != nil {
		return err
	}

	// A mistyped mode, open signup without a sender and half a Turnstile setting stop startup
	if err := config.ValidateSignup(cfg.SignupMode, cfg.SignupTurnstileSiteKey, cfg.SignupTurnstileSecret, cfg.AccountSMTPHost); err != nil {
		return err
	}

	// A mistyped monitor type stops startup, with the types that exist
	if err := config.ValidateDisabledMonitorTypes(cfg.DisabledMonitorTypes, executor.KnownTypes()); err != nil {
		return err
	}

	return nil
}

// ToInternalConfig converts API config to internal config format
// This is needed for backward compatibility with existing code
func (c *Config) ToInternalConfig() *config.Config {
	return &config.Config{
		Environment:                    c.Environment,
		Port:                           c.Port,
		ClientURL:                      c.ClientURL,
		DBHost:                         c.DBHost,
		DBPort:                         c.DBPort,
		DBName:                         c.DBName,
		DBUser:                         c.DBUser,
		DBPass:                         c.DBPass,
		DBType:                         c.DBType,
		SQLiteStartupCheck:             c.SQLiteStartupCheck,
		Mode:                           c.Mode,
		LogLevel:                       c.LogLevel,
		Timezone:                       c.Timezone,
		RedisHost:                      c.RedisHost,
		RedisPort:                      c.RedisPort,
		RedisUsername:                  c.RedisUsername,
		RedisPassword:                  c.RedisPassword,
		RedisDB:                        c.RedisDB,
		QueueConcurrency:               c.QueueConcurrency,
		BruteforceMaxAttempts:          c.BruteforceMaxAttempts,
		BruteforceWindow:               c.BruteforceWindow,
		BruteforceLockout:              c.BruteforceLockout,
		BruteforceAccountMaxAttempts:   c.BruteforceAccountMaxAttempts,
		BruteforceAccountWindow:        c.BruteforceAccountWindow,
		BruteforceAccountLockout:       c.BruteforceAccountLockout,
		TrustedProxies:                 c.TrustedProxies,
		CORSOrigins:                    c.CORSOrigins,
		ServiceName:                    c.ServiceName,
		MetricsEnabled:                 c.MetricsEnabled,
		MetricsPort:                    c.MetricsPort,
		Telemetry:                      c.Telemetry,
		TelemetryURL:                   c.TelemetryURL,
		ErrorReporting:                 c.ErrorReporting,
		ErrorReportingDSN:              c.ErrorReportingDSN,
		ProbeBootstrapToken:            c.ProbeBootstrapToken,
		ProbeOfflineAfter:              c.ProbeOfflineAfter,
		AccountSMTPHost:                c.AccountSMTPHost,
		AccountSMTPPort:                c.AccountSMTPPort,
		AccountSMTPUser:                c.AccountSMTPUser,
		AccountSMTPPassword:            c.AccountSMTPPassword,
		AccountSMTPFrom:                c.AccountSMTPFrom,
		AccountSMTPTLS:                 c.AccountSMTPTLS,
		SignupMode:                     c.SignupMode,
		SignupTurnstileSiteKey:         c.SignupTurnstileSiteKey,
		SignupTurnstileSecret:          c.SignupTurnstileSecret,
		DisabledMonitorTypes:           c.DisabledMonitorTypes,
		BlockPrivateTargets:            c.BlockPrivateTargets,
		APIRateLimitPerWorkspace:       c.APIRateLimitPerWorkspace,
		APIRateLimitPerAPIKey:          c.APIRateLimitPerAPIKey,
		ChecksPerHostPerMinute:         c.ChecksPerHostPerMinute,
		MinCheckInterval:               c.MinCheckInterval,
		SupportContact:                 c.SupportContact,
		WorkspaceLimitMonitors:         c.WorkspaceLimitMonitors,
		WorkspaceLimitMinCheckInterval: c.WorkspaceLimitMinCheckInterval,
		WorkspaceLimitStatusPages:      c.WorkspaceLimitStatusPages,
		WorkspaceLimitCustomDomains:    c.WorkspaceLimitCustomDomains,
		WorkspaceLimitMembers:          c.WorkspaceLimitMembers,
		WorkspaceLimitOwnedWorkspaces:  c.WorkspaceLimitOwnedWorkspaces,
		WorkspaceLimitProbesPerMonitor: c.WorkspaceLimitProbesPerMonitor,
		WorkspaceLimitRetentionDays:    c.WorkspaceLimitRetentionDays,
		WorkspaceLimitsName:            c.WorkspaceLimitsName,

		NotificationHistoryRetentionDays: c.NotificationHistoryRetentionDays,
		TLSInfoRetentionDays:             c.TLSInfoRetentionDays,
	}
}
