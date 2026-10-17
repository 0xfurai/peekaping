package setting_test

import (
	"context"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/setting"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var testUser = &auth.Model{ID: "user-1", Email: "admin@example.com"}

func newSettingsWithTokens() (setting.Service, *auth.TokenMaker) {
	logger := zap.NewNop().Sugar()
	settings := setting.NewService(newMemoryRepository(), logger)
	return settings, auth.NewTokenMaker(settings, logger)
}

// seedEarlierRelease stores the settings of an install from a release whose
// settings API still served the JWT signing secrets.
func seedEarlierRelease(t *testing.T, settings setting.Service) {
	t.Helper()
	for key, value := range map[string]string{
		"ACCESS_TOKEN_EXPIRED_IN":  "15m",
		"REFRESH_TOKEN_EXPIRED_IN": "720h",
		"ACCESS_TOKEN_SECRET_KEY":  "possibly-leaked-access-secret",
		"REFRESH_TOKEN_SECRET_KEY": "possibly-leaked-refresh-secret",
	} {
		_, err := settings.SetByKey(context.Background(), key, &setting.Model{Value: value, Type: "string"})
		require.NoError(t, err)
	}
}

func TestInitializeSettings_UpgradeInvalidatesTokensSignedWithOldSecrets(t *testing.T) {
	ctx := context.Background()
	settings, tokenMaker := newSettingsWithTokens()
	seedEarlierRelease(t, settings)
	accessToken, err := tokenMaker.CreateAccessToken(ctx, testUser)
	require.NoError(t, err)
	refreshToken, err := tokenMaker.CreateRefreshToken(ctx, testUser)
	require.NoError(t, err)

	require.NoError(t, settings.InitializeSettings(ctx))

	_, err = tokenMaker.VerifyToken(ctx, accessToken, "access")
	assert.Error(t, err)
	_, err = tokenMaker.VerifyToken(ctx, refreshToken, "refresh")
	assert.Error(t, err)
}

func TestInitializeSettings_RestartKeepsTokensValid(t *testing.T) {
	installs := []struct {
		name string
		seed func(*testing.T, setting.Service)
	}{
		{"fresh install", func(*testing.T, setting.Service) {}},
		{"upgraded install", seedEarlierRelease},
	}
	for _, install := range installs {
		t.Run(install.name, func(t *testing.T) {
			ctx := context.Background()
			settings, tokenMaker := newSettingsWithTokens()
			install.seed(t, settings)
			require.NoError(t, settings.InitializeSettings(ctx))
			accessToken, err := tokenMaker.CreateAccessToken(ctx, testUser)
			require.NoError(t, err)
			refreshToken, err := tokenMaker.CreateRefreshToken(ctx, testUser)
			require.NoError(t, err)

			require.NoError(t, settings.InitializeSettings(ctx))

			_, err = tokenMaker.VerifyToken(ctx, accessToken, "access")
			assert.NoError(t, err)
			_, err = tokenMaker.VerifyToken(ctx, refreshToken, "refresh")
			assert.NoError(t, err)
		})
	}
}
