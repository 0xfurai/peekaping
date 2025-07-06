package config

import (
	"bufio"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port      string `env:"PORT" default:"8034"`
	ClientURL string `env:"CLIENT_URL"`

	DBHost string `env:"DB_HOST"`
	DBPort string `env:"DB_PORT"`
	DBName string `env:"DB_NAME"`
	DBUser string `env:"DB_USER"`
	DBPass string `env:"DB_PASS"`
	DBType string `env:"DB_TYPE"`

	AccessTokenExpiresIn  time.Duration `env:"ACCESS_TOKEN_EXPIRED_IN" default:"15m"`
	AccessTokenSecretKey  string        `env:"ACCESS_TOKEN_SECRET_KEY"`
	RefreshTokenExpiresIn time.Duration `env:"REFRESH_TOKEN_EXPIRED_IN" default:"720h"`
	RefreshTokenSecretKey string        `env:"REFRESH_TOKEN_SECRET_KEY"`

	Mode string `env:"MODE" default:"dev"`

	// Loki logging
	LokiURL    string            `env:"LOKI_URL"`
	LokiLabels map[string]string // Set programmatically or extend env parsing for map
	Timezone   string            `env:"TZ" default:"UTC"`
}

func LoadConfig(path string) (config Config, err error) {
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

	// Count total provided environment variables
	totalProvided := len(envVarsFromFile) + len(envVarsFromEnv)
	fmt.Printf("Config loaded: %d environment variables provided (%d from .env file, %d from system env)\n",
		totalProvided, len(envVarsFromFile), len(envVarsFromEnv))

	// Print detailed environment variables
	printEnvVars("From .env file:", envVarsFromFile)
	printEnvVars("From system env:", envVarsFromEnv)

	os.Setenv("TZ", config.Timezone)

	return
}

func loadEnvFile(filePath string, config *Config, envVarsFromFile map[string]string) error {
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

func loadFromEnv(config *Config) map[string]string {
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

func setFieldsFromMap(config *Config, values map[string]string) error {
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
