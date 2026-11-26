package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/installsettings"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/crypto/bcrypt"
)

// brokenAccounts is the account repository whose steps fail, or answer, as the test says
type brokenAccounts struct {
	auth.AccountRepository
	findLive      error
	consume       error
	consumeNobody bool
	setPassword   error
	markVerified  error
	deleteUnused  error
	calls         []string
}

func (a *brokenAccounts) FindLiveToken(ctx context.Context, purpose, hash string, now time.Time) (string, error) {
	a.calls = append(a.calls, "find")
	if a.findLive != nil {
		return "", a.findLive
	}
	return a.AccountRepository.FindLiveToken(ctx, purpose, hash, now)
}

func (a *brokenAccounts) ConsumeToken(ctx context.Context, purpose, hash string, now time.Time) (string, error) {
	a.calls = append(a.calls, "consume")
	if a.consume != nil {
		return "", a.consume
	}
	if a.consumeNobody {
		return "", nil
	}
	return a.AccountRepository.ConsumeToken(ctx, purpose, hash, now)
}

func (a *brokenAccounts) SetPassword(ctx context.Context, userID, hash string, at time.Time) error {
	a.calls = append(a.calls, "password")
	if a.setPassword != nil {
		return a.setPassword
	}
	return a.AccountRepository.SetPassword(ctx, userID, hash, at)
}

func (a *brokenAccounts) MarkEmailVerified(ctx context.Context, userID string, at time.Time) error {
	a.calls = append(a.calls, "verify")
	if a.markVerified != nil {
		return a.markVerified
	}
	return a.AccountRepository.MarkEmailVerified(ctx, userID, at)
}

func (a *brokenAccounts) DeleteUnusedTokens(ctx context.Context, userID, purpose string) error {
	a.calls = append(a.calls, "delete")
	if a.deleteUnused != nil {
		return a.deleteUnused
	}
	return a.AccountRepository.DeleteUnusedTokens(ctx, userID, purpose)
}

// brokenLift is a workspace repository that can't lift the suspension of the user's workspaces
type brokenLift struct{ workspace.Repository }

func (brokenLift) LiftUnverifiedOwnedBy(context.Context, string) ([]workspace.ID, error) {
	return nil, errors.New("lift failed")
}

type wakes struct{ woken []workspace.ID }

func (w *wakes) PublishActiveUpdated(_ context.Context, id workspace.ID) error {
	w.woken = append(w.woken, id)
	return nil
}

type resetFixture struct {
	*harness
	svc      *auth.AccountService
	accounts *brokenAccounts
	bus      *eventstest.Bus
	monitors *wakes
	logs     *observer.ObservedLogs
	userID   string
}

func newResetFixture(t *testing.T, workspaces func(workspace.Repository) workspace.Repository) *resetFixture {
	t.Helper()
	h := newHarness(t)
	userID := h.register(t).User.ID
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core).Sugar()
	f := &resetFixture{harness: h, accounts: &brokenAccounts{AccountRepository: auth.NewSQLAccountRepository(h.db)}, bus: eventstest.NewBus(), monitors: &wakes{}, logs: logs, userID: userID}
	var repo workspace.Repository = workspace.NewSQLRepository(h.db)
	if workspaces != nil {
		repo = workspaces(repo)
	}
	f.svc = auth.NewAccountService(auth.NewSQLRepository(h.db), f.accounts, dbtx.NewSQLTransactor(h.db), mailer.New(&config.Config{}, logger),
		repo, f.monitors, f.bus, installsettings.Static(&config.Config{ClientURL: "http://localhost:3000"}), logger)
	return f
}

// link makes a reset link for the user and returns its token
func (f *resetFixture) token(t *testing.T) string {
	t.Helper()
	link, _, err := f.svc.CreateResetLink(context.Background(), f.userID)
	require.NoError(t, err)
	return link[strings.LastIndex(link, "/")+1:]
}

