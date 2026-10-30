package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

type DBConfig struct {
	DBHost string `env:"DB_HOST"`                           // validated in validateCustomRules
	DBPort string `env:"DB_PORT"`                           // validated in validateCustomRules
	DBName string `env:"DB_NAME" validate:"required,min=1"` // validated in validateCustomRules
	DBUser string `env:"DB_USER"`                           // validated in validateCustomRules
	DBPass string `env:"DB_PASS"`                           // validated in validateCustomRules
	DBType string `env:"DB_TYPE" validate:"required,db_type"`
}

type Config struct {
	// Environment remembers which variables the environment set, for the install settings
	Environment

	Port      string `env:"SERVER_PORT" validate:"required,port" default:"8034"`
	ClientURL string `env:"CLIENT_URL" validate:"url" default:"http://localhost:3000"`

	DBHost string `env:"DB_HOST"`                           // validated in validateCustomRules
	DBPort string `env:"DB_PORT"`                           // validated in validateCustomRules
	DBName string `env:"DB_NAME" validate:"required,min=1"` // validated in validateCustomRules
	DBUser string `env:"DB_USER"`                           // validated in validateCustomRules
	DBPass string `env:"DB_PASS"`                           // validated in validateCustomRules
	DBType string `env:"DB_TYPE" validate:"required,db_type"`

	// Which check a process that opens SQLite runs at startup: "quick" runs PRAGMA quick_check,
	// "full" runs PRAGMA integrity_check, and "off" runs none. A finding stops startup.
	SQLiteStartupCheck string `env:"SQLITE_STARTUP_CHECK" validate:"oneof=quick full off" default:"quick"`

	Mode     string `env:"MODE" validate:"required,oneof=dev prod test" default:"dev"`
	LogLevel string `env:"LOG_LEVEL" validate:"omitempty,log_level" default:"info"`

	Timezone string `env:"TZ" validate:"required" default:"UTC"`

	// Redis configuration for queue
	RedisHost     string `env:"REDIS_HOST" validate:"required" default:"redis"`
	RedisPort     string `env:"REDIS_PORT" validate:"required,port" default:"6379"`
	RedisUsername string `env:"REDIS_USERNAME" default:""`
	RedisPassword string `env:"REDIS_PASSWORD" default:""`
	RedisDB       int    `env:"REDIS_DB" validate:"min=0,max=15" default:"0"`

	// Queue configuration
	// Number of concurrent workers to process tasks
	QueueConcurrency int `env:"QUEUE_CONCURRENCY" validate:"min=1" default:"128"`

	// Single mode's scheduler runs this many checks at the same time. A check that is due
	// while the pool is full waits for a free slot.
	CheckConcurrency int `env:"CHECK_CONCURRENCY" validate:"min=1" default:"128"`

	// Bruteforce protection settings
	// Maximum number of failed login attempts allowed within the time window
	// After exceeding this limit, the account will be temporarily locked
	BruteforceMaxAttempts int `env:"BRUTEFORCE_MAX_ATTEMPTS" default:"20"`

	// Time window for counting failed login attempts
	// Only attempts within this window are counted towards the max attempts limit
	// Examples: "1m", "5m", "1h", "24h"
	BruteforceWindow time.Duration `env:"BRUTEFORCE_WINDOW" default:"1m"`

	// Duration to lock the account after exceeding the maximum attempts
	// During this period, all login attempts will be blocked with HTTP 429
	// Examples: "5m", "30m", "1h", "24h"
	BruteforceLockout time.Duration `env:"BRUTEFORCE_LOCKOUT" default:"1m"`

	// A second counter for the login lockout, per account and independent of the client's
	// address, so that guessing one account's password from many addresses (or behind a proxy
	// that hides them) still locks the account. A successful login resets both counters.
	// Maximum number of failed logins for one account within the window
	BruteforceAccountMaxAttempts int `env:"BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS" default:"100"`

	// How long the count of an account's failed logins is kept, from the first of them. Until
	// it has passed, each further failure after a lock ends locks the account again, so guessing
	// is bounded to the maximum per window plus one attempt each time a lock ends.
	// Examples: "10m", "1h"
	BruteforceAccountWindow time.Duration `env:"BRUTEFORCE_ACCOUNT_WINDOW" default:"10m"`

	// How long one lock lasts. After an attack stops, the owner is back in within the window at
	// most; an attacker who keeps going can keep the account locked. Examples: "1m", "5m"
	BruteforceAccountLockout time.Duration `env:"BRUTEFORCE_ACCOUNT_LOCKOUT" default:"1m"`

	// Proxies whose X-Forwarded-For and X-Real-IP headers the server believes: a
	// comma-separated list of IP addresses and CIDR ranges, such as "127.0.0.1,10.0.0.0/8".
	// A request from any other address is attributed to the address it connected from, since a
	// client can send those headers itself. The login lockout counts failures per client
	// address. Empty (the default) trusts nobody.
	TrustedProxies string `env:"TRUSTED_PROXIES"` // parsed by httpx.ParseTrustedProxies

	// Origins whose pages may read the API's responses: a comma-separated list such as
	// "https://peek.example,http://localhost:8383". A page on any other origin can still send
	// requests, but its browser won't let it read the answers. "*" alone allows any origin.
	// Empty (the default) allows the origin of CLIENT_URL.
	CORSOrigins string `env:"CORS_ORIGINS"` // parsed by httpx.ParseCORSOrigins

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

	// Usage ping (ADR 0003). The ping is on until an admin opts out. TELEMETRY=off (or false, 0,
	// no) forces it off whatever the admin chose; TelemetryForcedOff reads it. It stays a string
	// so the config doesn't guess at other spellings. DO_NOT_TRACK is not read: it was dropped
	// on purpose, so setting it changes nothing.
	Telemetry string `env:"TELEMETRY"`
	// TelemetryURL is where the usage ping is sent. Tests and local stacks point it elsewhere.
	TelemetryURL string `env:"TELEMETRY_URL" default:"https://telemetry.peekaping.com/v1/ping"`

	// Error reports (ADR 0003). They are on until an admin opts out. ERROR_REPORTING=off (or
	// false, 0, no) forces them off whatever the admin chose; ErrorReportingForcedOff reads it.
	ErrorReporting string `env:"ERROR_REPORTING"`
	// ErrorReportingDSN replaces the Sentry DSN built into the binary, to send the reports to
	// another Sentry, to GlitchTip or to a server of your own
	ErrorReportingDSN string `env:"ERROR_REPORTING_DSN"`

	// ProbeBootstrapToken is the token of the local probe. With it set, the api stores its hash
	// on the local probe at startup, so that a compose or Helm stack comes up with a probe token
	// and no UI step. At least 32 characters, checked by the api's Validate. Empty leaves the
	// local probe as it is. peekaping (single mode) doesn't read it into this config: its process
	// checks the local probe's monitors, and it refuses a probe that uses the local probe's token.
	ProbeBootstrapToken string `env:"PROBE_BOOTSTRAP_TOKEN"`

	// ProbeOfflineAfter is how long an online probe's instances may all go unseen before the
	// sweep sets the probe offline. A Go duration such as "3m" or "90s". The api and peekaping,
	// which run the sweep, check that it is from MinProbeOfflineAfter to MaxProbeOfflineAfter.
	ProbeOfflineAfter time.Duration `env:"PROBE_OFFLINE_AFTER" default:"3m"`

	// Account emails: password reset, email verification and invites are sent by one sender for
	// the whole install. With ACCOUNT_SMTP_HOST unset (the default) they are off: the app shows
	// no "Forgot password" link and no verification banner, and invites are copied, not emailed.
	// ACCOUNT_SMTP_FROM is required when the host is set, which ValidateAccountSMTP checks. These are
	// install settings: read them through installsettings.Resolver, which also takes the sender an
	// instance admin saved when the variables are unset.
	AccountSMTPHost     string `env:"ACCOUNT_SMTP_HOST"`
	AccountSMTPPort     int    `env:"ACCOUNT_SMTP_PORT" validate:"min=1,max=65535" default:"587"`
	AccountSMTPUser     string `env:"ACCOUNT_SMTP_USER"`
	AccountSMTPPassword string `env:"ACCOUNT_SMTP_PASSWORD"`
	AccountSMTPFrom     string `env:"ACCOUNT_SMTP_FROM"`
	// AccountSMTPTLS is how the connection is secured: starttls (the default, and it fails when
	// the server doesn't offer it), tls (TLS from the first byte, usually port 465) or none
	AccountSMTPTLS string `env:"ACCOUNT_SMTP_TLS" validate:"oneof=starttls tls none" default:"starttls"`

	// Who may create an account (PP-155, docs/adr/0005 section 11): "invite" (the default) lets the
	// first user sign up and everyone after join through an invite, "open" lets anyone sign up and
	// gives each a workspace of their own. Checked by ValidateSignup, which also requires a sender
	// for account emails in open mode. Empty counts as invite. An install setting: read it through
	// installsettings.Resolver.
	SignupMode string `env:"SIGNUP_MODE" default:"invite"`
	// SignupTurnstileSiteKey and SignupTurnstileSecret turn on Cloudflare Turnstile for open signup:
	// both or neither. The site key is public (the web app shows the widget with it); the secret
	// stays on the server, which checks every answer.
	SignupTurnstileSiteKey string `env:"SIGNUP_TURNSTILE_SITE_KEY"`
	SignupTurnstileSecret  string `env:"SIGNUP_TURNSTILE_SECRET"`
	// SignupTurnstileVerifyURL replaces Cloudflare's siteverify address. It has no environment
	// variable: tests point it at a fake server, and the binaries never set it.
	SignupTurnstileVerifyURL string

	// DisabledMonitorTypes is a comma-separated list of monitor types this install refuses and does
	// not run (PP-157), empty by default. The Docker monitor, for one, reads the Docker socket of
	// the machine the server runs on, which a shared install can't offer. DisabledMonitorTypeList
	// splits it, and ValidateDisabledMonitorTypes checks the entries at startup.
	DisabledMonitorTypes string `env:"DISABLED_MONITOR_TYPES"`

	// BlockPrivateTargets keeps the checks from reaching the internal network (PP-156): loopback,
	// private and link-local addresses, and cloud metadata. Off by default, since a self-hosted
	// install monitors its own network; the cloud turns it on. A remote probe blocks when its own
	// setting is on or the server's is, which the server states in hello.
	BlockPrivateTargets bool `env:"BLOCK_PRIVATE_TARGETS" default:"false"`

	// VerifyCustomDomains makes a custom domain serve its status page only once its owner has
	// proved the domain points at this install (PP-165): a TXT record at _peekaping-verify.<domain>
	// holding the domain's token, or a CNAME to CustomDomainCNAMETarget. Off by default, since a
	// self-hosted install trusts its people to add their own domains; the cloud turns it on, so
	// that a workspace can't hold a domain it doesn't own.
	VerifyCustomDomains bool `env:"VERIFY_CUSTOM_DOMAINS" default:"false"`
	// CustomDomainCNAMETarget is the host a verified custom domain may be a CNAME to. Empty, the
	// default, means the host of the public URL (CLIENT_URL, or the address an instance admin
	// saved); see CustomDomainCNAMETargetFor. ValidateCustomDomainCNAMETarget checks it at startup.
	CustomDomainCNAMETarget string `env:"CUSTOM_DOMAIN_CNAME_TARGET"`

	// APIRateLimitPerWorkspace and APIRateLimitPerAPIKey cap the requests a workspace, and one of
	// its API keys, may make per minute (PP-159). 0, the default, is off. A request over either
	// answers 429 with Retry-After. Each api replica counts for itself (see package ratelimit).
	APIRateLimitPerWorkspace int `env:"API_RATE_LIMIT_PER_WORKSPACE" validate:"min=0" default:"0"`
	APIRateLimitPerAPIKey    int `env:"API_RATE_LIMIT_PER_API_KEY" validate:"min=0" default:"0"`
	// APIRateLimitWindow is the time the two limits count over. It has no environment variable: it
	// is a minute unless a test shortens it.
	APIRateLimitWindow time.Duration

	// ChecksPerHostPerMinute caps how many checks to one target host a checking process runs per
	// minute, across all workspaces (PP-159). 0, the default, is off. A check whose turn is a few
	// seconds away waits for it; a check further out skips its tick. The cap is per process: the
	// server's scheduler and each remote probe count for themselves. A probe uses the server's value
	// from hello when that is stricter than its own.
	ChecksPerHostPerMinute int `env:"CHECKS_PER_HOST_PER_MINUTE" validate:"min=0" default:"0"`

	// MinCheckInterval is the smallest check interval and retry interval, in seconds, a monitor can
	// be saved with (PP-159). 20, the default, is the smallest a monitor ever had; a value below it
	// stops startup. A monitor stored with less is checked at the minimum. MinCheckIntervalSeconds
	// is the value to read: a Config built without the variable counts as 20.
	MinCheckInterval int `env:"MIN_CHECK_INTERVAL" validate:"min=20" default:"20"`

	// WorkspaceLimit* are the install's default limits (PP-160, package limits): what a workspace
	// that has no limits of its own is held to. Each is unset, or 0, by default, which is no limit,
	// so an install that sets none has no limits. WorkspaceLimitMonitors counts active monitors,
	// WorkspaceLimitMinCheckInterval is in seconds and applies beside MIN_CHECK_INTERVAL (the
	// stricter wins), and WorkspaceLimitProbesPerMonitor is how many probes one monitor runs on.
	// These are install settings: read them through installsettings.Resolver, which also takes the
	// default an instance admin saved when a variable is unset.
	WorkspaceLimitMonitors         int `env:"WORKSPACE_LIMIT_MONITORS" validate:"min=0" default:"0"`
	WorkspaceLimitMinCheckInterval int `env:"WORKSPACE_LIMIT_MIN_CHECK_INTERVAL" validate:"min=0" default:"0"`
	WorkspaceLimitStatusPages      int `env:"WORKSPACE_LIMIT_STATUS_PAGES" validate:"min=0" default:"0"`
	WorkspaceLimitCustomDomains    int `env:"WORKSPACE_LIMIT_CUSTOM_DOMAINS" validate:"min=0" default:"0"`
	WorkspaceLimitMembers          int `env:"WORKSPACE_LIMIT_MEMBERS" validate:"min=0" default:"0"`
	WorkspaceLimitOwnedWorkspaces  int `env:"WORKSPACE_LIMIT_OWNED_WORKSPACES" validate:"min=0" default:"0"`
	WorkspaceLimitProbesPerMonitor int `env:"WORKSPACE_LIMIT_PROBES_PER_MONITOR" validate:"min=0" default:"0"`
	WorkspaceLimitRetentionDays    int `env:"WORKSPACE_LIMIT_RETENTION_DAYS" validate:"min=0" default:"0"`
	// WorkspaceLimitsName is the install's name for its default limits (PP-163), such as "Free tier"
	// on the cloud: free text, shown on a workspace's Usage page when its limits are the defaults.
	// Unset, the page says an administrator set them. An install setting: read it through
	// installsettings.Resolver.
	WorkspaceLimitsName string `env:"WORKSPACE_LIMITS_NAME"`

	// SupportContact is how a member of a suspended workspace gets in touch with whoever runs the
	// install (PP-372): free text, an email address or a URL, shown in the suspension banner and
	// returned by GET /me. Unset, the banner says nothing about how.
	SupportContact string `env:"SUPPORT_CONTACT"`

	// InstallSettingsCacheLifetime is how long a process keeps the install settings it read from the
	// database (package installsettings), so a change saved on another api replica takes effect
	// within it. It has no environment variable: it is installsettings.DefaultCacheLifetime unless a
	// test shortens it.
	InstallSettingsCacheLifetime time.Duration
}

