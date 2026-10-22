package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"peekaping/internal/modules/shared"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// MockSettingService is a mock implementation of SettingService for testing
type MockSettingService struct {
	mock.Mock
}

func (m *MockSettingService) GetByKey(ctx context.Context, key string) (*shared.SettingModel, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.SettingModel), args.Error(1)
}

func (m *MockSettingService) SetByKey(ctx context.Context, key string, entity *shared.SettingModel) (*shared.SettingModel, error) {
	args := m.Called(ctx, key, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.SettingModel), args.Error(1)
}

func (m *MockSettingService) CreateIfAbsent(ctx context.Context, key string, entity *shared.SettingModel) (*shared.SettingModel, error) {
	args := m.Called(ctx, key, entity)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*shared.SettingModel), args.Error(1)
}

func (m *MockSettingService) DeleteByKey(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

func (m *MockSettingService) InitializeSettings(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestNewTokenMaker(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()

	tokenMaker := NewTokenMaker(mockSettingService, logger)

	assert.NotNil(t, tokenMaker)
	assert.Equal(t, mockSettingService, tokenMaker.settingService)
	assert.NotNil(t, tokenMaker.logger)
}

func TestTokenMaker_CreateAccessToken_Success(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the settings
	mockSettingService.On("GetByKey", ctx, "ACCESS_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "ACCESS_TOKEN_EXPIRED_IN",
		Value: "15m",
	}, nil)
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "test-secret-key",
	}, nil)

	token, err := tokenMaker.CreateAccessToken(ctx, user)

	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// Verify the token can be parsed and contains correct claims
	claims := &Claims{}
	parsedToken, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte("test-secret-key"), nil
	})

	assert.NoError(t, err)
	assert.True(t, parsedToken.Valid)
	assert.Equal(t, "user123", claims.UserID)
	assert.Equal(t, "test@example.com", claims.Email)
	assert.Equal(t, "access", claims.Type)

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_CreateAccessToken_ExpirySettingNotFound(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the expiry setting not found
	mockSettingService.On("GetByKey", ctx, "ACCESS_TOKEN_EXPIRED_IN").Return(nil, nil)

	token, err := tokenMaker.CreateAccessToken(ctx, user)

	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "access token expiration setting not found")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_CreateAccessToken_ExpirySettingError(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the expiry setting error
	mockSettingService.On("GetByKey", ctx, "ACCESS_TOKEN_EXPIRED_IN").Return(nil, errors.New("database error"))

	token, err := tokenMaker.CreateAccessToken(ctx, user)

	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "failed to get access token expiration")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_CreateAccessToken_InvalidExpiryFormat(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the settings with invalid expiry format
	mockSettingService.On("GetByKey", ctx, "ACCESS_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "ACCESS_TOKEN_EXPIRED_IN",
		Value: "invalid-duration",
	}, nil)

	token, err := tokenMaker.CreateAccessToken(ctx, user)

	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "invalid access token expiration format")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_CreateAccessToken_SecretKeyNotFound(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the expiry setting
	mockSettingService.On("GetByKey", ctx, "ACCESS_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "ACCESS_TOKEN_EXPIRED_IN",
		Value: "15m",
	}, nil)
	// Mock the secret key setting not found
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(nil, nil)

	token, err := tokenMaker.CreateAccessToken(ctx, user)

	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "access token secret key setting not found")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_CreateAccessToken_SecretKeyError(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the expiry setting
	mockSettingService.On("GetByKey", ctx, "ACCESS_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "ACCESS_TOKEN_EXPIRED_IN",
		Value: "15m",
	}, nil)
	// Mock the secret key setting error
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(nil, errors.New("database error"))

	token, err := tokenMaker.CreateAccessToken(ctx, user)

	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "failed to get access token secret key")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_CreateRefreshToken_Success(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the settings
	mockSettingService.On("GetByKey", ctx, "REFRESH_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "REFRESH_TOKEN_EXPIRED_IN",
		Value: "168h", // 7 days in hours
	}, nil)
	mockSettingService.On("GetByKey", ctx, shared.RefreshTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.RefreshTokenSecretKey,
		Value: "refresh-secret-key",
	}, nil)

	token, err := tokenMaker.CreateRefreshToken(ctx, user)

	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// Verify the token can be parsed and contains correct claims
	claims := &Claims{}
	parsedToken, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte("refresh-secret-key"), nil
	})

	assert.NoError(t, err)
	assert.True(t, parsedToken.Valid)
	assert.Equal(t, "user123", claims.UserID)
	assert.Equal(t, "test@example.com", claims.Email)
	assert.Equal(t, "refresh", claims.Type)

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_CreateRefreshToken_ExpirySettingNotFound(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the expiry setting not found
	mockSettingService.On("GetByKey", ctx, "REFRESH_TOKEN_EXPIRED_IN").Return(nil, nil)

	token, err := tokenMaker.CreateRefreshToken(ctx, user)

	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "refresh token expiration setting not found")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_CreateRefreshToken_InvalidExpiryFormat(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the settings with invalid expiry format
	mockSettingService.On("GetByKey", ctx, "REFRESH_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "REFRESH_TOKEN_EXPIRED_IN",
		Value: "invalid-duration",
	}, nil)

	token, err := tokenMaker.CreateRefreshToken(ctx, user)

	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "invalid refresh token expiration format")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_CreateRefreshToken_SecretKeyNotFound(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the expiry setting
	mockSettingService.On("GetByKey", ctx, "REFRESH_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "REFRESH_TOKEN_EXPIRED_IN",
		Value: "168h",
	}, nil)
	// Mock the secret key setting not found
	mockSettingService.On("GetByKey", ctx, shared.RefreshTokenSecretKey).Return(nil, nil)

	token, err := tokenMaker.CreateRefreshToken(ctx, user)

	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "refresh token secret key setting not found")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_CreateRefreshToken_SecretKeyError(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Mock the expiry setting
	mockSettingService.On("GetByKey", ctx, "REFRESH_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "REFRESH_TOKEN_EXPIRED_IN",
		Value: "168h",
	}, nil)
	// Mock the secret key setting error
	mockSettingService.On("GetByKey", ctx, shared.RefreshTokenSecretKey).Return(nil, errors.New("database error"))

	token, err := tokenMaker.CreateRefreshToken(ctx, user)

	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "failed to get refresh token secret key")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_VerifyToken_AccessTokenSuccess(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Create a valid token first
	mockSettingService.On("GetByKey", ctx, "ACCESS_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "ACCESS_TOKEN_EXPIRED_IN",
		Value: "15m",
	}, nil)
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "test-secret-key",
	}, nil)

	token, err := tokenMaker.CreateAccessToken(ctx, user)
	assert.NoError(t, err)

	// Reset mock expectations for verification
	mockSettingService.ExpectedCalls = nil
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "test-secret-key",
	}, nil)

	// Verify the token
	claims, err := tokenMaker.VerifyToken(ctx, token, "access")

	assert.NoError(t, err)
	assert.NotNil(t, claims)
	assert.Equal(t, "user123", claims.UserID)
	assert.Equal(t, "test@example.com", claims.Email)
	assert.Equal(t, "access", claims.Type)

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_VerifyToken_RefreshTokenSuccess(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Create a valid refresh token first
	mockSettingService.On("GetByKey", ctx, "REFRESH_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "REFRESH_TOKEN_EXPIRED_IN",
		Value: "168h", // 7 days in hours
	}, nil)
	mockSettingService.On("GetByKey", ctx, shared.RefreshTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.RefreshTokenSecretKey,
		Value: "refresh-secret-key",
	}, nil)

	token, err := tokenMaker.CreateRefreshToken(ctx, user)
	assert.NoError(t, err)

	// Reset mock expectations for verification
	mockSettingService.ExpectedCalls = nil
	mockSettingService.On("GetByKey", ctx, shared.RefreshTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.RefreshTokenSecretKey,
		Value: "refresh-secret-key",
	}, nil)

	// Verify the token
	claims, err := tokenMaker.VerifyToken(ctx, token, "refresh")

	assert.NoError(t, err)
	assert.NotNil(t, claims)
	assert.Equal(t, "user123", claims.UserID)
	assert.Equal(t, "test@example.com", claims.Email)
	assert.Equal(t, "refresh", claims.Type)

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_VerifyToken_InvalidTokenType(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	token := "some-token"

	claims, err := tokenMaker.VerifyToken(ctx, token, "invalid-type")

	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Equal(t, ErrInvalidToken, err)
}

