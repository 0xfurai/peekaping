package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/shared"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// PP-152: a password reset ends every session of the user. A token is refused when it was issued
// before the user's tokens_valid_after.

type failingCutoffs struct{}

func (failingCutoffs) TokensValidAfter(context.Context, string) (*time.Time, error) {
	return nil, errors.New("database is down")
}

func (failingCutoffs) UserActive(context.Context, string) (bool, error) {
	return false, errors.New("database is down")
}

func TestTokenMaker_RefusesATokenIssuedBeforeTheUsersCutoffAndAcceptsOneIssuedAfter(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)
	ctx := context.Background()
	accounts := auth.NewSQLAccountRepository(h.db)
	tokens := auth.NewTokenMaker(h.settings, zap.NewNop().Sugar()).WithSessionCutoffs(accounts)
	user, err := auth.NewSQLRepository(h.db).FindByEmail(ctx, adminEmail)
	require.NoError(t, err)

	for _, kind := range []string{"access", "refresh"} {
		token := map[string]string{"access": session.AccessToken, "refresh": session.RefreshToken}[kind]
		_, err := tokens.VerifyToken(ctx, token, kind)
		require.NoError(t, err, "before the cutoff there is none: %s", kind)
	}

	// The cutoff is a moment inside the second the first session was issued in, and a session that
	// starts after it must work: the tokens carry their issue time to the millisecond
	time.Sleep(5 * time.Millisecond)
	cutoff := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, accounts.SetPassword(ctx, user.ID, user.Password, cutoff))
	time.Sleep(5 * time.Millisecond)
	fresh, err := tokens.CreateAccessToken(ctx, user)
	require.NoError(t, err)
	freshRefresh, err := tokens.CreateRefreshToken(ctx, user)
	require.NoError(t, err)

	_, err = tokens.VerifyToken(ctx, session.AccessToken, "access")
	assert.ErrorIs(t, err, auth.ErrInvalidToken, "an access token issued before")
	_, err = tokens.VerifyToken(ctx, session.RefreshToken, "refresh")
	assert.ErrorIs(t, err, auth.ErrInvalidToken, "a refresh token issued before")
	_, err = tokens.VerifyToken(ctx, fresh, "access")
	assert.NoError(t, err, "an access token issued after")
	_, err = tokens.VerifyToken(ctx, freshRefresh, "refresh")
	assert.NoError(t, err, "a refresh token issued after")
}

func TestTokenMaker_ATokenIssuedInTheSameMillisecondAsTheCutoffWorks(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	ctx := context.Background()
	accounts := auth.NewSQLAccountRepository(h.db)
	user, err := auth.NewSQLRepository(h.db).FindByEmail(ctx, adminEmail)
	require.NoError(t, err)
	tokens := auth.NewTokenMaker(h.settings, zap.NewNop().Sugar()).WithSessionCutoffs(accounts)

	token, err := tokens.CreateAccessToken(ctx, user)
	require.NoError(t, err)
	claims, err := tokens.VerifyToken(ctx, token, "access")
	require.NoError(t, err)
	// the cutoff is the very instant the token says it was issued
	require.NoError(t, accounts.SetPassword(ctx, user.ID, user.Password, claims.IssuedAt.Time))

	_, err = tokens.VerifyToken(ctx, token, "access")

	assert.NoError(t, err, "a session that starts when the sessions are ended is the new one")
}

func TestTokenMaker_ATokenThatMightBeAnEndedSessionIsRefusedWhenTheCutoffCannotBeRead(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)
	tokens := auth.NewTokenMaker(h.settings, zap.NewNop().Sugar()).WithSessionCutoffs(failingCutoffs{})

	_, err := tokens.VerifyToken(context.Background(), session.AccessToken, "access")

	assert.Error(t, err)
}

func TestTokenMaker_WithoutCutoffsATokenIsAccepted(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)

	_, err := h.tokens.VerifyToken(context.Background(), session.AccessToken, "access")

	assert.NoError(t, err, "a maker with no cutoffs checks none")
}