// AbsoluteMinCheckInterval is the shortest check interval, in seconds, that any install allows:
// the monitor DTOs' minimum. It is also the floor of MIN_CHECK_INTERVAL.
const AbsoluteMinCheckInterval = 20

// MinCheckIntervalSeconds is MIN_CHECK_INTERVAL, and never less than AbsoluteMinCheckInterval
func (c *Config) MinCheckIntervalSeconds() int {
	return max(c.MinCheckInterval, AbsoluteMinCheckInterval)
}

// MinProbeOfflineAfter is the shortest PROBE_OFFLINE_AFTER. A probe instance that says bye is left
// last seen a minute and a second ago (probe.RetiredAt), and the sweep looks at the probes every 30
// seconds. A shorter limit would raise an offline notification for a probe that restarts
// gracefully, before the new instance has said hello. The room the floor leaves is pinned in
// TestRetiredAt_LeavesRoomUnderTheSmallestOfflineAfter.
const MinProbeOfflineAfter = 2 * time.Minute

// MaxProbeOfflineAfter is the longest PROBE_OFFLINE_AFTER. It equals how long the sweep keeps an
// instance that went unseen (probe.DefaultInstanceRetention, which is this constant), because an
// instance older than that is about to be pruned. A longer limit would let the sweep's backstop set
// an offline probe online on such an instance, publish probe.online, and set the probe offline
// again on the next pass, once the instance was gone: a false pair of notifications.
const MaxProbeOfflineAfter = 24 * time.Hour

