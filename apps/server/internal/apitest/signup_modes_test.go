package apitest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/scheduler"
	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-155: signup modes. In invite mode (the default) the first user signs up and everyone after
// joins through an invite. In open mode anyone signs up and gets a workspace of their own, which is
// suspended until their email address is verified.

const (
	workspacesPath = "/api/v1/workspaces"
	// turnstileSecret and turnstileSiteKey are the settings of the tests that guard signup with Turnstile
	turnstileSecret  = "ts-secret"
	turnstileSiteKey = "ts-site-key"
)

func withSignupMode(mode string) func(*config.Config) {
	return func(cfg *config.Config) { cfg.SignupMode = mode }
}

// withOpenSignup makes signup open, with the fake SMTP server as the sender account emails need
func withOpenSignup(server *smtptest.Server) func(*config.Config) {
	return func(cfg *config.Config) {
		withAccountSMTP(server)(cfg)
		cfg.SignupMode = config.SignupModeOpen
	}
}

// withTurnstile guards signup with Turnstile, checked at the fake siteverify server
func withTurnstile(verifyURL string) func(*config.Config) {
	return func(cfg *config.Config) {
		cfg.SignupTurnstileSiteKey, cfg.SignupTurnstileSecret, cfg.SignupTurnstileVerifyURL = turnstileSiteKey, turnstileSecret, verifyURL
	}
}

// forEachBackend runs fn on SQLite and, when PEE_TEST_POSTGRES_DSN is set, on PostgreSQL. fn gets
// the option that picks the database.
func forEachBackend(t *testing.T, fn func(t *testing.T, backend func(*config.Config))) {
	t.Helper()
	backends := []struct {
		name   string
		option func(*config.Config)
	}{{"SQLite", func(*config.Config) {}}}
	if dsn := os.Getenv("PEE_TEST_POSTGRES_DSN"); dsn != "" {
		backends = append(backends, struct {
			name   string
			option func(*config.Config)
		}{"PostgreSQL", usePostgres(t, dsn)})
	}
	for _, backend := range backends {
		t.Run(backend.name, func(t *testing.T) { fn(t, backend.option) })
	}
}

// fromAddress makes a request come from the client address
func fromAddress(address string) credentials {
	return func(req *http.Request) { req.RemoteAddr = address + ":4000" }
}

func (a *app) signUp(t *testing.T, email string, creds ...credentials) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, registerPath, map[string]any{"email": email, "password": adminPassword}, creds...)
}

// signedUp signs the email up, which must succeed, and returns the credentials of its session
func (a *app) signedUp(t *testing.T, email string) credentials {
	t.Helper()
	session := decode[registered](t, a.signUp(t, email), http.StatusCreated)
	return bearer(session.AccessToken)
}

func (a *app) meOf(t *testing.T, creds credentials) meResponse {
	t.Helper()
	return decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, creds), http.StatusOK)
}

type signupConfig struct {
	AccountEmails    bool    `json:"accountEmails"`
	SignupMode       string  `json:"signupMode"`
	SignupOpen       bool    `json:"signupOpen"`
	TurnstileSiteKey *string `json:"turnstileSiteKey"`
}

func (a *app) authConfig(t *testing.T) signupConfig {
	t.Helper()
	return decode[signupConfig](t, a.request(t, http.MethodGet, configPath, nil), http.StatusOK)
}

