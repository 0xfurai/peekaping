package auth_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const (
	adminEmail    = "admin@example.com"
	adminPassword = "Correct-horse-1"
	wrongPassword = "Wrong-password-1"
)

// harness is the auth service over a migrated SQLite database, with the
// settings the server creates at startup.
type harness struct {
	db       *bun.DB
	svc      auth.Service
	tokens   *auth.TokenMaker
	settings shared.SettingService
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "auth.db"))
	require.NoError(t, err)
	db := bun.NewDB(sqlDB, sqlitedialect.New())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err = migrator.Migrate(ctx)
	require.NoError(t, err)

	logger := zap.NewNop().Sugar()
	settings := setting.NewService(setting.NewSQLRepository(db), logger)
	require.NoError(t, settings.InitializeSettings(ctx))
	tokens := auth.NewTokenMaker(settings, logger)
	return &harness{db, auth.NewService(auth.NewSQLRepository(db), tokens, logger), tokens, settings}
}

// restart is the same install after the server starts again, with its signing
// secrets read from the settings anew.
func (h *harness) restart() *harness {
	logger := zap.NewNop().Sugar()
	tokens := auth.NewTokenMaker(h.settings, logger)
	return &harness{h.db, auth.NewService(auth.NewSQLRepository(h.db), tokens, logger), tokens, h.settings}
}

// register creates the admin and returns its first session.
func (h *harness) register(t *testing.T) *auth.LoginResponse {
	t.Helper()
	session, err := h.svc.Register(context.Background(), auth.RegisterDto{Email: adminEmail, Password: adminPassword})
	require.NoError(t, err)
	return session
}

func (h *harness) login(password, token string) (*auth.LoginResponse, error) {
	return h.svc.Login(context.Background(), auth.LoginDto{Email: adminEmail, Password: password, Token: token})
}

// secret returns the signing secret stored under key.
func (h *harness) secret(t *testing.T, key string) string {
	t.Helper()
	s, err := h.settings.GetByKey(context.Background(), key)
	require.NoError(t, err)
	return s.Value
}

// sign makes a token the way the server does, with any claims and secret.
func sign(t *testing.T, secret string, claims auth.Claims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return token
}

// assertSessionFor checks that session's tokens verify as user's access and
// refresh tokens.
func (h *harness) assertSessionFor(t *testing.T, session *auth.LoginResponse, user *auth.Model) {
	t.Helper()
	ctx := context.Background()
	access, err := h.tokens.VerifyToken(ctx, session.AccessToken, "access")
	require.NoError(t, err)
	assert.Equal(t, user.ID, access.UserID)
	assert.Equal(t, user.Email, access.Email)
	assert.Equal(t, "access", access.Type)
	refresh, err := h.tokens.VerifyToken(ctx, session.RefreshToken, "refresh")
	require.NoError(t, err)
	assert.Equal(t, user.ID, refresh.UserID)
	assert.Equal(t, "refresh", refresh.Type)
}

// currentCode is the TOTP code an authenticator app shows now.
func currentCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	return code
}

// wrongCode is a six-digit code that differs from code in its first digit.
func wrongCode(code string) string {
	return string('0'+(code[0]-'0'+1)%10) + code[1:]
}

func TestService_Register_CreatesTheOnlyAdmin(t *testing.T) {
	h := newHarness(t)

	session := h.register(t)
	require.NotNil(t, session.User)
	assert.NotEmpty(t, session.User.ID)
	assert.Equal(t, adminEmail, session.User.Email)
	assert.True(t, session.User.Active)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(session.User.Password), []byte(adminPassword)), "the password is stored as a bcrypt hash")
	h.assertSessionFor(t, session, session.User)

	_, err := h.svc.Register(context.Background(), auth.RegisterDto{Email: "second@example.com", Password: adminPassword})
	assert.EqualError(t, err, "admin already exists")
}

// countBeforeCommit is a repository that still counts no users, as Register
// sees it when it counts before a concurrent registration commits.
type countBeforeCommit struct{ auth.Repository }

func (countBeforeCommit) FindAllCount(context.Context) (int64, error) { return 0, nil }

func TestService_Register_LosingTheRaceWithTheSameEmailIsAdminExists(t *testing.T) {
	h := newHarness(t)
	h.register(t)

	racing := auth.NewService(countBeforeCommit{auth.NewSQLRepository(h.db)}, h.tokens, zap.NewNop().Sugar())
	_, err := racing.Register(context.Background(), auth.RegisterDto{Email: adminEmail, Password: adminPassword})
	assert.ErrorIs(t, err, auth.ErrAdminExists)
}