// MongoStorageRemovedDocsURL is the page that explains how to move a MongoDB install to SQLite or
// PostgreSQL with "bun db import-mongo"
const MongoStorageRemovedDocsURL = "https://docs.peekaping.com/self-hosting/docker-with-mongo#move-to-sqlite-or-postgresql"

// ErrMongoStorageRemoved is why every binary stops when DB_TYPE names MongoDB (mongo or mongodb):
// MongoDB storage was removed (ADR 0001). The MongoDB monitor type, which checks someone else's
// MongoDB server, is not affected. RefuseMongoStorage wraps it with the DB_TYPE the install set.
var ErrMongoStorageRemoved = errors.New("MongoDB storage was removed. " +
	"Move this install's data to SQLite or PostgreSQL with \"bun db import-mongo\", then set DB_TYPE to sqlite or postgres. " +
	"See " + MongoStorageRemovedDocsURL)

// RefuseMongoStorage returns the error a binary stops with when dbType names MongoDB storage, and
// nil for any other value. It matches mongo and mongodb whatever the case, as the db_type validator
// does, and the error wraps ErrMongoStorageRemoved. The db_type validator doesn't accept those
// values, so a binary calls this first to give this error and not the validator's.
func RefuseMongoStorage(dbType string) error {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "mongo", "mongodb":
		return fmt.Errorf("DB_TYPE=%s: %w", dbType, ErrMongoStorageRemoved)
	}
	return nil
}

