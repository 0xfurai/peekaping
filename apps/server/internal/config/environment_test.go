package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_RemembersTheVariablesTheSystemEnvironmentSet(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"SIGNUP_MODE": "invite"})

	require.NoError(t, err)
	assert.True(t, cfg.FromEnvironment("SIGNUP_MODE"))
	assert.True(t, cfg.FromEnvironment("DB_TYPE"))
	// A default is not the environment's, though the field holds a value
	assert.Equal(t, "http://localhost:3000", cfg.ClientURL)
	assert.False(t, cfg.FromEnvironment("CLIENT_URL"))
	assert.False(t, cfg.FromEnvironment("ACCOUNT_SMTP_PORT"))
}

func TestLoadConfig_RemembersTheVariablesTheEnvFileSet(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("CLIENT_URL=https://peek.example\nACCOUNT_SMTP_USER=\n"), 0o600))
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadConfig[Config](dir)

	require.NoError(t, err)
	assert.True(t, cfg.FromEnvironment("CLIENT_URL"))
	// A variable set to nothing counts as unset, as it does for the value
	assert.False(t, cfg.FromEnvironment("ACCOUNT_SMTP_USER"))
}

func TestEnvironment_SetFromEnvironmentMarksNames(t *testing.T) {
	var cfg Config
	assert.False(t, cfg.FromEnvironment("SIGNUP_MODE"))

	cfg.SetFromEnvironment("SIGNUP_MODE")

	assert.True(t, cfg.FromEnvironment("SIGNUP_MODE"))
	assert.False(t, cfg.FromEnvironment("CLIENT_URL"))
}
