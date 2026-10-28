package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"peekaping/internal/modules/shared"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token has expired")
)

type Claims struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
	Type   string `json:"type"` // "access" or "refresh"
	// PasswordFingerprint is set on refresh tokens: it identifies the password the
	// user had when the token was issued, so a password change ends older ones.
	PasswordFingerprint string `json:"pwf,omitempty"`
	jwt.RegisteredClaims
}

// passwordFingerprint identifies a user's password without revealing it: the
// first 16 bytes of the SHA-256 of the stored password hash, in hex.
func passwordFingerprint(passwordHash string) string {
	sum := sha256.Sum256([]byte(passwordHash))
	return hex.EncodeToString(sum[:16])
}

func init() {
	// A token carries the time it was issued (iat) to the millisecond, not to the second. A password
	// reset ends the sessions issued before it, and a user who signs in again right after it must
	// get a session that works: with whole seconds, a token issued before the reset and one issued
	// after it could not be told apart within the second it happened in.
	jwt.TimePrecision = time.Millisecond
}

// SessionCutoffs says when a user's earlier sessions were ended
type SessionCutoffs interface {
	// TokensValidAfter returns the moment before which issued tokens no longer work, nil for none
	// and for a user that doesn't exist
	TokensValidAfter(ctx context.Context, userID string) (*time.Time, error)
}

// secretTTL is how long a signing secret stays in memory before it is read from
// the settings again. So a secret that changes in the database, by this process
// or by another one in cluster mode, is used within this time.
const secretTTL = time.Minute

type cachedSecret struct {
	value     string
	fetchedAt time.Time
}

type TokenMaker struct {
	settingService shared.SettingService
	logger         *zap.SugaredLogger
	clock          func() time.Time

	// cutoffs ends the sessions issued before a user's tokens_valid_after. Nil checks nothing.
	cutoffs SessionCutoffs

	mu      sync.Mutex
	secrets map[string]cachedSecret
}

// WithSessionCutoffs makes VerifyToken refuse a token issued before its user's cutoff (a password
// reset ends every session). It reads the cutoff from the database on every verification, one
// primary-key lookup, and does not cache it: a reset takes effect on every API replica at once,
// and a cache would keep an ended session alive for as long as it lasts. It returns the maker.
func (maker *TokenMaker) WithSessionCutoffs(cutoffs SessionCutoffs) *TokenMaker {
	maker.cutoffs = cutoffs
	return maker
}

func NewTokenMaker(settingService shared.SettingService, logger *zap.SugaredLogger) *TokenMaker {
	return &TokenMaker{
		settingService: settingService,
		logger:         logger.Named("[token-maker]"),
		clock:          time.Now,
		secrets:        map[string]cachedSecret{},
	}
}

// secret returns the signing secret stored under key, read from the settings at
// most once per secretTTL. name is how errors call it.
func (maker *TokenMaker) secret(ctx context.Context, key, name string) (string, error) {
	now := maker.clock()
	maker.mu.Lock()
	cached, ok := maker.secrets[key]
	maker.mu.Unlock()
	if ok && now.Sub(cached.fetchedAt) < secretTTL {
		return cached.value, nil
	}

	setting, err := maker.settingService.GetByKey(ctx, key)
	if err != nil {
		return "", fmt.Errorf("failed to get %s: %w", name, err)
	}
	if setting == nil {
		return "", fmt.Errorf("%s setting not found", name)
	}

	// Concurrent requests may each read the secret; keep the newest read
	maker.mu.Lock()
	if current, ok := maker.secrets[key]; !ok || !current.fetchedAt.After(now) {
		maker.secrets[key] = cachedSecret{value: setting.Value, fetchedAt: now}
	}
	maker.mu.Unlock()
	return setting.Value, nil
}

func (maker *TokenMaker) accessSecret(ctx context.Context) (string, error) {
	return maker.secret(ctx, shared.AccessTokenSecretKey, "access token secret key")
}