// TelemetryForcedOff reports whether the environment forces the usage ping off: TELEMETRY is
// off, false, 0 or no. Case and surrounding spaces don't matter.
func (c *Config) TelemetryForcedOff() bool {
	return switchedOff(c.Telemetry)
}

// ErrorReportingForcedOff reports whether the environment forces error reports off:
// ERROR_REPORTING is off, false, 0 or no. It reads the same spellings as TelemetryForcedOff, and
// the two switches don't affect each other.
func (c *Config) ErrorReportingForcedOff() bool {
	return switchedOff(c.ErrorReporting)
}

// switchedOff reports whether value is a spelling of "off": off, false, 0 or no
func switchedOff(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "false", "0", "no":
		return true
	}
	return false
}

var validate = validator.New()

func LoadConfig[T any](path string) (config T, err error) {
	// Register custom validators
	RegisterCustomValidators(validate)

	// Try to load from .env file first
	envFile := path + "/.env"
	envVarsFromFile := make(map[string]string)
	err = loadEnvFile(envFile, &config, envVarsFromFile)
	if err != nil {
		// Only return error if it's not a "file not found" error
		if !os.IsNotExist(err) {
			return
		}
		// Clear the error if it's just file not found (we'll use env vars instead)
		err = nil
	}

	// Override with environment variables (takes precedence)
	envVarsFromEnv, err := loadFromEnv(&config)
	if err != nil {
		return config, fmt.Errorf("configuration validation failed: %w", err)
	}
	// The install settings tell a value the environment set from a default (see Environment)
	recordEnvironment(&config, envVarsFromFile, envVarsFromEnv)

	// Apply default values for fields that weren't set
	defaultsApplied := applyDefaults(&config)

	// Validate the configuration
	err = validateConfig(&config)
	if err != nil {
		return config, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Count total provided environment variables
	totalProvided := len(envVarsFromFile) + len(envVarsFromEnv)
	totalDefaults := len(defaultsApplied)
	fmt.Printf("Config loaded: %d environment variables provided (%d from .env file, %d from system env), %d defaults applied\n",
		totalProvided, len(envVarsFromFile), len(envVarsFromEnv), totalDefaults)

	// Print detailed environment variables
	printEnvVars("From .env file:", envVarsFromFile)
	printEnvVars("From system env:", envVarsFromEnv)
	printEnvVars("Defaults applied:", defaultsApplied)

	return
}

func validateConfig[T any](config *T) error {
	// Validate using struct tags
	if err := validate.Struct(config); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			var errorMessages []string
			for _, fieldError := range validationErrors {
				// MongoDB storage was removed: say so, not that the value is invalid
				if fieldError.Tag() == "db_type" {
					if err := RefuseMongoStorage(fmt.Sprint(fieldError.Value())); err != nil {
						return err
					}
				}
				errorMessages = append(errorMessages, formatValidationError(fieldError))
			}
			return fmt.Errorf("validation errors: %s", strings.Join(errorMessages, "; "))
		}
		return err
	}

	return nil
}