// passwordWorks is whether the user's stored password is password
func (f *resetFixture) passwordWorks(t *testing.T, password string) bool {
	t.Helper()
	var hash string
	require.NoError(t, f.db.NewSelect().Table("users").Column("password").Where("id = ?", f.userID).Scan(context.Background(), &hash))
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// tokenIsLive is whether the token can still be spent
func (f *resetFixture) tokenIsLive(t *testing.T, token string) bool {
	t.Helper()
	var unused int
	require.NoError(t, f.db.NewSelect().Table("account_tokens").ColumnExpr("COUNT(*)").Where("used_at IS NULL").Where("token_hash = ?", hashOf(token)).Scan(context.Background(), &unused))
	return unused > 0
}

// hashOf is the hash a token is stored under
func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256$" + hex.EncodeToString(sum[:])
}

func TestAccountService_ResetPassword_SpendsALinkOnce(t *testing.T) {
	f := newResetFixture(t, nil)
	ctx := context.Background()
	other := f.token(t)
	token := f.token(t)
	suspended, err := workspace.NewSQLRepository(f.db).CreateSuspended(ctx, "Waiting", workspace.SuspendedUnverified)
	require.NoError(t, err)
	require.NoError(t, workspace.NewSQLRepository(f.db).AddMember(ctx, suspended.ID, f.userID, workspace.RoleOwner))

	require.NoError(t, f.svc.ResetPassword(ctx, token, "New-password-1"))

	assert.True(t, f.passwordWorks(t, "New-password-1"))
	assert.False(t, f.passwordWorks(t, adminPassword))
	assert.False(t, f.tokenIsLive(t, token), "the link is spent")
	assert.False(t, f.tokenIsLive(t, other), "and the user's other reset links with it")
	assert.Equal(t, []string{"find", "consume", "password", "verify", "delete"}, f.accounts.calls, "the lookup comes before the hash, and the steps in order")
	assert.Equal(t, []workspace.ID{suspended.ID}, f.monitors.woken, "the workspace the verification lifted is woken")
	assert.Equal(t, []auth.SessionsEndedEvent{{UserID: f.userID}}, eventstest.Published(f.bus, auth.SessionsEnded))
	var verified int
	require.NoError(t, f.db.NewSelect().Table("users").ColumnExpr("COUNT(*)").Where("id = ?", f.userID).Where("email_verified_at IS NOT NULL").Scan(ctx, &verified))
	assert.Equal(t, 1, verified, "the address that got the link is verified")

	err = f.svc.ResetPassword(ctx, token, "Another-password-1")
	assert.ErrorIs(t, err, auth.ErrInvalidLink, "a spent link is refused")
	assert.True(t, f.passwordWorks(t, "New-password-1"))
}

func TestAccountService_ResetPassword_RefusesALinkThatCannotBeOneBeforeReadingAnything(t *testing.T) {
	f := newResetFixture(t, nil)
	good := f.token(t)

	for name, token := range map[string]string{
		"empty":               "",
		"a verification link": "pkv_" + good[len(auth.ResetTokenPrefix):],
		"no prefix":           good[len(auth.ResetTokenPrefix):],
		"too long":            auth.ResetTokenPrefix + strings.Repeat("a", 200),
	} {
		err := f.svc.ResetPassword(context.Background(), token, "New-password-1")

		assert.ErrorIs(t, err, auth.ErrInvalidLink, name)
	}
	assert.Empty(t, f.accounts.calls, "no lookup was made")
	assert.True(t, f.tokenIsLive(t, good))
}

func TestAccountService_ResetPassword_RefusesAnUnknownLinkAfterTheLookupAndBeforeTheHash(t *testing.T) {
	f := newResetFixture(t, nil)

	// 100 characters of password, too long for bcrypt: a hash would fail, and the answer shows it didn't run
	err := f.svc.ResetPassword(context.Background(), auth.ResetTokenPrefix+"unknown", strings.Repeat("a", 100))

	assert.ErrorIs(t, err, auth.ErrInvalidLink)
	assert.Equal(t, []string{"find"}, f.accounts.calls)
}