// verifyWithMail follows the verification link the address was sent
func (a *app) verifyWithMail(t *testing.T, mail *smtptest.Server, email string) {
	t.Helper()
	a.settled(t)
	messages := mailTo(mail, email)
	require.NotEmpty(t, messages, "no email was sent to %s", email)
	token := tokenIn(t, messages[len(messages)-1], "pkv_")
	w := a.request(t, http.MethodPost, verifyPath, map[string]any{"token": token})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// AC2: an install that sets nothing, and one that says invite, behave as before
func TestSignupModes_InviteModeLetsTheFirstUserInAndNoOneElse(t *testing.T) {
	for name, mode := range map[string]string{"unset": "", "invite": config.SignupModeInvite} {
		t.Run(name, func(t *testing.T) {
			forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
				a := newApp(t, backend, withSignupMode(mode))
				before := a.authConfig(t)
				assert.Equal(t, config.SignupModeInvite, before.SignupMode)
				assert.True(t, before.SignupOpen, "the install has no user yet, so its first can sign up")
				assert.Nil(t, before.TurnstileSiteKey)

				first := a.signedUp(t, "first@example.com")

				me := a.meOf(t, first)
				assert.True(t, me.User.InstanceAdmin)
				require.Len(t, me.Workspaces, 1)
				assert.Equal(t, "My workspace", me.Workspaces[0].Name)
				assert.Equal(t, "owner", me.Workspaces[0].Role)
				assert.False(t, me.Workspaces[0].Suspended, "an invite install never suspends")
				assert.Nil(t, me.Workspaces[0].SuspendedReason)
				after := a.authConfig(t)
				assert.False(t, after.SignupOpen, "and then it is closed")
				assert.Equal(t, config.SignupModeInvite, after.SignupMode)

				second := a.signUp(t, "second@example.com")
				assert.Equal(t, http.StatusBadRequest, second.Code, second.Body.String())
				assert.Equal(t, 1, a.count(t, "users"))
				assert.Equal(t, 1, a.count(t, "workspaces"))
			})
		})
	}
}

// AC3: anyone signs up and gets a workspace of their own, as its owner; only the install's first
// user is an instance admin
func TestSignupModes_OpenModeGivesEveryStrangerAWorkspaceOfTheirOwn(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		mail := smtptest.Start(t)
		a := newApp(t, backend, withOpenSignup(mail))
		cfg := a.authConfig(t)
		assert.Equal(t, config.SignupModeOpen, cfg.SignupMode)
		assert.True(t, cfg.SignupOpen)

		first := a.signedUp(t, "first@example.com")
		second := a.signedUp(t, "second@example.com")
		third := a.signedUp(t, "third@example.com")

		seen := map[string]bool{}
		for email, creds := range map[string]credentials{"first@example.com": first, "second@example.com": second, "third@example.com": third} {
			me := a.meOf(t, creds)
			assert.Equal(t, email == "first@example.com", me.User.InstanceAdmin, "%s: only the install's first user manages the install", email)
			require.Len(t, me.Workspaces, 1, email)
			assert.Equal(t, "My workspace", me.Workspaces[0].Name, email)
			assert.Equal(t, "owner", me.Workspaces[0].Role, email)
			seen[me.Workspaces[0].ID] = true
		}
		assert.Len(t, seen, 3, "each has a workspace of their own")
		assert.Equal(t, 3, a.count(t, "workspaces"))
		assert.Equal(t, 3, a.count(t, "workspace_members"))

		// their workspaces are apart: the first user's monitor is not the second's
		decode[created](t, a.request(t, http.MethodPost, monitorsPath, pushMonitorBody("Mine", "first-token"), first), http.StatusCreated)
		assert.Empty(t, decode[[]monitorView](t, a.request(t, http.MethodGet, monitorsPath, nil, second), http.StatusOK))

		// the address of an account is taken, in whatever case
		again := a.signUp(t, "SECOND@example.com")
		assert.Equal(t, http.StatusConflict, again.Code, again.Body.String())
		assert.Equal(t, 3, a.count(t, "users"))

		// each was sent the link that verifies their address
		a.settled(t)
		for _, email := range []string{"first@example.com", "second@example.com", "third@example.com"} {
			assert.Len(t, mailTo(mail, email), 1, email)
		}
	})
}

// Of signups that arrive together on a fresh open install, exactly one is the first: the instance
// admin. The others are strangers with a workspace each.
func TestSignupModes_OfSimultaneousSignupsOnAFreshOpenInstallExactlyOneIsTheInstanceAdmin(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newApp(t, backend, withOpenSignup(smtptest.Start(t)))
		const signups = 6
		var wg sync.WaitGroup
		results := make([]*httpRecorder, signups)
		for i := range signups {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := map[string]any{"email": "user" + string(rune('a'+i)) + "@example.com", "password": adminPassword}
				results[i] = a.request(t, http.MethodPost, registerPath, req, fromAddress("203.0.113."+string(rune('1'+i))))
			}()
		}
		wg.Wait()

		admins := 0
		for i, w := range results {
			session := decode[registered](t, w, http.StatusCreated)
			me := a.meOf(t, bearer(session.AccessToken))
			require.Len(t, me.Workspaces, 1, "signup %d", i)
			assert.Equal(t, "owner", me.Workspaces[0].Role)
			if me.User.InstanceAdmin {
				admins++
			}
		}
		assert.Equal(t, 1, admins)
		assert.Equal(t, signups, a.count(t, "workspaces"))
	})
}