// validationMessages say in words which rule a field broke, by tag. The field's name is the one
// verb.
var validationMessages = map[string]string{
	"required":  "%s is required",
	"url":       "%s must be a valid URL",
	"numeric":   "%s must be a valid number",
	"port":      "%s must be a valid port number (1-65535)",
	"db_type":   "%s must be one of: postgres, postgresql, mysql, sqlite",
	"log_level": "%s must be one of: debug, info, warn, warning, error, dpanic, panic, fatal",
}

// paramValidationMessages are validationMessages for the rules with a parameter: the field's name
// is the first verb, the parameter the second.
var paramValidationMessages = map[string]string{
	"duration_min": "%s must be at least %s",
	"max":          "%s must be at most %s",
	"oneof":        "%s must be one of: %s",
}

func formatValidationError(err validator.FieldError) string {
	field := err.Field()
	tag := err.Tag()

	if tag == "min" {
		return formatMinError(err)
	}
	if message, ok := validationMessages[tag]; ok {
		return fmt.Sprintf(message, field)
	}
	if message, ok := paramValidationMessages[tag]; ok {
		return fmt.Sprintf(message, field, err.Param())
	}
	return fmt.Sprintf("%s failed validation: %s", field, tag)
}

// formatMinError words a min rule: a number is at least, a string at least that many characters
func formatMinError(err validator.FieldError) string {
	switch err.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%s must be at least %s", err.Field(), err.Param())
	}
	return fmt.Sprintf("%s must be at least %s characters", err.Field(), err.Param())
}