// An instance admin deletes a user (PP-371): the tokens they hold have not expired, but they end
func TestTokenMaker_RefusesATokenOfAUserWhoNoLongerExists(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tokens := auth.NewTokenMaker(h.settings, zap.NewNop().Sugar()).WithSessionCutoffs(auth.NewSQLAccountRepository(h.db))
	ghost := &auth.Model{ID: "00000000-0000-0000-0000-000000000000", Email: "ghost@example.com"}

	access, err := tokens.CreateAccessToken(ctx, ghost)
	require.NoError(t, err)
	refresh, err := tokens.CreateRefreshToken(ctx, ghost)
	require.NoError(t, err)

	_, err = tokens.VerifyToken(ctx, access, "access")
	assert.ErrorIs(t, err, auth.ErrInvalidToken)
	_, err = tokens.VerifyToken(ctx, refresh, "refresh")
	assert.ErrorIs(t, err, auth.ErrInvalidToken)
}

// An open socket checks its user once more when it joins a room (PP-371): the account has to exist
// and be active
func TestTokenMaker_CheckUserCanSignInRefusesADisabledOrDeletedUserAndOneThatCannotBeRead(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	ctx := context.Background()
	user, err := auth.NewSQLRepository(h.db).FindByEmail(ctx, adminEmail)
	require.NoError(t, err)
	tokens := auth.NewTokenMaker(h.settings, zap.NewNop().Sugar()).WithSessionCutoffs(auth.NewSQLAccountRepository(h.db))

	assert.NoError(t, tokens.CheckUserCanSignIn(ctx, user.ID))

	_, err = h.db.Exec("UPDATE users SET active = ? WHERE id = ?", false, user.ID)
	require.NoError(t, err)
	assert.ErrorIs(t, tokens.CheckUserCanSignIn(ctx, user.ID), auth.ErrInvalidToken, "disabled")

	_, err = h.db.Exec("DELETE FROM users WHERE id = ?", user.ID)
	require.NoError(t, err)
	assert.ErrorIs(t, tokens.CheckUserCanSignIn(ctx, user.ID), auth.ErrInvalidToken, "gone")

	failing := auth.NewTokenMaker(h.settings, zap.NewNop().Sugar()).WithSessionCutoffs(failingCutoffs{})
	err = failing.CheckUserCanSignIn(ctx, user.ID)
	assert.Error(t, err, "an account that can't be read is refused")
	assert.NotErrorIs(t, err, auth.ErrInvalidToken)
	assert.NoError(t, h.tokens.CheckUserCanSignIn(ctx, user.ID), "a maker with no cutoffs checks none")
}

// afterFind runs a hook once FindByID has answered, to force the moment a request has read the
// user and another one commits before the first goes on
type afterFind struct {
	auth.Repository
	hook func(user *auth.Model)
}

func (r *afterFind) FindByID(ctx context.Context, id string) (*auth.Model, error) {
	user, err := r.Repository.FindByID(ctx, id)
	if user != nil && r.hook != nil {
		hook := r.hook
		r.hook = nil
		hook(user)
	}
	return user, err
}

func TestRefreshToken_ARefreshThatRacesAPasswordResetGetsATokenThatIsRefused(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)
	ctx := context.Background()
	accounts := auth.NewSQLAccountRepository(h.db)
	tokens := auth.NewTokenMaker(h.settings, zap.NewNop().Sugar()).WithSessionCutoffs(accounts)
	repo := &afterFind{Repository: auth.NewSQLRepository(h.db)}
	svc := newService(h.db, repo, tokens, zap.NewNop().Sugar())
	time.Sleep(5 * time.Millisecond)
	// the reset commits after the refresh has read the user and before it makes its tokens
	repo.hook = func(user *auth.Model) {
		hashed, err := bcrypt.GenerateFromPassword([]byte("Reset-password-1"), bcrypt.MinCost)
		require.NoError(t, err)
		require.NoError(t, accounts.SetPassword(ctx, user.ID, string(hashed), time.Now().UTC().Truncate(time.Millisecond)))
		time.Sleep(5 * time.Millisecond)
	}

	refreshed, err := svc.RefreshToken(ctx, session.RefreshToken)
	require.NoError(t, err, "the refresh token was valid when the request began")

	_, err = tokens.VerifyToken(ctx, refreshed.AccessToken, "access")
	assert.ErrorIs(t, err, auth.ErrInvalidToken, "an access token of a session that began before the reset")
	_, err = tokens.VerifyToken(ctx, refreshed.RefreshToken, "refresh")
	assert.ErrorIs(t, err, auth.ErrInvalidToken, "a refresh token of a session that began before the reset")
}