func TestTokenMaker_VerifyToken_SecretKeyNotFound(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	token := "some-token"

	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(nil, nil)

	claims, err := tokenMaker.VerifyToken(ctx, token, "access")

	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "access token secret key setting not found")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_VerifyToken_SecretKeyError(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	token := "some-token"

	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(nil, errors.New("database error"))

	claims, err := tokenMaker.VerifyToken(ctx, token, "access")

	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "failed to get access token secret key")

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_VerifyToken_InvalidTokenString(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	invalidToken := "invalid.token.string"

	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "test-secret-key",
	}, nil)

	claims, err := tokenMaker.VerifyToken(ctx, invalidToken, "access")

	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Equal(t, ErrInvalidToken, err)

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_VerifyToken_ExpiredToken(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Create an expired token
	claims := &Claims{
		UserID: user.ID,
		Email:  user.Email,
		Type:   "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(-1 * time.Hour)), // Expired 1 hour ago
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC().Add(-2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().UTC().Add(-2 * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("test-secret-key"))
	assert.NoError(t, err)

	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "test-secret-key",
	}, nil)

	verifiedClaims, err := tokenMaker.VerifyToken(ctx, tokenString, "access")

	assert.Error(t, err)
	assert.Nil(t, verifiedClaims)
	assert.Equal(t, ErrExpiredToken, err)

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_VerifyToken_WrongSecretKey(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Create a token with one secret key
	claims := &Claims{
		UserID: user.ID,
		Email:  user.Email,
		Type:   "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			NotBefore: jwt.NewNumericDate(time.Now().UTC()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("wrong-secret-key"))
	assert.NoError(t, err)

	// Try to verify with different secret key
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "correct-secret-key",
	}, nil)

	verifiedClaims, err := tokenMaker.VerifyToken(ctx, tokenString, "access")

	assert.Error(t, err)
	assert.Nil(t, verifiedClaims)
	assert.Equal(t, ErrInvalidToken, err)

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_VerifyToken_WrongTokenType(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Create an access token
	mockSettingService.On("GetByKey", ctx, "ACCESS_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "ACCESS_TOKEN_EXPIRED_IN",
		Value: "15m",
	}, nil)
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "test-secret-key",
	}, nil)

	accessToken, err := tokenMaker.CreateAccessToken(ctx, user)
	assert.NoError(t, err)

	// Reset mock expectations for verification
	mockSettingService.ExpectedCalls = nil
	mockSettingService.On("GetByKey", ctx, shared.RefreshTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.RefreshTokenSecretKey,
		Value: "refresh-secret-key",
	}, nil)

	// Try to verify access token as refresh token
	claims, err := tokenMaker.VerifyToken(ctx, accessToken, "refresh")

	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Equal(t, ErrInvalidToken, err)

	mockSettingService.AssertExpectations(t)
}