func TestService_Login(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t).User

	session, err := h.login(adminPassword, "")
	require.NoError(t, err)
	assert.Equal(t, admin.ID, session.User.ID)
	h.assertSessionFor(t, session, admin)

	_, err = h.login(wrongPassword, "")
	assert.EqualError(t, err, "invalid credentials")
	_, err = h.svc.Login(context.Background(), auth.LoginDto{Email: "nobody@example.com", Password: adminPassword})
	assert.EqualError(t, err, "invalid credentials", "an unknown email fails the same way as a wrong password")
}

func TestService_Login_RequiresTheSecondFactorOnceVerified(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t).User
	ctx := context.Background()

	secret, _, err := h.svc.SetupTwoFA(ctx, admin.ID, adminPassword)
	require.NoError(t, err)
	_, err = h.login(adminPassword, "")
	require.NoError(t, err, "2FA isn't on until a code is verified")

	verified, err := h.svc.VerifyTwoFA(ctx, admin.ID, currentCode(t, secret))
	require.NoError(t, err)
	require.True(t, verified)

	_, err = h.login(adminPassword, "")
	assert.EqualError(t, err, "2FA token required")
	_, err = h.login(adminPassword, wrongCode(currentCode(t, secret)))
	assert.EqualError(t, err, "invalid 2FA token")
	_, err = h.login(wrongPassword, currentCode(t, secret))
	assert.EqualError(t, err, "invalid credentials", "a valid code doesn't stand in for the password")
	session, err := h.login(adminPassword, currentCode(t, secret))
	require.NoError(t, err)
	h.assertSessionFor(t, session, admin)
}

func TestService_SetupTwoFA(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t).User
	ctx := context.Background()

	_, _, err := h.svc.SetupTwoFA(ctx, admin.ID, wrongPassword)
	assert.EqualError(t, err, "invalid password")
	_, err = h.svc.VerifyTwoFA(ctx, admin.ID, "123456")
	assert.EqualError(t, err, "2FA not setup", "a rejected setup stores no secret")
	_, _, err = h.svc.SetupTwoFA(ctx, "missing-user", adminPassword)
	assert.EqualError(t, err, "user not found")

	secret, uri, err := h.svc.SetupTwoFA(ctx, admin.ID, adminPassword)
	require.NoError(t, err)
	key, err := otp.NewKeyFromURL(uri)
	require.NoError(t, err)
	assert.Equal(t, secret, key.Secret(), "the QR code holds the secret")
	assert.Equal(t, "peekaping", key.Issuer())
	assert.Equal(t, adminEmail, key.AccountName())
}

func TestService_SetupTwoFA_AgainBeforeVerifyingShowsTheSameSecret(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t).User
	ctx := context.Background()

	first, _, err := h.svc.SetupTwoFA(ctx, admin.ID, adminPassword)
	require.NoError(t, err)
	again, uri, err := h.svc.SetupTwoFA(ctx, admin.ID, adminPassword)
	require.NoError(t, err)
	assert.Equal(t, first, again)

	key, err := otp.NewKeyFromURL(uri)
	require.NoError(t, err)
	assert.Equal(t, first, key.Secret(), "the QR code holds the same secret")
	verified, err := h.svc.VerifyTwoFA(ctx, admin.ID, currentCode(t, key.Secret()))
	require.NoError(t, err)
	assert.True(t, verified, "a code from the scanned QR code verifies")
}

func TestService_VerifyTwoFA(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t).User
	ctx := context.Background()

	_, err := h.svc.VerifyTwoFA(ctx, admin.ID, "123456")
	assert.EqualError(t, err, "2FA not setup")
	_, err = h.svc.VerifyTwoFA(ctx, "missing-user", "123456")
	assert.EqualError(t, err, "user not found")

	secret, _, err := h.svc.SetupTwoFA(ctx, admin.ID, adminPassword)
	require.NoError(t, err)
	verified, err := h.svc.VerifyTwoFA(ctx, admin.ID, wrongCode(currentCode(t, secret)))
	require.NoError(t, err)
	assert.False(t, verified)
	_, err = h.login(adminPassword, "")
	assert.NoError(t, err, "a wrong code leaves 2FA off")
}