// AC3: someone who signs up through an invite joins the inviting workspace instead, in both modes
func TestSignupModes_AnInviteSignupInOpenModeJoinsTheInvitingWorkspaceAndGetsNoneOfTheirOwn(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newApp(t, backend, withOpenSignup(smtptest.Start(t)))
		owner := a.signedUp(t, "owner@example.com")
		ownerWorkspace := a.meOf(t, owner).Workspaces[0].ID
		invite := a.invite(t, "bob@example.com", "admin", owner)

		w := a.signUpThrough(t, invite.Token, "bob@example.com")

		bob := bearer(decode[registered](t, w, http.StatusCreated).AccessToken)
		me := a.meOf(t, bob)
		assert.False(t, me.User.InstanceAdmin)
		require.Len(t, me.Workspaces, 1, "no workspace of their own")
		assert.Equal(t, ownerWorkspace, me.Workspaces[0].ID)
		assert.Equal(t, "admin", me.Workspaces[0].Role, "the invite's role")
		assert.Equal(t, 1, a.count(t, "workspaces"))
	})
}

type monitorViewWithFlag struct {
	ID     string `json:"id"`
	Active bool   `json:"active"`
}

// the checks that a suspended workspace's monitors don't get: the scheduler's source, and what a probe is dealt
func (a *app) schedulerSource(t *testing.T) *scheduler.DatabaseSource {
	t.Helper()
	var source *scheduler.DatabaseSource
	require.NoError(t, a.container.Invoke(func(monitors monitor.Service, loader *check.Loader, bus events.EventBus) {
		source = scheduler.NewDatabaseSource(monitors, loader, bus)
	}))
	return source
}

func scheduled(t *testing.T, source *scheduler.DatabaseSource) []string {
	t.Helper()
	monitors, err := source.ListActive(context.Background(), 0, 1000)
	require.NoError(t, err)
	ids := make([]string, len(monitors))
	for i, m := range monitors {
		ids[i] = m.ID
	}
	return ids
}

// updates collects the ids of the monitors the scheduler's source is told were updated
type updates struct {
	mu  sync.Mutex
	ids []string
}

func (u *updates) add(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.ids = append(u.ids, id)
}

func (u *updates) seen() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.ids...)
}

