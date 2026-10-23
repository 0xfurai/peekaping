package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresSSLMode(t *testing.T) {
	for _, mode := range []string{"", "require", "verify-full", "invalid"} {
		t.Run("mode="+mode, func(t *testing.T) {
			t.Setenv("DB_TYPE", "postgres")
			t.Setenv("DB_HOST", "localhost")
			t.Setenv("DB_PORT", "5432")
			t.Setenv("DB_USER", "peekaping")
			t.Setenv("DB_PASS", "password")
			t.Setenv("DB_NAME", "peekaping")
			t.Setenv("DB_SSLMODE", mode)

			cfg, err := LoadAndValidate(t.TempDir())
			if mode == "invalid" {
				require.ErrorContains(t, err, "DBSSLMode")
				return
			}
			require.NoError(t, err)
			if mode == "" {
				mode = "disable"
			}
			assert.Equal(t, mode, cfg.ToInternalConfig().DBSSLMode)
		})
	}
}