func TestAccountService_ResetPassword_AFailedLookupIsReturnedAsItIs(t *testing.T) {
	f := newResetFixture(t, nil)
	token := f.token(t)
	f.accounts.findLive = errors.New("database is closed")

	err := f.svc.ResetPassword(context.Background(), token, "New-password-1")

	assert.Same(t, f.accounts.findLive, err)
	assert.True(t, f.passwordWorks(t, adminPassword))
}

func TestAccountService_ResetPassword_APasswordThatCannotBeHashedLeavesTheLinkLive(t *testing.T) {
	f := newResetFixture(t, nil)
	token := f.token(t)

	err := f.svc.ResetPassword(context.Background(), token, strings.Repeat("a", 73))

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to hash the new password: ")
	assert.ErrorContains(t, err, "password length exceeds 72 bytes")
	assert.True(t, f.tokenIsLive(t, token))
	assert.True(t, f.passwordWorks(t, adminPassword))
	assert.Equal(t, []string{"find"}, f.accounts.calls)
}

func TestAccountService_ResetPassword_AFailureAtAnyStepSpendsNothing(t *testing.T) {
	steps := map[string]func(f *resetFixture){
		"the link can't be spent":          func(f *resetFixture) { f.accounts.consume = errors.New("consume failed") },
		"the password can't be stored":     func(f *resetFixture) { f.accounts.setPassword = errors.New("password failed") },
		"the address can't be marked":      func(f *resetFixture) { f.accounts.markVerified = errors.New("verify failed") },
		"the other links can't be deleted": func(f *resetFixture) { f.accounts.deleteUnused = errors.New("delete failed") },
	}
	for name, fail := range steps {
		t.Run(name, func(t *testing.T) {
			f := newResetFixture(t, nil)
			token := f.token(t)
			fail(f)

			err := f.svc.ResetPassword(context.Background(), token, "New-password-1")

			require.Error(t, err)
			assert.True(t, f.passwordWorks(t, adminPassword), "the transaction is rolled back")
			assert.True(t, f.tokenIsLive(t, token))
			assert.Empty(t, f.monitors.woken)
			assert.Empty(t, f.bus.Topics(), "and no session is said to have ended")
		})
	}
	t.Run("the workspaces can't be lifted", func(t *testing.T) {
		f := newResetFixture(t, func(r workspace.Repository) workspace.Repository { return brokenLift{r} })
		token := f.token(t)

		err := f.svc.ResetPassword(context.Background(), token, "New-password-1")

		assert.EqualError(t, err, "lift failed")
		assert.True(t, f.passwordWorks(t, adminPassword))
		assert.True(t, f.tokenIsLive(t, token))
		assert.Empty(t, f.bus.Topics())
	})
}

// A link that a lookup found and that another request spent before this one could is refused
func TestAccountService_ResetPassword_ALinkSpentMeanwhileIsInvalid(t *testing.T) {
	f := newResetFixture(t, nil)
	token := f.token(t)
	f.accounts.consumeNobody = true

	err := f.svc.ResetPassword(context.Background(), token, "New-password-1")

	assert.ErrorIs(t, err, auth.ErrInvalidLink)
	assert.True(t, f.passwordWorks(t, adminPassword))
	assert.Equal(t, []string{"find", "consume"}, f.accounts.calls)
}

// The reset stands when the bus that says so is down
func TestAccountService_ResetPassword_ABusThatFailsIsLoggedAndTheResetStands(t *testing.T) {
	f := newResetFixture(t, nil)
	token := f.token(t)
	require.NoError(t, f.bus.Close(context.Background()))

	err := f.svc.ResetPassword(context.Background(), token, "New-password-1")

	require.NoError(t, err)
	assert.True(t, f.passwordWorks(t, "New-password-1"))
	entries := f.logs.FilterMessage("Failed to publish that the sessions of a user ended").All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	assert.Equal(t, f.userID, entries[0].ContextMap()["userId"])
}