// AC4: a new account's workspace holds its monitors, but none runs until its email is verified
func TestSignupModes_ANewWorkspaceIsSuspendedUntilItsOwnerVerifiesTheEmail(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		mail := smtptest.Start(t)
		a := newApp(t, backend, withOpenSignup(mail), func(cfg *config.Config) { cfg.ProbeBootstrapToken = localToken })
		a.signedUp(t, "first@example.com") // the install's first user, so that the owner below is a plain stranger
		owner := a.signedUp(t, "owner@example.com")
		source := a.schedulerSource(t)
		var heard updates
		source.Watch(func(string) {}, heard.add, func(string) {})

		me := a.meOf(t, owner)
		require.Len(t, me.Workspaces, 1)
		assert.True(t, me.Workspaces[0].Suspended)
		require.NotNil(t, me.Workspaces[0].SuspendedReason)
		assert.Equal(t, "unverified", *me.Workspaces[0].SuspendedReason, "the API says why")

		httpID := decode[created](t, a.request(t, http.MethodPost, monitorsPath, httpMonitorBody("Site", ""), owner), http.StatusCreated).ID
		pushID := decode[created](t, a.request(t, http.MethodPost, monitorsPath, pushMonitorBody("Cron", "owner-push"), owner), http.StatusCreated).ID

		// the monitors can be created, are listed as active and read as they are
		listed := decode[[]monitorViewWithFlag](t, a.request(t, http.MethodGet, monitorsPath, nil, owner), http.StatusOK)
		assert.ElementsMatch(t, []monitorViewWithFlag{{ID: httpID, Active: true}, {ID: pushID, Active: true}}, listed)
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, monitorsPath+"/"+httpID, nil, owner).Code)

		// but the scheduler has nothing to check, and a probe nothing to run
		assert.Empty(t, scheduled(t, source))
		found, err := source.Find(context.Background(), httpID)
		require.NoError(t, err)
		assert.Nil(t, found)
		_, err = source.Load(context.Background(), httpID)
		assert.ErrorIs(t, err, check.ErrWorkspaceSuspended)
		a.sayHello(t, localToken, "00000000-0000-4000-8000-000000000001")
		dealt := decodeAssignments(t, a.assignments(t, localToken, "00000000-0000-4000-8000-000000000001", ""))
		assert.Empty(t, dealt.Monitors, "a probe is dealt nothing")

		// a push is refused and stores nothing
		push := a.request(t, http.MethodGet, "/api/v1/push/owner-push?status=up&msg=hello", nil)
		assert.Equal(t, http.StatusForbidden, push.Code, push.Body.String())
		assert.Contains(t, push.Body.String(), "suspended")
		assert.Empty(t, a.storedHeartbeats(t, pushID))

		// following the verification link lifts it
		a.verifyWithMail(t, mail, "owner@example.com")

		me = a.meOf(t, owner)
		assert.False(t, me.Workspaces[0].Suspended)
		assert.Nil(t, me.Workspaces[0].SuspendedReason)
		assert.ElementsMatch(t, []string{httpID, pushID}, scheduled(t, source))
		found, err = source.Find(context.Background(), httpID)
		require.NoError(t, err)
		assert.NotNil(t, found)
		loaded, err := source.Load(context.Background(), httpID)
		require.NoError(t, err)
		assert.Equal(t, httpID, loaded.Monitor.ID)
		dealt = decodeAssignments(t, a.assignments(t, localToken, "00000000-0000-4000-8000-000000000001", ""))
		assert.Equal(t, []string{httpID}, monitorIDs(dealt.Monitors), "a probe runs it now (a push monitor is not dealt)")
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/push/owner-push?status=up&msg=hello", nil).Code)
		assert.Len(t, a.storedHeartbeats(t, pushID), 1)
		// and the scheduler was told to pick up the monitors, by the event it hears for any update
		require.Eventually(t, func() bool { return len(heard.seen()) >= 2 }, 5*time.Second, 10*time.Millisecond)
		assert.ElementsMatch(t, []string{httpID, pushID}, heard.seen())
	})
}

// A reset link proves the address too, so it lifts the suspension as a verification link does
func TestSignupModes_AResetPasswordLinkLiftsTheSuspensionToo(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		mail := smtptest.Start(t)
		a := newApp(t, backend, withOpenSignup(mail))
		a.signedUp(t, "first@example.com")
		owner := a.signedUp(t, "owner@example.com")
		monitorID := decode[created](t, a.request(t, http.MethodPost, monitorsPath, pushMonitorBody("Cron", "owner-push"), owner), http.StatusCreated).ID
		source := a.schedulerSource(t)
		require.Empty(t, scheduled(t, source))
		a.forgot(t, "owner@example.com")
		a.settled(t)
		messages := mailTo(mail, "owner@example.com")
		require.Len(t, messages, 2, "the verification link and the reset link")
		token := tokenIn(t, messages[1], "pkr_")

		require.Equal(t, http.StatusOK, a.reset(t, token, newPassword).Code)

		fresh := a.request(t, http.MethodPost, "/api/v1/auth/login", map[string]any{"email": "owner@example.com", "password": newPassword})
		me := a.meOf(t, bearer(decode[tokens](t, fresh, http.StatusOK).AccessToken))
		assert.False(t, me.Workspaces[0].Suspended)
		assert.Equal(t, []string{monitorID}, scheduled(t, source))
	})
}