func (maker *TokenMaker) refreshSecret(ctx context.Context) (string, error) {
	return maker.secret(ctx, shared.RefreshTokenSecretKey, "refresh token secret key")
}

func (maker *TokenMaker) CreateAccessToken(ctx context.Context, user *Model) (string, error) {
	// Get access token expiration
	expirySetting, err := maker.settingService.GetByKey(ctx, "ACCESS_TOKEN_EXPIRED_IN")
	if err != nil {
		return "", fmt.Errorf("failed to get access token expiration: %w", err)
	}
	if expirySetting == nil {
		return "", fmt.Errorf("access token expiration setting not found")
	}

	accessExpiry, err := time.ParseDuration(expirySetting.Value)
	if err != nil {
		return "", fmt.Errorf("invalid access token expiration format: %w", err)
	}

	// Get access token secret key
	secretKey, err := maker.accessSecret(ctx)
	if err != nil {
		return "", err
	}

	return maker.createToken(user, "access", accessExpiry, secretKey)
}

func (maker *TokenMaker) CreateRefreshToken(ctx context.Context, user *Model) (string, error) {
	// Get refresh token expiration
	expirySetting, err := maker.settingService.GetByKey(ctx, "REFRESH_TOKEN_EXPIRED_IN")
	if err != nil {
		return "", fmt.Errorf("failed to get refresh token expiration: %w", err)
	}
	if expirySetting == nil {
		return "", fmt.Errorf("refresh token expiration setting not found")
	}

	refreshExpiry, err := time.ParseDuration(expirySetting.Value)
	if err != nil {
		return "", fmt.Errorf("invalid refresh token expiration format: %w", err)
	}

	// Get refresh token secret key
	secretKey, err := maker.refreshSecret(ctx)
	if err != nil {
		return "", err
	}

	return maker.createToken(user, "refresh", refreshExpiry, secretKey)
}

func (maker *TokenMaker) createToken(user *Model, tokenType string, duration time.Duration, secretKey string) (string, error) {
	claims := &Claims{
		UserID: user.ID,
		Email:  user.Email,
		Type:   tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			NotBefore: jwt.NewNumericDate(time.Now().UTC()),
		},
	}

	if tokenType == "refresh" {
		claims.PasswordFingerprint = passwordFingerprint(user.Password)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secretKey))
}

func (maker *TokenMaker) VerifyToken(ctx context.Context, tokenString string, tokenType string) (*Claims, error) {
	var secretKey string
	var err error

	// Get the appropriate secret key based on token type
	switch tokenType {
	case "access":
		secretKey, err = maker.accessSecret(ctx)
	case "refresh":
		secretKey, err = maker.refreshSecret(ctx)
	default:
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}

	// Tokens are signed with HS256 only, so any other algorithm is rejected.
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secretKey), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	if err := maker.checkSessionNotEnded(ctx, claims); err != nil {
		return nil, err
	}

	return claims, nil
}

// checkSessionNotEnded refuses a token issued before its user's sessions were ended. A failure to
// read the cutoff refuses the token too, since a token that might be an ended session must not pass.
func (maker *TokenMaker) checkSessionNotEnded(ctx context.Context, claims *Claims) error {
	if maker.cutoffs == nil {
		return nil
	}
	cutoff, err := maker.cutoffs.TokensValidAfter(ctx, claims.UserID)
	if err != nil {
		maker.logger.Errorw("Failed to read when the sessions of a user were ended", "userId", claims.UserID, "error", err)
		return fmt.Errorf("failed to check the session: %w", err)
	}
	if cutoff == nil {
		return nil
	}
	if claims.IssuedAt == nil {
		return ErrInvalidToken
	}
	// Round to the millisecond: a fractional iat does not survive the float it travels as exactly
	if claims.IssuedAt.Time.Round(time.Millisecond).Before(cutoff.Round(time.Millisecond)) {
		return ErrInvalidToken
	}
	return nil
}