func ValidateDatabaseCustomRules(config *DBConfig) error {
	// Validate database-specific requirements
	switch config.DBType {
	case "postgres", "postgresql", "mysql":
		if config.DBHost == "" {
			return fmt.Errorf("DB_HOST is required for %s database", config.DBType)
		}
		if config.DBPort == "" {
			return fmt.Errorf("DB_PORT is required for %s database", config.DBType)
		}
		if config.DBUser == "" {
			return fmt.Errorf("DB_USER is required for %s database", config.DBType)
		}
		if config.DBPass == "" {
			return fmt.Errorf("DB_PASS is required for %s database", config.DBType)
		}
		// Validate port format for database connection
		if _, err := strconv.Atoi(config.DBPort); err != nil {
			return fmt.Errorf("DB_PORT must be a valid number for %s database", config.DBType)
		}
	case "sqlite":
		// SQLite only requires a database file path
		if config.DBName == "" {
			return fmt.Errorf("DB_NAME (database file path) is required for SQLite database")
		}
	}

	return nil
}

func applyDefaults[T any](config *T) map[string]string {
	configType := reflect.TypeOf(*config)
	configValue := reflect.ValueOf(config).Elem()
	defaultsApplied := make(map[string]string)

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		fieldValue := configValue.Field(i)
		defaultValue := field.Tag.Get("default")
		envKey := field.Tag.Get("env")

		if defaultValue == "" || !fieldValue.IsZero() {
			continue
		}
		// A default that doesn't parse, or a kind the loader doesn't read, leaves the field zero
		if set, err := setFieldFromText(field, fieldValue, defaultValue); set && err == nil && envKey != "" {
			defaultsApplied[envKey] = defaultValue
		}
	}

	return defaultsApplied
}