// Only the workspaces the user owns, and only this kind of suspension, are lifted: a user who is a
// member of someone else's suspended workspace does not lift it by verifying
func TestSignupModes_VerifyingLiftsOnlyTheVerifiedUsersOwnWorkspaces(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		mail := smtptest.Start(t)
		a := newApp(t, backend, withOpenSignup(mail))
		a.signedUp(t, "first@example.com")
		owner := a.signedUp(t, "owner@example.com")
		invite := a.invite(t, "member@example.com", "member", owner)
		member := bearer(decode[registered](t, a.signUpThrough(t, invite.Token, "member@example.com"), http.StatusCreated).AccessToken)
		other := a.signedUp(t, "other@example.com")

		a.verifyWithMail(t, mail, "member@example.com")

		assert.True(t, a.meOf(t, member).Workspaces[0].Suspended, "the workspace is the owner's to lift")
		assert.True(t, a.meOf(t, owner).Workspaces[0].Suspended)
		assert.True(t, a.meOf(t, other).Workspaces[0].Suspended)

		a.verifyWithMail(t, mail, "owner@example.com")

		assert.False(t, a.meOf(t, owner).Workspaces[0].Suspended)
		assert.False(t, a.meOf(t, member).Workspaces[0].Suspended)
		assert.True(t, a.meOf(t, other).Workspaces[0].Suspended, "another stranger's workspace stays suspended")
	})
}

// A user who has not verified their address can not get around the suspension by making a
// workspace; once they have, the workspaces they make run
func TestSignupModes_AWorkspaceAnUnverifiedUserMakesIsSuspendedToo(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		mail := smtptest.Start(t)
		a := newApp(t, backend, withOpenSignup(mail))
		a.signedUp(t, "first@example.com")
		owner := a.signedUp(t, "owner@example.com")

		made := a.request(t, http.MethodPost, workspacesPath, map[string]any{"name": "Another"}, owner)
		require.Equal(t, http.StatusCreated, made.Code, made.Body.String())

		me := a.meOf(t, owner)
		require.Len(t, me.Workspaces, 2)
		assert.True(t, me.Workspaces[0].Suspended)
		assert.True(t, me.Workspaces[1].Suspended)

		a.verifyWithMail(t, mail, "owner@example.com")
		assert.False(t, a.meOf(t, owner).Workspaces[0].Suspended)
		assert.False(t, a.meOf(t, owner).Workspaces[1].Suspended, "the verification lifts every workspace the user owns")
		decode[json.RawMessage](t, a.request(t, http.MethodPost, workspacesPath, map[string]any{"name": "Third"}, owner), http.StatusCreated)
		me = a.meOf(t, owner)
		require.Len(t, me.Workspaces, 3)
		for _, w := range me.Workspaces {
			assert.False(t, w.Suspended, w.Name)
		}
	})
}

func TestSignupModes_InInviteModeAWorkspaceTheUserMakesRuns(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newApp(t, backend, withSignupMode(config.SignupModeInvite))
		first := a.signedUp(t, "first@example.com")

		decode[json.RawMessage](t, a.request(t, http.MethodPost, workspacesPath, map[string]any{"name": "Another"}, first), http.StatusCreated)

		for _, w := range a.meOf(t, first).Workspaces {
			assert.False(t, w.Suspended, w.Name)
		}
	})
}

// AC5: five signups an hour from one client address; an invite signup is not counted
func TestSignupModes_OpenSignupsAreLimitedPerClientAddress(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newApp(t, backend, withOpenSignup(smtptest.Start(t)))
		owner := a.signedUp(t, "owner@example.com") // from the default address of a test request, which is a client of its own
		invite := a.invite(t, "invited@example.com", "member", owner)
		busy, quiet := fromAddress("203.0.113.7"), fromAddress("203.0.113.8")
		for i := range 5 {
			w := a.signUp(t, "bot"+string(rune('a'+i))+"@example.com", busy)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		}

		sixth := a.signUp(t, "botf@example.com", busy)

		assert.Equal(t, http.StatusTooManyRequests, sixth.Code, sixth.Body.String())
		assert.Equal(t, 6, a.count(t, "users"), "the sixth made no account")
		assert.Equal(t, http.StatusCreated, a.signUp(t, "someone@example.com", quiet).Code, "another client is not held back")
		through := a.request(t, http.MethodPost, registerPath, map[string]any{"email": "invited@example.com", "password": adminPassword, "inviteToken": invite.Token}, busy)
		assert.Equal(t, http.StatusCreated, through.Code, "an invite is the proof that the visitor is wanted: "+through.Body.String())
	})
}

