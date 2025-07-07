package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConfig(t *testing.T) {
	// Register custom validators for testing
	RegisterCustomValidators()

	tests := []struct {
		name          string
		config        Config
		expectedError bool
		errorContains string
	}{
		{
			name: "valid config",
			config: Config{
				Port:                  "8080",
				ClientURL:             "http://localhost:3000",
				DBHost:                "localhost",
				DBPort:                "5432",
				DBName:                "testdb",
				DBUser:                "testuser",
				DBPass:                "testpass",
				DBType:                "postgres",
				AccessTokenExpiresIn:  time.Minute * 15,
				AccessTokenSecretKey:  "very-secret-key-123456",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "dev",
				Timezone:              "UTC",
			},
			expectedError: false,
		},
		{
			name: "missing required port",
			config: Config{
				ClientURL:             "http://localhost:3000",
				DBHost:                "localhost",
				DBPort:                "5432",
				DBName:                "testdb",
				DBUser:                "testuser",
				DBPass:                "testpass",
				DBType:                "postgres",
				AccessTokenExpiresIn:  time.Minute * 15,
				AccessTokenSecretKey:  "very-secret-key-123456",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "dev",
				Timezone:              "UTC",
			},
			expectedError: true,
			errorContains: "Port is required",
		},
		{
			name: "invalid port number",
			config: Config{
				Port:                  "99999",
				ClientURL:             "http://localhost:3000",
				DBHost:                "localhost",
				DBPort:                "5432",
				DBName:                "testdb",
				DBUser:                "testuser",
				DBPass:                "testpass",
				DBType:                "postgres",
				AccessTokenExpiresIn:  time.Minute * 15,
				AccessTokenSecretKey:  "very-secret-key-123456",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "dev",
				Timezone:              "UTC",
			},
			expectedError: true,
			errorContains: "Port must be a valid port number",
		},
		{
			name: "invalid client URL",
			config: Config{
				Port:                  "8080",
				ClientURL:             "not-a-url",
				DBHost:                "localhost",
				DBPort:                "5432",
				DBName:                "testdb",
				DBUser:                "testuser",
				DBPass:                "testpass",
				DBType:                "postgres",
				AccessTokenExpiresIn:  time.Minute * 15,
				AccessTokenSecretKey:  "very-secret-key-123456",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "dev",
				Timezone:              "UTC",
			},
			expectedError: true,
			errorContains: "ClientURL must be a valid URL",
		},
		{
			name: "invalid database type",
			config: Config{
				Port:                  "8080",
				ClientURL:             "http://localhost:3000",
				DBHost:                "localhost",
				DBPort:                "5432",
				DBName:                "testdb",
				DBUser:                "testuser",
				DBPass:                "testpass",
				DBType:                "invalid-db",
				AccessTokenExpiresIn:  time.Minute * 15,
				AccessTokenSecretKey:  "very-secret-key-123456",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "dev",
				Timezone:              "UTC",
			},
			expectedError: true,
			errorContains: "DBType must be one of: postgres, postgresql, mysql, sqlite, mongo, mongodb",
		},
		{
			name: "short access token secret",
			config: Config{
				Port:                  "8080",
				ClientURL:             "http://localhost:3000",
				DBHost:                "localhost",
				DBPort:                "5432",
				DBName:                "testdb",
				DBUser:                "testuser",
				DBPass:                "testpass",
				DBType:                "postgres",
				AccessTokenExpiresIn:  time.Minute * 15,
				AccessTokenSecretKey:  "short",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "dev",
				Timezone:              "UTC",
			},
			expectedError: true,
			errorContains: "AccessTokenSecretKey must be at least 16 characters",
		},
		{
			name: "invalid access token duration",
			config: Config{
				Port:                  "8080",
				ClientURL:             "http://localhost:3000",
				DBHost:                "localhost",
				DBPort:                "5432",
				DBName:                "testdb",
				DBUser:                "testuser",
				DBPass:                "testpass",
				DBType:                "postgres",
				AccessTokenExpiresIn:  time.Second * 30,
				AccessTokenSecretKey:  "very-secret-key-123456",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "dev",
				Timezone:              "UTC",
			},
			expectedError: true,
			errorContains: "AccessTokenExpiresIn must be at least 1m",
		},
		{
			name: "invalid mode",
			config: Config{
				Port:                  "8080",
				ClientURL:             "http://localhost:3000",
				DBHost:                "localhost",
				DBPort:                "5432",
				DBName:                "testdb",
				DBUser:                "testuser",
				DBPass:                "testpass",
				DBType:                "postgres",
				AccessTokenExpiresIn:  time.Minute * 15,
				AccessTokenSecretKey:  "very-secret-key-123456",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "invalid",
				Timezone:              "UTC",
			},
			expectedError: true,
			errorContains: "Mode must be one of: dev prod test",
		},
		{
			name: "valid SQLite config",
			config: Config{
				Port:                  "8080",
				ClientURL:             "http://localhost:3000",
				DBHost:                "",
				DBPort:                "",
				DBName:                "test.db",
				DBUser:                "",
				DBPass:                "",
				DBType:                "sqlite",
				AccessTokenExpiresIn:  time.Minute * 15,
				AccessTokenSecretKey:  "very-secret-key-123456",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "dev",
				Timezone:              "UTC",
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(&tt.config)
			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCustomRules(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		expectedError bool
		errorContains string
	}{
		{
			name: "access token expires after refresh token",
			config: Config{
				AccessTokenExpiresIn:  time.Hour * 2,
				RefreshTokenExpiresIn: time.Hour * 1,
			},
			expectedError: true,
			errorContains: "ACCESS_TOKEN_EXPIRED_IN must be less than REFRESH_TOKEN_EXPIRED_IN",
		},
		{
			name: "valid token expiration times",
			config: Config{
				AccessTokenExpiresIn:  time.Hour * 1,
				RefreshTokenExpiresIn: time.Hour * 24,
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCustomRules(&tt.config)
			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	config := Config{}
	applyDefaults(&config)

	assert.Equal(t, "dev", config.Mode)
	assert.Equal(t, "UTC", config.Timezone)
}

func TestCustomValidators(t *testing.T) {
	RegisterCustomValidators()

	t.Run("validatePort", func(t *testing.T) {
		tests := []struct {
			port  string
			valid bool
		}{
			{"8080", true},
			{"80", true},
			{"443", true},
			{"1", true},
			{"65535", true},
			{"0", false},
			{"65536", false},
			{"abc", false},
			{"", false},
		}

		for _, tt := range tests {
			// Create a minimal config for testing specific field validation
			config := Config{
				Port:                  tt.port,
				ClientURL:             "http://localhost:3000",
				DBName:                "test",
				DBType:                "sqlite",
				AccessTokenExpiresIn:  time.Minute * 15,
				AccessTokenSecretKey:  "very-secret-key-123456",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "dev",
				Timezone:              "UTC",
			}
			err := validate.Struct(config)
			if tt.valid {
				assert.NoError(t, err, "Port %s should be valid", tt.port)
			} else {
				assert.Error(t, err, "Port %s should be invalid", tt.port)
			}
		}
	})

	t.Run("validateDBType", func(t *testing.T) {
		tests := []struct {
			dbType string
			valid  bool
		}{
			{"postgres", true},
			{"postgresql", true},
			{"mysql", true},
			{"sqlite", true},
			{"mongo", true},
			{"mongodb", true},
			{"invalid", false},
			{"", false},
		}

		for _, tt := range tests {
			// Create a minimal config for testing specific field validation
			config := Config{
				Port:                  "8080",
				ClientURL:             "http://localhost:3000",
				DBName:                "test",
				DBType:                tt.dbType,
				AccessTokenExpiresIn:  time.Minute * 15,
				AccessTokenSecretKey:  "very-secret-key-123456",
				RefreshTokenExpiresIn: time.Hour * 24,
				RefreshTokenSecretKey: "another-secret-key-123456",
				Mode:                  "dev",
				Timezone:              "UTC",
			}
			err := validate.Struct(config)
			if tt.valid {
				assert.NoError(t, err, "DBType %s should be valid", tt.dbType)
			} else {
				assert.Error(t, err, "DBType %s should be invalid", tt.dbType)
			}
		}
	})
}

func TestLoadConfigWithValidation(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create a valid .env file
	envContent := `PORT=8080
CLIENT_URL=http://localhost:3000
DB_HOST=localhost
DB_PORT=5432
DB_NAME=testdb
DB_USER=testuser
DB_PASS=testpass
DB_TYPE=postgres
ACCESS_TOKEN_EXPIRED_IN=15m
ACCESS_TOKEN_SECRET_KEY=very-secret-key-123456
REFRESH_TOKEN_EXPIRED_IN=24h
REFRESH_TOKEN_SECRET_KEY=another-secret-key-123456
MODE=dev
TZ=UTC`

	envFile := tempDir + "/.env"
	err := os.WriteFile(envFile, []byte(envContent), 0644)
	require.NoError(t, err)

	// Test loading valid config
	config, err := LoadConfig(tempDir)
	assert.NoError(t, err)
	assert.Equal(t, "8080", config.Port)
	assert.Equal(t, "postgres", config.DBType)

	// Test loading invalid config
	invalidEnvContent := `PORT=invalid-port
CLIENT_URL=not-a-url
DB_TYPE=invalid-db`

	err = os.WriteFile(envFile, []byte(invalidEnvContent), 0644)
	require.NoError(t, err)

	_, err = LoadConfig(tempDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}