// setFieldFromText sets a string, int, int64 (a time.Duration included) or bool field from its
// text. It reports whether the field's kind is one it reads, and the error of a number or boolean
// that doesn't parse, which leaves the field as it was.
func setFieldFromText(field reflect.StructField, fieldValue reflect.Value, value string) (bool, error) {
	switch fieldValue.Kind() {
	case reflect.String:
		fieldValue.SetString(value)
	case reflect.Int, reflect.Int64:
		intValue, err := parseInt(field.Type, value)
		if err != nil {
			return true, err
		}
		fieldValue.SetInt(intValue)
	case reflect.Bool:
		boolValue, err := parseBool(field, value)
		if err != nil {
			return true, err
		}
		fieldValue.SetBool(boolValue)
	default:
		return false, nil
	}
	return true, nil
}

// InvalidBooleanError is a boolean variable set to text strconv.ParseBool doesn't read, such as yes
// or on. It would otherwise turn a setting meant to be on into off without a word, so startup stops.
type InvalidBooleanError struct {
	Name  string
	Value string
}

func (e *InvalidBooleanError) Error() string {
	return fmt.Sprintf("%s must be a boolean: 1, t, T, TRUE, true, True, 0, f, F, FALSE, false or False, not %q", e.Name, e.Value)
}

// parseBool reads value as strconv.ParseBool does, and names the field's variable when it can't
func parseBool(field reflect.StructField, value string) (bool, error) {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, &InvalidBooleanError{Name: field.Tag.Get("env"), Value: value}
	}
	return parsed, nil
}

// parseInt reads value as a number, or as a duration for a time.Duration
func parseInt(fieldType reflect.Type, value string) (int64, error) {
	// Special case for time.Duration
	if fieldType == reflect.TypeOf(time.Duration(0)) {
		duration, err := time.ParseDuration(value)
		return int64(duration), err
	}
	return strconv.ParseInt(value, 10, 64)
}

func loadEnvFile[T any](filePath string, config *T, envVarsFromFile map[string]string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue // Skip comments and empty lines
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue // Skip invalid lines
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if they exist
		value = strings.Trim(value, `"'`)
		envVarsFromFile[key] = value
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return setFieldsFromMap(config, envVarsFromFile)
}

func loadFromEnv[T any](config *T) (map[string]string, error) {
	// Get all the relevant environment variables at once
	envVars := make(map[string]string)

	// Use reflection to read struct tags and load corresponding environment variables
	configType := reflect.TypeOf(*config)
	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		envKey := field.Tag.Get("env")
		if envKey != "" {
			if value := os.Getenv(envKey); value != "" {
				envVars[envKey] = value
			}
		}
	}

	return envVars, setFieldsFromMap(config, envVars)
}

func setFieldsFromMap[T any](config *T, values map[string]string) error {
	configType := reflect.TypeOf(*config)
	configValue := reflect.ValueOf(config).Elem()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		fieldValue := configValue.Field(i)
		envKey := field.Tag.Get("env")

		if envKey == "" || !fieldValue.CanSet() {
			continue
		}

		value, exists := values[envKey]
		if !exists || value == "" {
			continue
		}

		if _, err := setFieldFromText(field, fieldValue, value); err != nil {
			var invalid *InvalidBooleanError
			if errors.As(err, &invalid) {
				return err
			}
			fmt.Printf("Warning: could not parse %s=%s as number: %v\n", envKey, value, err)
		}
	}

	return nil
}

func printEnvVars(title string, envVars map[string]string) {
	if len(envVars) == 0 {
		return
	}

	fmt.Printf("  %s\n", title)
	for key, value := range envVars {
		maskedValue := maskSensitiveValue(key, value)
		fmt.Printf("    %s=%s\n", key, maskedValue)
	}
}

func maskSensitiveValue(key, value string) string {
	if value == "" {
		return value
	}

	// Define sensitive key patterns
	sensitiveKeys := []string{
		"SECRET_KEY", "SECRET", "PASSWORD", "PASS", "TOKEN", "API_KEY", "PRIVATE_KEY", "DSN",
	}

	keyUpper := strings.ToUpper(key)
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(keyUpper, sensitive) {
			return "*****"
		}
	}

	// For database URI or connection strings, mask everything after the protocol
	if strings.Contains(keyUpper, "URI") || strings.Contains(keyUpper, "URL") {
		if strings.Contains(value, "://") {
			parts := strings.SplitN(value, "://", 2)
			if len(parts) == 2 {
				return parts[0] + "://***"
			}
		}
	}

	return value
}