// A fake Cloudflare siteverify: "good" passes, everything else is refused
func fakeSiteverify(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("secret") == turnstileSecret && r.PostForm.Get("response") == "good" {
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func (a *app) signUpWithAnswer(t *testing.T, email, answer string, creds ...credentials) *httpRecorder {
	t.Helper()
	body := map[string]any{"email": email, "password": adminPassword}
	if answer != "" {
		body["turnstileToken"] = answer
	}
	return a.request(t, http.MethodPost, registerPath, body, creds...)
}

// AC5: with Turnstile set, open signup needs an answer the server checks
func TestSignupModes_WithTurnstileOpenSignupNeedsAnAnswerThatCloudflareAccepts(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		siteverify := fakeSiteverify(t)
		a := newApp(t, backend, withOpenSignup(smtptest.Start(t)), withTurnstile(siteverify.URL))
		require.NotNil(t, a.authConfig(t).TurnstileSiteKey)
		assert.Equal(t, turnstileSiteKey, *a.authConfig(t).TurnstileSiteKey, "the app shows the widget with it")

		missing := a.signUpWithAnswer(t, "a@example.com", "")
		refused := a.signUpWithAnswer(t, "b@example.com", "forged")
		assert.Equal(t, http.StatusBadRequest, missing.Code, missing.Body.String())
		assert.Equal(t, http.StatusBadRequest, refused.Code, refused.Body.String())
		assert.Equal(t, 0, a.count(t, "users"), "no account for an answer that was not accepted")
		assert.NotContains(t, refused.Body.String(), turnstileSecret)

		passed := a.signUpWithAnswer(t, "c@example.com", "good")
		assert.Equal(t, http.StatusCreated, passed.Code, passed.Body.String())
		assert.Equal(t, 1, a.count(t, "users"))
	})
}

func TestSignupModes_WithTurnstileACloudflareThatCanNotBeReachedRefusesTheSignup(t *testing.T) {
	siteverify := fakeSiteverify(t)
	url := siteverify.URL
	siteverify.Close()
	a := newApp(t, withOpenSignup(smtptest.Start(t)), withTurnstile(url))

	w := a.signUpWithAnswer(t, "a@example.com", "good")

	assert.Equal(t, http.StatusBadRequest, w.Code, "fail closed: "+w.Body.String())
	assert.Equal(t, 0, a.count(t, "users"))
}

func TestSignupModes_AnInviteSignupNeedsNoTurnstileAnswer(t *testing.T) {
	siteverify := fakeSiteverify(t)
	a := newApp(t, withOpenSignup(smtptest.Start(t)), withTurnstile(siteverify.URL))
	owner := a.signedUp2(t, "owner@example.com", "good")
	invite := a.invite(t, "invited@example.com", "member", owner)

	w := a.signUpThrough(t, invite.Token, "invited@example.com")

	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func (a *app) signedUp2(t *testing.T, email, answer string) credentials {
	t.Helper()
	session := decode[registered](t, a.signUpWithAnswer(t, email, answer), http.StatusCreated)
	return bearer(session.AccessToken)
}

// Turnstile guards open signup only: invite mode asks for no answer, and does not show a widget
func TestSignupModes_InInviteModeTurnstileIsNotAskedFor(t *testing.T) {
	siteverify := fakeSiteverify(t)
	a := newApp(t, withSignupMode(config.SignupModeInvite), withTurnstile(siteverify.URL))
	assert.Nil(t, a.authConfig(t).TurnstileSiteKey)

	w := a.signUp(t, "first@example.com")

	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func TestSignupModes_TheConfigSaysWhoMaySignUp(t *testing.T) {
	t.Run("invite, no user yet", func(t *testing.T) {
		a := newApp(t, withSignupMode(config.SignupModeInvite))
		assert.Equal(t, signupConfig{SignupMode: "invite", SignupOpen: true}, a.authConfig(t))
	})
	t.Run("invite, with a user", func(t *testing.T) {
		a := newApp(t, withSignupMode(config.SignupModeInvite))
		a.signedUp(t, "first@example.com")
		assert.Equal(t, signupConfig{SignupMode: "invite", SignupOpen: false}, a.authConfig(t))
	})
	t.Run("open", func(t *testing.T) {
		a := newApp(t, withOpenSignup(smtptest.Start(t)))
		a.signedUp(t, "first@example.com")
		got := a.authConfig(t)
		assert.Equal(t, "open", got.SignupMode)
		assert.True(t, got.SignupOpen)
		assert.True(t, got.AccountEmails)
		assert.Nil(t, got.TurnstileSiteKey)
	})
}
