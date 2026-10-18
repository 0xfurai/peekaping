package setting_test

import (
	"context"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/setting"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	leakedAccessSecret  = "possibly-leaked-access-secret"
	leakedRefreshSecret = "possibly-leaked-refresh-secret"
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
		"ACCESS_TOKEN_SECRET_KEY":  leakedAccessSecret,
		"REFRESH_TOKEN_SECRET_KEY": leakedRefreshSecret,
	} {
		_, err := settings.SetByKey(context.Background(), key, &setting.Model{Value: value, Type: "string"})
		require.NoError(t, err)
	}
}

// forgeToken signs a token for testUser the way TokenMaker does, with secret.
func forgeToken(t *testing.T, tokenType, secret string) string {
	t.Helper()
	claims := &auth.Claims{
		UserID: testUser.ID,
		Email:  testUser.Email,
		Type:   tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return token
}

func TestInitializeSettings_UpgradeInvalidatesTokensSignedWithOldSecrets(t *testing.T) {
	ctx := context.Background()
	settings, tokenMaker := newSettingsWithTokens()
	seedEarlierRelease(t, settings)

	require.NoError(t, settings.InitializeSettings(ctx))

	_, err := tokenMaker.VerifyToken(ctx, forgeToken(t, "access", leakedAccessSecret), "access")
	assert.Error(t, err)
	_, err = tokenMaker.VerifyToken(ctx, forgeToken(t, "refresh", leakedRefreshSecret), "refresh")
	assert.Error(t, err)
}

// During a rolling upgrade, pods from the earlier release keep serving its
// settings API, which returns any key matching ^[A-Z0-9_]+$.
func TestInitializeSettings_SecretsServedByEarlierReleaseDontSignTokens(t *testing.T) {
	ctx := context.Background()
	settings, tokenMaker := newSettingsWithTokens()
	seedEarlierRelease(t, settings)

	require.NoError(t, settings.InitializeSettings(ctx))

	for tokenType, key := range map[string]string{
		"access":  "ACCESS_TOKEN_SECRET_KEY",
		"refresh": "REFRESH_TOKEN_SECRET_KEY",
	} {
		served, err := settings.GetByKey(ctx, key)
		require.NoError(t, err)
		require.NotNil(t, served)
		_, err = tokenMaker.VerifyToken(ctx, forgeToken(t, tokenType, served.Value), tokenType)
		assert.Error(t, err, key)
	}
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
