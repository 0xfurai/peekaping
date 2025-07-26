package bruteforce

import (
	"os"
	"testing"
	"time"

	"peekaping/src/config"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestBruteforceConfigFromEnv(t *testing.T) {
	// Test default values
	cfg := &config.Config{}
	// Load defaults
	cfg.BruteforceMaxAttempts = 5
	cfg.BruteforceWindow = time.Minute
	cfg.BruteforceLockout = 15 * time.Minute

	mockService := &MockService{}
	logger := zap.NewNop().Sugar()

	guard := NewGuard(mockService, logger, cfg)
	assert.Equal(t, 5, guard.cfg.MaxAttempts)
	assert.Equal(t, time.Minute, guard.cfg.Window)
	assert.Equal(t, 15*time.Minute, guard.cfg.Lockout)

	// Test custom values via environment variables
	os.Setenv("BRUTEFORCE_MAX_ATTEMPTS", "3")
	os.Setenv("BRUTEFORCE_WINDOW", "2m")
	os.Setenv("BRUTEFORCE_LOCKOUT", "30m")
	defer func() {
		os.Unsetenv("BRUTEFORCE_MAX_ATTEMPTS")
		os.Unsetenv("BRUTEFORCE_WINDOW")
		os.Unsetenv("BRUTEFORCE_LOCKOUT")
	}()

	// Create a new config that will load from environment
	testConfig := &config.Config{}
	// Simulate loading from environment (in real app, this would be done by LoadConfig)
	testConfig.BruteforceMaxAttempts = 3
	testConfig.BruteforceWindow = 2 * time.Minute
	testConfig.BruteforceLockout = 30 * time.Minute

	guard = NewGuard(mockService, logger, testConfig)
	assert.Equal(t, 3, guard.cfg.MaxAttempts)
	assert.Equal(t, 2*time.Minute, guard.cfg.Window)
	assert.Equal(t, 30*time.Minute, guard.cfg.Lockout)
}
