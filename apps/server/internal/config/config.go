package config

import (
	"bufio"
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
	Port      string `env:"SERVER_PORT" validate:"required,port" default:"8034"`
	ClientURL string `env:"CLIENT_URL" validate:"url" default:"http://localhost:3000"`

	DBHost string `env:"DB_HOST"`                           // validated in validateCustomRules
	DBPort string `env:"DB_PORT"`                           // validated in validateCustomRules
	DBName string `env:"DB_NAME" validate:"required,min=1"` // validated in validateCustomRules
	DBUser string `env:"DB_USER"`                           // validated in validateCustomRules
	DBPass string `env:"DB_PASS"`                           // validated in validateCustomRules
	DBType string `env:"DB_TYPE" validate:"required,db_type"`

	Mode     string `env:"MODE" validate:"required,oneof=dev prod test" default:"dev"`
	LogLevel string `env:"LOG_LEVEL" validate:"omitempty,log_level" default:"info"`

	Timezone string `env:"TZ" validate:"required" default:"UTC"`

	// Redis configuration for queue
	RedisHost     string `env:"REDIS_HOST" validate:"required" default:"redis"`
	RedisPort     string `env:"REDIS_PORT" validate:"required,port" default:"6379"`
	RedisPassword string `env:"REDIS_PASSWORD" default:""`
	RedisDB       int    `env:"REDIS_DB" validate:"min=0,max=15" default:"0"`

	// Queue configuration
	// Number of concurrent workers to process tasks
	QueueConcurrency int `env:"QUEUE_CONCURRENCY" validate:"min=1" default:"128"`

	// Producer configuration
	// Number of concurrent producer goroutines for claiming and processing monitors
	ProducerConcurrency int `env:"PRODUCER_CONCURRENCY" validate:"min=1,max=128" default:"10"`

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

	// Single admin mode configuration
	// If set to true, only one admin user can be created
	EnableSingleAdmin bool `env:"ENABLE_SINGLE_ADMIN" default:"false"`

	ServiceName string `env:"SERVICE_NAME" validate:"required,min=1" default:"vigi:api"`
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
	envVarsFromEnv := loadFromEnv(&config)

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
				errorMessages = append(errorMessages, formatValidationError(fieldError))
			}
			return fmt.Errorf("validation errors: %s", strings.Join(errorMessages, "; "))
		}
		return err
	}

	return nil
}

func formatValidationError(err validator.FieldError) string {
	field := err.Field()
	tag := err.Tag()

	switch tag {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	case "numeric":
		return fmt.Sprintf("%s must be a valid number", field)
	case "port":
		return fmt.Sprintf("%s must be a valid port number (1-65535)", field)
	case "db_type":
		return fmt.Sprintf("%s must be one of: postgres, postgresql, mysql, sqlite, mongo, mongodb", field)
	case "log_level":
		return fmt.Sprintf("%s must be one of: debug, info, warn, warning, error, dpanic, panic, fatal", field)
	case "duration_min":
		return fmt.Sprintf("%s must be at least %s", field, err.Param())
	case "min":
		return fmt.Sprintf("%s must be at least %s characters", field, err.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, err.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, err.Param())
	default:
		return fmt.Sprintf("%s failed validation: %s", field, tag)
	}
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
	case "mongo", "mongodb":
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

		if defaultValue != "" && fieldValue.IsZero() {
			switch fieldValue.Kind() {
			case reflect.String:
				fieldValue.SetString(defaultValue)
				if envKey != "" {
					defaultsApplied[envKey] = defaultValue
				}
			case reflect.Int, reflect.Int64:
				var intValue int64
				var err error

				// Special case for time.Duration
				if field.Type == reflect.TypeOf(time.Duration(0)) {
					var duration time.Duration
					duration, err = time.ParseDuration(defaultValue)
					intValue = int64(duration)
				} else {
					intValue, err = strconv.ParseInt(defaultValue, 10, 64)
				}

				if err == nil {
					fieldValue.SetInt(intValue)
					if envKey != "" {
						defaultsApplied[envKey] = defaultValue
					}
				}
			case reflect.Bool:
				fieldValue.SetBool(defaultValue == "true" || defaultValue == "1")
				if envKey != "" {
					defaultsApplied[envKey] = defaultValue
				}
			}
		}
	}

	return defaultsApplied
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

func loadFromEnv[T any](config *T) map[string]string {
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

	setFieldsFromMap(config, envVars)
	return envVars
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

		switch fieldValue.Kind() {
		case reflect.String:
			fieldValue.SetString(value)
		case reflect.Int, reflect.Int64:
			var intValue int64
			var err error

			// Special case for time.Duration
			if field.Type == reflect.TypeOf(time.Duration(0)) {
				var duration time.Duration
				duration, err = time.ParseDuration(value)
				intValue = int64(duration)
			} else {
				intValue, err = strconv.ParseInt(value, 10, 64)
			}

			if err != nil {
				fmt.Printf("Warning: could not parse %s=%s as number: %v\n", envKey, value, err)
				continue
			}
			fieldValue.SetInt(intValue)
		case reflect.Bool:
			fieldValue.SetBool(value == "true" || value == "1")
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
		"SECRET_KEY", "SECRET", "PASSWORD", "PASS", "TOKEN", "API_KEY", "PRIVATE_KEY",
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
