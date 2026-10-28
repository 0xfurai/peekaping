package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresSSLMode(t *testing.T) {
	for name, load := range map[string]func(string) (string, error){
		"application": func(path string) (string, error) {
			cfg, err := LoadConfig[Config](path)
			return cfg.DBSSLMode, err
		},
		"migrations": func(path string) (string, error) {
			cfg, err := LoadConfig[DBConfig](path)
			return cfg.DBSSLMode, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, mode := range []string{"", "disable", "allow", "prefer", "require", "verify-ca", "verify-full", "invalid"} {
				t.Run("mode="+mode, func(t *testing.T) {
					t.Setenv("DB_TYPE", "postgres")
					t.Setenv("DB_HOST", "localhost")
					t.Setenv("DB_PORT", "5432")
					t.Setenv("DB_USER", "peekaping")
					t.Setenv("DB_PASS", "password")
					t.Setenv("DB_NAME", "peekaping")
					t.Setenv("DB_SSLMODE", mode)

					got, err := load(t.TempDir())
					if mode == "invalid" {
						require.ErrorContains(t, err, "DBSSLMode")
						return
					}
					require.NoError(t, err)
					if mode == "" {
						mode = "disable"
					}
					assert.Equal(t, mode, got)
				})
			}
		})
	}
}