func TestTokenMaker_VerifyToken_InvalidSigningMethod(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "test-secret-key",
	}, nil)

	claims := &Claims{
		UserID: "user123",
		Email:  "test@example.com",
		Type:   "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			NotBefore: jwt.NewNumericDate(time.Now().UTC()),
		},
	}

	// The claims are valid: signed with HS256 and the secret, they verify
	hs256, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret-key"))
	require.NoError(t, err)
	_, err = tokenMaker.VerifyToken(ctx, hs256, "access")
	require.NoError(t, err)

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	rs256, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(rsaKey)
	require.NoError(t, err)
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	for name, tokenString := range map[string]string{"RS256": rs256, "none": unsigned} {
		t.Run(name, func(t *testing.T) {
			verifiedClaims, err := tokenMaker.VerifyToken(ctx, tokenString, "access")

			assert.Equal(t, ErrInvalidToken, err)
			assert.Nil(t, verifiedClaims)
		})
	}

	mockSettingService.AssertExpectations(t)
}

// Only HS256 is accepted: a token signed with another HMAC algorithm and the
// right secret is rejected, as an access token and as a refresh token.
func TestTokenMaker_VerifyToken_OnlyHS256(t *testing.T) {
	ctx := context.Background()
	secrets := map[string]string{
		"access":  "test-access-secret",
		"refresh": "test-refresh-secret",
	}
	mockSettingService := &MockSettingService{}
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: secrets["access"],
	}, nil)
	mockSettingService.On("GetByKey", ctx, shared.RefreshTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.RefreshTokenSecretKey,
		Value: secrets["refresh"],
	}, nil)
	tokenMaker := NewTokenMaker(mockSettingService, zap.NewNop().Sugar())

	for tokenType, secret := range secrets {
		claims := &Claims{
			UserID: "user123",
			Email:  "test@example.com",
			Type:   tokenType,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
				NotBefore: jwt.NewNumericDate(time.Now().UTC()),
			},
		}
		sign := func(method jwt.SigningMethod) string {
			signed, err := jwt.NewWithClaims(method, claims).SignedString([]byte(secret))
			require.NoError(t, err)
			return signed
		}

		t.Run(tokenType+" HS256", func(t *testing.T) {
			verified, err := tokenMaker.VerifyToken(ctx, sign(jwt.SigningMethodHS256), tokenType)
			require.NoError(t, err)
			assert.Equal(t, "user123", verified.UserID)
		})
		for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
			t.Run(tokenType+" "+method.Alg(), func(t *testing.T) {
				verified, err := tokenMaker.VerifyToken(ctx, sign(method), tokenType)
				assert.Equal(t, ErrInvalidToken, err)
				assert.Nil(t, verified)
			})
		}
	}
}