func TestService_VerifyTwoFA_ReportsAFailedSave(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t).User
	ctx := context.Background()
	secret, _, err := h.svc.SetupTwoFA(ctx, admin.ID, adminPassword)
	require.NoError(t, err)
	_, err = h.db.Exec(`CREATE TRIGGER users_read_only BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'users are read-only'); END`)
	require.NoError(t, err)

	verified, err := h.svc.VerifyTwoFA(ctx, admin.ID, currentCode(t, secret))
	assert.EqualError(t, err, "failed to enable 2FA")
	assert.False(t, verified)
	_, err = h.login(adminPassword, "")
	assert.NoError(t, err, "2FA stayed off")
}

func TestService_DisableTwoFA(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t).User
	ctx := context.Background()
	secret, _, err := h.svc.SetupTwoFA(ctx, admin.ID, adminPassword)
	require.NoError(t, err)
	_, err = h.svc.VerifyTwoFA(ctx, admin.ID, currentCode(t, secret))
	require.NoError(t, err)

	assert.EqualError(t, h.svc.DisableTwoFA(ctx, admin.ID, wrongPassword), "invalid password")
	_, err = h.login(adminPassword, "")
	assert.EqualError(t, err, "2FA token required", "a rejected disable leaves 2FA on")
	assert.EqualError(t, h.svc.DisableTwoFA(ctx, "missing-user", adminPassword), "user not found")

	require.NoError(t, h.svc.DisableTwoFA(ctx, admin.ID, adminPassword))
	_, err = h.login(adminPassword, "")
	assert.NoError(t, err)
	_, err = h.svc.VerifyTwoFA(ctx, admin.ID, currentCode(t, secret))
	assert.EqualError(t, err, "2FA not setup", "disabling drops the secret")
}

func TestService_RefreshToken(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)
	ctx := context.Background()

	refreshed, err := h.svc.RefreshToken(ctx, session.RefreshToken)
	require.NoError(t, err)
	h.assertSessionFor(t, refreshed, session.User)

	_, err = h.svc.RefreshToken(ctx, session.AccessToken)
	assert.EqualError(t, err, "invalid refresh token", "an access token is signed with the other secret")
	_, err = h.svc.RefreshToken(ctx, "not-a-token")
	assert.EqualError(t, err, "invalid refresh token")

	claims := auth.Claims{UserID: session.User.ID, Email: adminEmail, Type: "access", RegisteredClaims: jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	_, err = h.svc.RefreshToken(ctx, sign(t, h.secret(t, shared.RefreshTokenSecretKey), claims))
	assert.EqualError(t, err, "invalid token type")

	orphan, err := h.tokens.CreateRefreshToken(ctx, &auth.Model{ID: "missing-user", Email: "gone@example.com"})
	require.NoError(t, err)
	_, err = h.svc.RefreshToken(ctx, orphan)
	assert.EqualError(t, err, "user not found")
}

func TestService_UpdatePassword(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t).User
	ctx := context.Background()
	const newPassword = "Battery-staple-2"

	err := h.svc.UpdatePassword(ctx, admin.ID, auth.UpdatePasswordDto{CurrentPassword: wrongPassword, NewPassword: newPassword})
	assert.EqualError(t, err, "current password is incorrect")
	_, err = h.login(adminPassword, "")
	assert.NoError(t, err, "a rejected change keeps the old password")
	err = h.svc.UpdatePassword(ctx, "missing-user", auth.UpdatePasswordDto{CurrentPassword: adminPassword, NewPassword: newPassword})
	assert.EqualError(t, err, "user not found")

	require.NoError(t, h.svc.UpdatePassword(ctx, admin.ID, auth.UpdatePasswordDto{CurrentPassword: adminPassword, NewPassword: newPassword}))
	_, err = h.login(adminPassword, "")
	assert.EqualError(t, err, "invalid credentials")
	_, err = h.login(newPassword, "")
	assert.NoError(t, err)
}

func TestService_IssuesNoTokensWithoutSigningSecrets(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)
	ctx := context.Background()

	// A server keeps its secrets for a minute, so each deletion is followed by a restart
	require.NoError(t, h.settings.DeleteByKey(ctx, shared.RefreshTokenSecretKey))
	h = h.restart()
	_, err := h.login(adminPassword, "")
	assert.ErrorContains(t, err, "refresh token secret key setting not found")

	require.NoError(t, h.settings.DeleteByKey(ctx, shared.AccessTokenSecretKey))
	h = h.restart()
	_, err = h.login(adminPassword, "")
	assert.ErrorContains(t, err, "access token secret key setting not found")
	_, err = h.svc.RefreshToken(ctx, session.RefreshToken)
	assert.Error(t, err)
}
