package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/installsettings"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events/eventstest"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// brokenAuthRepo is the auth repository whose reads fail where the test says
type brokenAuthRepo struct {
	auth.Repository
	findByEmail error
	count       error
}

func (r brokenAuthRepo) FindByEmail(ctx context.Context, email string) (*auth.Model, error) {
	if r.findByEmail != nil {
		return nil, r.findByEmail
	}
	return r.Repository.FindByEmail(ctx, email)
}

func (r brokenAuthRepo) FindAllCount(ctx context.Context) (int64, error) {
	if r.count != nil {
		return 0, r.count
	}
	return r.Repository.FindAllCount(ctx)
}

// newServiceWith is the harness's service with a verifier for the emails of new accounts
func newServiceWith(h *harness, verifier auth.Verifier) auth.Service {
	logger := zap.NewNop().Sugar()
	transactor := dbtx.NewSQLTransactor(h.db)
	workspaces := workspace.NewSQLRepository(h.db)
	invites := workspace.NewService(workspaces, workspace.NewSQLMemberRepository(h.db), workspace.NewSQLInviteRepository(h.db),
		transactor, eventstest.NewBus(), installsettings.Static(&config.Config{ClientURL: "http://localhost:3000"}), nil, nil, logger)
	return auth.NewService(auth.NewSQLRepository(h.db), transactor, workspaces, invites, h.tokens, verifier, logger)
}

func (h *harness) serviceOver(repo auth.Repository) auth.Service {
	return newService(h.db, repo, h.tokens, zap.NewNop().Sugar())
}

func TestService_Login_ALookupThatFailsIsAnInvalidLogin(t *testing.T) {
	h := newHarness(t)
	h.register(t)
	service := h.serviceOver(brokenAuthRepo{Repository: auth.NewSQLRepository(h.db), findByEmail: errors.New("database is closed")})

	session, err := service.Login(context.Background(), auth.LoginDto{Email: adminEmail, Password: adminPassword})

	assert.EqualError(t, err, "invalid credentials", "the database's reason is not told")
	assert.Nil(t, session)
}

// A disabled account is told so only to someone who knows its password, and before the second factor
// is asked for: the answer tells nobody which addresses have accounts
func TestService_Login_ADisabledAccountIsToldOnlyAfterTheRightPassword(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t).User
	ctx := context.Background()
	secret, _, err := h.svc.SetupTwoFA(ctx, admin.ID, adminPassword)
	require.NoError(t, err)
	verified, err := h.svc.VerifyTwoFA(ctx, admin.ID, currentCode(t, secret))
	require.NoError(t, err)
	require.True(t, verified)
	_, err = h.db.Exec("UPDATE users SET active = ? WHERE id = ?", false, admin.ID)
	require.NoError(t, err)

	_, err = h.login(wrongPassword, currentCode(t, secret))
	assert.EqualError(t, err, "invalid credentials", "a wrong password is not told it is disabled")
	_, err = h.login(wrongPassword, "")
	assert.EqualError(t, err, "invalid credentials")
	session, err := h.login(adminPassword, "")
	assert.ErrorIs(t, err, auth.ErrAccountDisabled, "and no code is asked for before it")
	assert.Nil(t, session)
	_, err = h.login(adminPassword, currentCode(t, secret))
	assert.ErrorIs(t, err, auth.ErrAccountDisabled)

	_, err = h.db.Exec("UPDATE users SET active = ? WHERE id = ?", true, admin.ID)
	require.NoError(t, err)
	_, err = h.login(adminPassword, currentCode(t, secret))
	assert.NoError(t, err, "enabled again, the account signs in")
}

// The second factor is asked for when the account has a secret and has turned it on, and not
// otherwise
func TestService_Login_AsksForTheSecondFactorOnlyForASecretThatIsOn(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t).User
	secret := "JBSWY3DPEHPK3PXP"

	for name, tc := range map[string]struct {
		secret string
		on     bool
		asked  bool
	}{
		"a secret that is on":       {secret, true, true},
		"a secret that is off":      {secret, false, false},
		"no secret and the flag on": {"", true, false},
		"nothing":                   {"", false, false},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := h.db.Exec("UPDATE users SET twofa_secret = ?, twofa_status = ? WHERE id = ?", tc.secret, tc.on, admin.ID)
			require.NoError(t, err)

			session, err := h.login(adminPassword, "")

			if tc.asked {
				assert.EqualError(t, err, "2FA token required")
				assert.Nil(t, session)
				return
			}
			require.NoError(t, err)
			h.assertSessionFor(t, session, admin)
		})
	}
}

func TestService_Register_AFailedCountOrAnOverlongPasswordCreatesNothing(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	counting := h.serviceOver(brokenAuthRepo{Repository: auth.NewSQLRepository(h.db), count: errors.New("database is closed")})
	session, err := counting.Register(ctx, auth.RegisterDto{Email: adminEmail, Password: adminPassword})
	assert.EqualError(t, err, "database is closed")
	assert.Nil(t, session)

	session, err = h.svc.Register(ctx, auth.RegisterDto{Email: adminEmail, Password: strings.Repeat("a", 73)})
	assert.ErrorContains(t, err, "password length exceeds 72 bytes")
	assert.Nil(t, session)

	assert.Equal(t, 0, h.count(t, "users"))
	assert.Equal(t, 0, h.count(t, "workspaces"))
	h.register(t) // and the install can still sign up its first user
}

// The first user is stored before tokens are issued: a server that can't sign a token has made the
// account all the same
func TestService_Register_AnAccountIsMadeEvenWhenNoTokenCanBeIssued(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	require.NoError(t, h.settings.DeleteByKey(ctx, shared.AccessTokenSecretKey))
	h = h.restart()

	session, err := h.svc.Register(ctx, auth.RegisterDto{Email: adminEmail, Password: adminPassword})

	assert.ErrorContains(t, err, "access token secret key setting not found")
	assert.Nil(t, session)
	assert.Equal(t, 1, h.count(t, "users"))
	assert.Equal(t, 1, h.count(t, "workspace_members"))
	_, err = h.svc.Register(ctx, auth.RegisterDto{Email: adminEmail, Password: adminPassword})
	assert.ErrorIs(t, err, auth.ErrAdminExists)
}

// A verification email is asked for once the account is stored, and a failure to send it does not fail
// the registration
type recordingVerifier struct {
	users []*auth.Model
	err   error
}

func (v *recordingVerifier) Enabled() bool { return true }

func (v *recordingVerifier) SendVerification(_ context.Context, user *auth.Model) error {
	v.users = append(v.users, user)
	return v.err
}

func TestService_Register_AsksTheVerifierForTheNewAdminAndSurvivesItsFailure(t *testing.T) {
	for name, failure := range map[string]error{"a verifier that sends": nil, "a verifier that fails": errors.New("no mail today")} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			verifier := &recordingVerifier{err: failure}
			withVerifier := newServiceWith(h, verifier)

			session, err := withVerifier.Register(context.Background(), auth.RegisterDto{Email: adminEmail, Password: adminPassword})

			require.NoError(t, err)
			h.assertSessionFor(t, session, session.User)
			require.Len(t, verifier.users, 1)
			assert.Equal(t, session.User.ID, verifier.users[0].ID)
			assert.Equal(t, adminEmail, verifier.users[0].Email)
		})
	}
}
