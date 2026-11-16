package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The PostgreSQL connection's TLS mode comes from DB_SSLMODE. The default connects without TLS,
// as the hard-coded sslmode=disable did.
func TestLoadConfig_DBSSLModeDefaultsToDisable(t *testing.T) {
	cfg, err := loadTestConfig(t, nil)

	require.NoError(t, err)
	assert.Equal(t, "disable", cfg.DBSSLMode)
}

func TestLoadConfig_DBSSLModeIsSetByTheEnvironment(t *testing.T) {
	for _, mode := range []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"} {
		t.Run(mode, func(t *testing.T) {
			cfg, err := loadTestConfig(t, map[string]string{"DB_SSLMODE": mode})

			require.NoError(t, err)
			assert.Equal(t, mode, cfg.DBSSLMode)
		})
	}
}

// pgdriver can't use a mode it doesn't know, and a mistyped one would hide a setup that doesn't
// encrypt the way its operator thinks it does, so startup stops. pgdriver is case sensitive
// about sslmode, and this check is too.
func TestLoadConfig_AnUnknownDBSSLModeStopsStartup(t *testing.T) {
	for _, mode := range []string{"sometimes", "REQUIRE"} {
		t.Run(mode, func(t *testing.T) {
			_, err := loadTestConfig(t, map[string]string{"DB_SSLMODE": mode})

			require.Error(t, err)
			assert.ErrorContains(t, err, "DBSSLMode must be one of: disable allow prefer require verify-ca verify-full")
		})
	}
}

// "bun db migrate" reads the same variable: it loads only the database settings
func TestLoadConfig_DBSSLModeInTheDatabaseSettings(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("DB_SSLMODE", "verify-full")

	cfg, err := LoadConfig[DBConfig](t.TempDir())

	require.NoError(t, err)
	assert.Equal(t, "verify-full", cfg.DBSSLMode)
}
