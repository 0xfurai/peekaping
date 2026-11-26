package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAndValidate_CORSOriginsAreUnsetByDefault(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Empty(t, cfg.ToInternalConfig().CORSOrigins)
}

func TestLoadAndValidate_KeepsCORSOriginsFromTheEnvironment(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("CORS_ORIGINS", "https://peek.example,http://localhost:8383")

	cfg, err := LoadAndValidate(t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, "https://peek.example,http://localhost:8383", cfg.ToInternalConfig().CORSOrigins)
}

func TestLoadAndValidate_RefusesAnInvalidCORSOrigin(t *testing.T) {
	for name, tc := range map[string]struct{ origins, clientURL, wantErr string }{
		"an entry that is no origin":      {"https://peek.example,example.com", "", `invalid CORS_ORIGINS entry "example.com"`},
		"an entry with a path":            {"http://peek.example/app", "", `invalid CORS_ORIGINS entry "http://peek.example/app"`},
		"a star next to other entries":    {"*,https://peek.example", "", `invalid CORS_ORIGINS entry "*"`},
		"a CLIENT_URL that has no origin": {"", "ftp://peek.example", `invalid CLIENT_URL "ftp://peek.example"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DB_TYPE", "sqlite")
			t.Setenv("DB_NAME", "peekaping.db")
			t.Setenv("CORS_ORIGINS", tc.origins)
			if tc.clientURL != "" {
				t.Setenv("CLIENT_URL", tc.clientURL)
			}

			_, err := LoadAndValidate(t.TempDir())

			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestLoadAndValidate_AcceptsAStarAlone(t *testing.T) {
	t.Setenv("DB_TYPE", "sqlite")
	t.Setenv("DB_NAME", "peekaping.db")
	t.Setenv("CORS_ORIGINS", "*")

	_, err := LoadAndValidate(t.TempDir())

	require.NoError(t, err)
}