// Verifying a token doesn't read the signing secret from the database each
// time: many verifies read it once.
func TestTokenMaker_VerifyToken_ReadsSecretOnce(t *testing.T) {
	ctx := context.Background()
	mockSettingService := &MockSettingService{}
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "test-secret-key",
	}, nil)
	tokenMaker := NewTokenMaker(mockSettingService, zap.NewNop().Sugar())

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		UserID: "user123",
		Type:   "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
		},
	}).SignedString([]byte("test-secret-key"))
	require.NoError(t, err)

	for range 50 {
		claims, err := tokenMaker.VerifyToken(ctx, token, "access")
		require.NoError(t, err)
		assert.Equal(t, "user123", claims.UserID)
	}

	mockSettingService.AssertNumberOfCalls(t, "GetByKey", 1)
}

// A secret that changes in the database is used within a minute: the cached one
// is read again once it is a minute old.
func TestTokenMaker_SecretIsReadAgainAfterAMinute(t *testing.T) {
	ctx := context.Background()
	mockSettingService := &MockSettingService{}
	secretSetting := func(value string) *shared.SettingModel {
		return &shared.SettingModel{Key: shared.AccessTokenSecretKey, Value: value}
	}
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(secretSetting("old-secret"), nil).Once()
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(secretSetting("new-secret"), nil)
	tokenMaker := NewTokenMaker(mockSettingService, zap.NewNop().Sugar())
	now := time.Now()
	tokenMaker.clock = func() time.Time { return now }

	signedWith := func(secret string) string {
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
			UserID: "user123",
			Type:   "access",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
			},
		}).SignedString([]byte(secret))
		require.NoError(t, err)
		return token
	}
	oldToken, newToken := signedWith("old-secret"), signedWith("new-secret")

	_, err := tokenMaker.VerifyToken(ctx, oldToken, "access")
	require.NoError(t, err)

	// The database has the new secret now, but the old one is under a minute old
	now = now.Add(59 * time.Second)
	_, err = tokenMaker.VerifyToken(ctx, oldToken, "access")
	require.NoError(t, err)
	_, err = tokenMaker.VerifyToken(ctx, newToken, "access")
	require.Error(t, err)
	mockSettingService.AssertNumberOfCalls(t, "GetByKey", 1)

	now = now.Add(2 * time.Second)
	_, err = tokenMaker.VerifyToken(ctx, newToken, "access")
	require.NoError(t, err)
	_, err = tokenMaker.VerifyToken(ctx, oldToken, "access")
	require.Error(t, err)
	mockSettingService.AssertNumberOfCalls(t, "GetByKey", 2)

	// Creating a token uses the cached secret too
	mockSettingService.On("GetByKey", ctx, "ACCESS_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{Key: "ACCESS_TOKEN_EXPIRED_IN", Value: "15m"}, nil)
	created, err := tokenMaker.CreateAccessToken(ctx, &Model{ID: "user123", Email: "test@example.com"})
	require.NoError(t, err)
	_, err = tokenMaker.VerifyToken(ctx, created, "access")
	require.NoError(t, err)
	mockSettingService.AssertNumberOfCalls(t, "GetByKey", 3) // the expiry only
}

func TestTokenMaker_CreateToken_EdgeCases(t *testing.T) {
	mockSettingService := &MockSettingService{}
	logger := zap.NewNop().Sugar()
	tokenMaker := NewTokenMaker(mockSettingService, logger)

	ctx := context.Background()
	user := &Model{
		ID:    "user123",
		Email: "test@example.com",
	}

	// Test with very short duration
	mockSettingService.On("GetByKey", ctx, "ACCESS_TOKEN_EXPIRED_IN").Return(&shared.SettingModel{
		Key:   "ACCESS_TOKEN_EXPIRED_IN",
		Value: "5s",
	}, nil)
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "test-secret-key",
	}, nil)

	token, err := tokenMaker.CreateAccessToken(ctx, user)
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// Verify the token can be parsed immediately
	mockSettingService.ExpectedCalls = nil
	mockSettingService.On("GetByKey", ctx, shared.AccessTokenSecretKey).Return(&shared.SettingModel{
		Key:   shared.AccessTokenSecretKey,
		Value: "test-secret-key",
	}, nil)

	claims, err := tokenMaker.VerifyToken(ctx, token, "access")
	assert.NoError(t, err)
	assert.NotNil(t, claims)

	// Test that the token has the correct expiration time
	assert.True(t, claims.ExpiresAt.Time.After(time.Now().UTC()))
	assert.True(t, claims.ExpiresAt.Time.Before(time.Now().UTC().Add(6*time.Second)))

	mockSettingService.AssertExpectations(t)
}