func TestRefreshToken_TheNewTokensKeepTheTimeTheSessionBegan(t *testing.T) {
	h := newHarness(t)
	session := h.register(t)
	ctx := context.Background()
	began, err := h.tokens.VerifyToken(ctx, session.RefreshToken, "refresh")
	require.NoError(t, err)
	require.NotNil(t, began.AuthTime, "a token carries auth_time")
	time.Sleep(20 * time.Millisecond)

	refreshed, err := h.svc.RefreshToken(ctx, session.RefreshToken)
	require.NoError(t, err)

	for kind, token := range map[string]string{"access": refreshed.AccessToken, "refresh": refreshed.RefreshToken} {
		claims, err := h.tokens.VerifyToken(ctx, token, kind)
		require.NoError(t, err)
		require.NotNil(t, claims.AuthTime, kind)
		assert.True(t, claims.AuthTime.Time.Equal(began.AuthTime.Time), "%s: auth_time is the session's start, not the refresh", kind)
		assert.True(t, claims.IssuedAt.Time.After(began.AuthTime.Time), "%s: iat is the refresh", kind)
	}
}

func TestTokenMaker_ATokenWithoutAuthTimeIsJudgedByItsIssueTime(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	ctx := context.Background()
	accounts := auth.NewSQLAccountRepository(h.db)
	tokens := auth.NewTokenMaker(h.settings, zap.NewNop().Sugar()).WithSessionCutoffs(accounts)
	user, err := auth.NewSQLRepository(h.db).FindByEmail(ctx, adminEmail)
	require.NoError(t, err)
	legacy := func(issued time.Time) string {
		return sign(t, h.secret(t, shared.AccessTokenSecretKey), auth.Claims{UserID: user.ID, Email: adminEmail, Type: "access",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(issued.Add(time.Hour)), IssuedAt: jwt.NewNumericDate(issued), NotBefore: jwt.NewNumericDate(issued),
			}})
	}
	cutoff := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	require.NoError(t, accounts.SetPassword(ctx, user.ID, user.Password, cutoff))

	_, err = tokens.VerifyToken(ctx, legacy(cutoff.Add(-time.Second)), "access")
	assert.ErrorIs(t, err, auth.ErrInvalidToken, "issued before the cutoff")
	_, err = tokens.VerifyToken(ctx, legacy(cutoff.Add(time.Second)), "access")
	assert.NoError(t, err, "issued after the cutoff")
}

func TestTokenMaker_AuthTimeSurvivesBeingCopiedFromTokenToToken(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	ctx := context.Background()
	user, err := auth.NewSQLRepository(h.db).FindByEmail(ctx, adminEmail)
	require.NoError(t, err)
	// 1793208344.587 is a value a float reads back as .586999..., which jwt.NumericDate truncates to .586
	began := time.UnixMilli(1793208344587).UTC()

	for i := 0; i < 5; i++ {
		token, err := h.tokens.CreateRefreshTokenForSession(ctx, user, began)
		require.NoError(t, err)
		verified := &auth.Claims{}
		_, _, err = jwt.NewParser().ParseUnverified(token, verified)
		require.NoError(t, err)
		began = verified.AuthTime.Time
	}

	assert.Equal(t, int64(1793208344587), began.UnixMilli(), "after five copies")
}
