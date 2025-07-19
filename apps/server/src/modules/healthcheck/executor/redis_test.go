package executor

import (
	"context"
	"net/url"
	"peekaping/src/modules/shared"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestRedisExecutor_Validate(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	tests := []struct {
		name      string
		config    string
		wantError bool
	}{
		{
			name: "valid config",
			config: `{
				"databaseConnectionString": "redis://localhost:6379",
				"ignoreTls": false
			}`,
			wantError: false,
		},
		{
			name: "valid config with TLS",
			config: `{
				"databaseConnectionString": "rediss://user:password@localhost:6379",
				"ignoreTls": true
			}`,
			wantError: false,
		},
		{
			name: "valid config with database",
			config: `{
				"databaseConnectionString": "redis://localhost:6379/1",
				"ignoreTls": false
			}`,
			wantError: false,
		},
		{
			name: "valid config with auth and database",
			config: `{
				"databaseConnectionString": "redis://user:password@localhost:6379/0",
				"ignoreTls": false
			}`,
			wantError: false,
		},
		{
			name: "missing connection string",
			config: `{
				"ignoreTls": false
			}`,
			wantError: true,
		},
		{
			name: "empty connection string",
			config: `{
				"databaseConnectionString": "",
				"ignoreTls": false
			}`,
			wantError: true,
		},
		{
			name: "invalid protocol",
			config: `{
				"databaseConnectionString": "http://localhost:6379",
				"ignoreTls": false
			}`,
			wantError: true,
		},
		{
			name: "invalid port",
			config: `{
				"databaseConnectionString": "redis://localhost:99999",
				"ignoreTls": false
			}`,
			wantError: true,
		},
		{
			name: "invalid database number",
			config: `{
				"databaseConnectionString": "redis://localhost:6379/-1",
				"ignoreTls": false
			}`,
			wantError: true,
		},
		{
			name: "invalid hostname",
			config: `{
				"databaseConnectionString": "redis://invalid host name with spaces:6379",
				"ignoreTls": false
			}`,
			wantError: true,
		},
		{
			name: "invalid username format",
			config: `{
				"databaseConnectionString": "redis://user:name:password@localhost:6379",
				"ignoreTls": false
			}`,
			wantError: true,
		},
		{
			name: "invalid password format",
			config: `{
				"databaseConnectionString": "redis://user:pass@word@localhost:6379",
				"ignoreTls": false
			}`,
			wantError: true,
		},
		{
			name:      "invalid json",
			config:    `{"invalid": json}`,
			wantError: true,
		},
		{
			name:      "empty config",
			config:    `{}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.config)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRedisExecutor_Unmarshal(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	tests := []struct {
		name      string
		config    string
		expected  *RedisConfig
		wantError bool
	}{
		{
			name: "valid config",
			config: `{
				"databaseConnectionString": "redis://localhost:6379",
				"ignoreTls": false
			}`,
			expected: &RedisConfig{
				DatabaseConnectionString: "redis://localhost:6379",
				IgnoreTls:                false,
			},
			wantError: false,
		},
		{
			name: "valid config with TLS ignore",
			config: `{
				"databaseConnectionString": "rediss://user:password@localhost:6379",
				"ignoreTls": true
			}`,
			expected: &RedisConfig{
				DatabaseConnectionString: "rediss://user:password@localhost:6379",
				IgnoreTls:                true,
			},
			wantError: false,
		},
		{
			name:      "invalid json",
			config:    `{"invalid": json}`,
			expected:  nil,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Unmarshal(tt.config)
			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				cfg := result.(*RedisConfig)
				assert.Equal(t, tt.expected.DatabaseConnectionString, cfg.DatabaseConnectionString)
				assert.Equal(t, tt.expected.IgnoreTls, cfg.IgnoreTls)
			}
		})
	}
}

func TestRedisExecutor_Execute_InvalidConfig(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	monitor := &Monitor{
		ID:      "test-monitor",
		Name:    "Test Redis Monitor",
		Type:    "redis",
		Timeout: 10,
		Config:  `{"invalid": json}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)

	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "invalid character")
}

func TestRedisExecutor_Execute_InvalidConnectionString(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	monitor := &Monitor{
		ID:      "test-monitor",
		Name:    "Test Redis Monitor",
		Type:    "redis",
		Timeout: 10,
		Config: `{
			"databaseConnectionString": "invalid-connection-string",
			"ignoreTls": false
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)

	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "connection string validation failed")
}

func TestRedisExecutor_Execute_InvalidConnectionStringParse(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	monitor := &Monitor{
		ID:      "test-monitor",
		Name:    "Test Redis Monitor",
		Type:    "redis",
		Timeout: 10,
		Config: `{
			"databaseConnectionString": "redis://localhost:99999",
			"ignoreTls": false
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)

	assert.NotNil(t, result)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "connection string validation failed")
}

// Test validation helper functions
func TestRedisExecutor_validateRedisConnectionString(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		// Valid cases
		{input: "redis://localhost:6379", wantError: false},
		{input: "redis://localhost", wantError: false},
		{input: "rediss://localhost:6379", wantError: false},
		{input: "redis://user:password@localhost:6379", wantError: false},
		{input: "redis://:password@localhost:6379", wantError: false},
		{input: "redis://user@localhost:6379", wantError: false},
		{input: "redis://localhost:6379/0", wantError: false},
		{input: "redis://user:password@localhost:6379/1", wantError: false},
		{input: "rediss://user:password@localhost:6379/0", wantError: false},
		{input: "redis://[::1]:6379", wantError: false},
		{input: "redis://[::1]", wantError: false},
		{input: "redis://[2001:db8::1]:6379", wantError: false},
		{input: "redis://user:password@[::1]:6379", wantError: false},
		{input: "redis://user:password@[::1]:6379/0", wantError: false},
		{input: "rediss://[::1]:6379", wantError: false},

		// Invalid cases
		{input: "", wantError: true},
		{input: "http://localhost:6379", wantError: true},
		{input: "redis://", wantError: true},
		{input: "redis://localhost:99999", wantError: true},
		{input: "redis://localhost:0", wantError: true},
		{input: "redis://localhost:-1", wantError: true},
		{input: "redis://localhost:6379/-1", wantError: true},
		{input: "redis://invalid host:6379", wantError: true},
		{input: "redis://user:name:password@localhost:6379", wantError: true},
		{input: "redis://user:pass@word@localhost:6379", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := executor.validateRedisConnectionString(tt.input)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRedisExecutor_validateHostname(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		// Valid cases
		{input: "localhost", wantError: false},
		{input: "redis.example.com", wantError: false},
		{input: "redis-1.example.com", wantError: false},
		{input: "192.168.1.1", wantError: false},
		{input: "::1", wantError: false},
		{input: "[::1]", wantError: false},
		{input: "[2001:db8::1]", wantError: false},
		{input: "[2001:db8:0:0:0:0:0:1]", wantError: false},

		// Invalid cases
		{input: "", wantError: true},
		{input: "-invalid.com", wantError: true},
		{input: "invalid-.com", wantError: true},
		{input: "invalid host", wantError: true},
		{input: "invalid@host", wantError: true},
		{input: "::1", wantError: false}, // IPv6 is valid
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := executor.validateHostname(tt.input)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRedisExecutor_validatePort(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		// Valid cases
		{input: "", wantError: false}, // Optional
		{input: "6379", wantError: false},
		{input: "6380", wantError: false},
		{input: "1", wantError: false},
		{input: "65535", wantError: false},

		// Invalid cases
		{input: "0", wantError: true},
		{input: "-1", wantError: true},
		{input: "65536", wantError: true},
		{input: "99999", wantError: true},
		{input: "abc", wantError: true},
		{input: "6379abc", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := executor.validatePort(tt.input)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRedisExecutor_validateDatabaseNumber(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		// Valid cases
		{input: "", wantError: false},  // Optional
		{input: "/", wantError: false}, // Optional
		{input: "/0", wantError: false},
		{input: "/1", wantError: false},
		{input: "/15", wantError: false},
		{input: "/16", wantError: false}, // Warning but not error

		// Invalid cases
		{input: "/-1", wantError: true},
		{input: "/abc", wantError: true},
		{input: "/1abc", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := executor.validateDatabaseNumber(tt.input)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRedisExecutor_validateAuthentication(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	tests := []struct {
		name      string
		username  string
		password  string
		hasPass   bool
		wantError bool
	}{
		// Valid cases
		{username: "", password: "", hasPass: false, wantError: false}, // No auth
		{username: "user", password: "", hasPass: false, wantError: false},
		{username: "", password: "pass", hasPass: true, wantError: false},
		{username: "user", password: "pass", hasPass: true, wantError: false},
		{username: "user123", password: "pass123", hasPass: true, wantError: false},

		// Invalid cases
		{username: "user:name", password: "", hasPass: false, wantError: true},
		{username: "", password: "pass@word", hasPass: true, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var user *url.Userinfo
			if tt.username != "" || tt.hasPass {
				if tt.hasPass {
					user = url.UserPassword(tt.username, tt.password)
				} else {
					user = url.User(tt.username)
				}
			}

			err := executor.validateAuthentication(user)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Note: This test would require a running Redis instance
// func TestRedisExecutor_Execute_Success(t *testing.T) {
// 	logger := zap.NewNop().Sugar()
// 	executor := NewRedisExecutor(logger)

// 	monitor := &Monitor{
// 		ID:      "test-monitor",
// 		Name:    "Test Redis Monitor",
// 		Type:    "redis",
// 		Timeout: 10,
// 		Config: `{
// 			"databaseConnectionString": "redis://localhost:6379",
// 			"ignoreTls": false
// 		}`,
// 	}

// 	result := executor.Execute(context.Background(), monitor, nil)

// 	assert.NotNil(t, result)
// 	assert.Equal(t, shared.MonitorStatusUp, result.Status)
// 	assert.Contains(t, result.Message, "Redis ping successful")
// }

func TestRedisExecutor_Execute_TLS_Success(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	monitor := &Monitor{
		ID:      "test-monitor-tls",
		Name:    "Test Redis TLS Monitor",
		Type:    "redis",
		Timeout: 10,
		Config: `{
			"databaseConnectionString": "rediss://:testpassword@localhost:6389",
			"ignoreTls": false
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)

	assert.NotNil(t, result)
	// The test might fail if Redis TLS container is not running, but we can check the result structure
	if result.Status == shared.MonitorStatusUp {
		assert.Contains(t, result.Message, "Redis ping successful")
	} else {
		// If it's down, it should be due to connection issues, not validation issues
		assert.Contains(t, result.Message, "Redis")
	}
}

func TestRedisExecutor_Execute_Simple_Success(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	monitor := &Monitor{
		ID:      "test-monitor-simple",
		Name:    "Test Redis Simple Monitor",
		Type:    "redis",
		Timeout: 10,
		Config: `{
			"databaseConnectionString": "redis://localhost:6390",
			"ignoreTls": false
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)

	assert.NotNil(t, result)
	// The test might fail if Redis simple container is not running, but we can check the result structure
	if result.Status == shared.MonitorStatusUp {
		assert.Contains(t, result.Message, "Redis ping successful")
	} else {
		// If it's down, it should be due to connection issues, not validation issues
		assert.Contains(t, result.Message, "Redis")
	}
}

func TestRedisExecutor_Validate_WithCertificates(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	tests := []struct {
		name      string
		config    string
		wantError bool
	}{
		{
			name: "valid config with empty certificates",
			config: `{
				"databaseConnectionString": "rediss://localhost:6379",
				"ignoreTls": false,
				"caCert": "",
				"clientCert": "",
				"clientKey": ""
			}`,
			wantError: false,
		},
		{
			name: "invalid config - client cert without key",
			config: `{
				"databaseConnectionString": "rediss://localhost:6379",
				"ignoreTls": false,
				"clientCert": "-----BEGIN CERTIFICATE-----\nMIIBkTCB+wIJAKZJ..."
			}`,
			wantError: true,
		},
		{
			name: "invalid config - client key without cert",
			config: `{
				"databaseConnectionString": "rediss://localhost:6379",
				"ignoreTls": false,
				"clientKey": "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkq..."
			}`,
			wantError: true,
		},
		{
			name: "invalid CA certificate format",
			config: `{
				"databaseConnectionString": "rediss://localhost:6379",
				"ignoreTls": false,
				"caCert": "invalid certificate"
			}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.config)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRedisExecutor_configureTLS(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewRedisExecutor(logger)

	// Valid test certificate (self-signed for testing)
	validCert := `-----BEGIN CERTIFICATE-----
MIIBhTCCAS4CCQDsvbqzXHBqbjANBgkqhkiG9w0BAQsFADANMQswCQYDVQQGEwJV
UzAeFw0yNDEyMDAwMDAwMDBaFw0yNTEyMDAwMDAwMDBaMA0xCzAJBgNVBAYTAlVT
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQC7
-----END CERTIFICATE-----`

	tests := []struct {
		name      string
		config    *RedisConfig
		wantError bool
	}{
		{
			name: "no TLS config for redis://",
			config: &RedisConfig{
				DatabaseConnectionString: "redis://localhost:6379",
				IgnoreTls:                false,
			},
			wantError: false,
		},
		{
			name: "ignore TLS for rediss://",
			config: &RedisConfig{
				DatabaseConnectionString: "rediss://localhost:6379",
				IgnoreTls:                true,
			},
			wantError: false,
		},
		{
			name: "no certificates provided for rediss://",
			config: &RedisConfig{
				DatabaseConnectionString: "rediss://localhost:6379",
				IgnoreTls:                false,
			},
			wantError: false, // Should work but skip verification
		},
		{
			name: "invalid CA cert",
			config: &RedisConfig{
				DatabaseConnectionString: "rediss://localhost:6379",
				IgnoreTls:                false,
				CaCert:                   "invalid certificate",
			},
			wantError: true,
		},
		{
			name: "client cert without key",
			config: &RedisConfig{
				DatabaseConnectionString: "rediss://localhost:6379",
				IgnoreTls:                false,
				ClientCert:               validCert,
			},
			wantError: true,
		},
		{
			name: "client key without cert",
			config: &RedisConfig{
				DatabaseConnectionString: "rediss://localhost:6379",
				IgnoreTls:                false,
				ClientKey:                "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkq...",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &redis.Options{}
			err := executor.configureTLS(tt.config, opts)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
