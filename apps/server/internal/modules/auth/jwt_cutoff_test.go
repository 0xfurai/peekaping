package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/modules/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
