package apitest

import (
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// PP-152: account emails (a password reset link, an email verification link, emailed invites) are
// sent by one sender for the install, set by ACCOUNT_SMTP_*. Without it, nothing changes.

const (
	senderAddress = "no-reply@peek.example"
	newPassword   = "Battery-staple-2"

	configPath       = "/api/v1/auth/config"
	forgotPath       = "/api/v1/auth/forgot-password"
	resetPath        = "/api/v1/auth/reset-password"
	verifyPath       = "/api/v1/auth/verify-email"
	resendVerifyPath = "/api/v1/auth/verify-email/resend"
)

// withAccountSMTP gives the app a sender: the fake SMTP server
func withAccountSMTP(server *smtptest.Server) func(*config.Config) {
	return func(cfg *config.Config) {
		cfg.AccountSMTPHost, cfg.AccountSMTPPort, cfg.AccountSMTPTLS = server.Host(), server.Port(), "none"
		cfg.AccountSMTPUser, cfg.AccountSMTPPassword, cfg.AccountSMTPFrom = "mailer", "secret", "Peekaping <"+senderAddress+">"
	}
}

// forEachDatabaseWithMail runs fn on SQLite and, when PEE_TEST_POSTGRES_DSN is set, PostgreSQL,
// with an app that has the admin signed up and a fake SMTP server that is its sender. The admin's
// verification email has been taken when fn starts, and mail.Messages() holds just that.
func forEachDatabaseWithMail(t *testing.T, fn func(t *testing.T, a *app, mail *smtptest.Server)) {
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
		t.Run(backend.name, func(t *testing.T) {
			server := smtptest.Start(t)
			// In invite mode, whatever the run's default: these tests are about the emails, and the admin
			// they start from has not verified their address
			a := newAppWithAdmin(t, backend.option, withAccountSMTP(server), withSignupMode(config.SignupModeInvite))
			server.WaitFor(t, 1)
			fn(t, a, server)
		})
	}
}

// settled waits for the emails the app is sending in the background
func (a *app) settled(t *testing.T) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(m *mailer.Mailer) { m.Wait() }))
}

// textOf returns the plain text of a message the fake server took
func textOf(t *testing.T, message smtptest.Message) string {
	t.Helper()
	parsed, err := mail.ReadMessage(strings.NewReader(message.Data))
	require.NoError(t, err)
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	if mediaType == "text/plain" {
		body, err := io.ReadAll(quotedprintable.NewReader(parsed.Body))
		require.NoError(t, err)
		return string(body)
	}
	require.Equal(t, "multipart/alternative", mediaType)
	part, err := multipart.NewReader(parsed.Body, params["boundary"]).NextPart()
	require.NoError(t, err)
	body, err := io.ReadAll(quotedprintable.NewReader(part))
	require.NoError(t, err)
	return string(body)
}

// tokenIn returns the token with the prefix that a message's text carries
func tokenIn(t *testing.T, message smtptest.Message, prefix string) string {
	t.Helper()
	token := regexp.MustCompile(regexp.QuoteMeta(prefix) + `[A-Za-z0-9_-]+`).FindString(textOf(t, message))
	require.NotEmpty(t, token, "no %s token in %q", prefix, textOf(t, message))
	return token
}

func subjectOf(t *testing.T, message smtptest.Message) string {
	t.Helper()
	parsed, err := mail.ReadMessage(strings.NewReader(message.Data))
	require.NoError(t, err)
	return parsed.Header.Get("Subject")
}

// mailTo returns the messages sent to the address, oldest first
func mailTo(server *smtptest.Server, address string) []smtptest.Message {
	var out []smtptest.Message
	for _, m := range server.Messages() {
		if len(m.To) == 1 && m.To[0] == address {
			out = append(out, m)
		}
	}
	return out
}

type accountMe struct {
	User struct {
		EmailVerified bool `json:"emailVerified"`
	} `json:"user"`
	AccountEmails bool `json:"accountEmails"`
}

func (a *app) me(t *testing.T, creds credentials) accountMe {
	t.Helper()
	return decode[accountMe](t, a.request(t, http.MethodGet, "/api/v1/me", nil, creds), http.StatusOK)
}

// expireToken moves the expiry of the account tokens of a purpose into the past
func (a *app) expireTokens(t *testing.T, purpose string) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		_, err := db.Exec("UPDATE account_tokens SET expires_at = ? WHERE purpose = ?", time.Now().UTC().Add(-time.Minute), purpose)
		return err
	}))
}

func (a *app) forgot(t *testing.T, email string) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, forgotPath, map[string]any{"email": email})
}

func (a *app) reset(t *testing.T, token, password string) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, resetPath, map[string]any{"token": token, "password": password})
}

// AC1: the sender is the install's, and the public config says whether there is one
func TestAccountEmails_TheConfigSaysWhetherTheInstallHasASender(t *testing.T) {
	type authConfig struct {
		AccountEmails bool `json:"accountEmails"`
	}
	t.Run("unset: off", func(t *testing.T) {
		a := newAppWithAdmin(t, selfHosted)

		assert.False(t, decode[authConfig](t, a.request(t, http.MethodGet, configPath, nil), http.StatusOK).AccountEmails, "public: it takes no credentials")
		assert.False(t, a.me(t, a.asAdmin).AccountEmails)
	})
	t.Run("set: on", func(t *testing.T) {
		a := newAppWithAdmin(t, withAccountSMTP(smtptest.Start(t)), withSignupMode(config.SignupModeInvite))

		assert.True(t, decode[authConfig](t, a.request(t, http.MethodGet, configPath, nil), http.StatusOK).AccountEmails)
		assert.True(t, a.me(t, a.asAdmin).AccountEmails)
	})
}

// AC5: without a sender nothing changes
func TestAccountEmails_WithoutASenderThereIsNothingToSendOrVerify(t *testing.T) {
	forEachDatabaseWith(t, []func(*config.Config){selfHosted}, func(t *testing.T, a *app) {
		assert.Equal(t, http.StatusNotFound, a.forgot(t, adminEmail).Code, "no reset link without a sender")
		assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodPost, resendVerifyPath, nil, a.asAdmin).Code)

		me := a.me(t, a.asAdmin)
		assert.False(t, me.AccountEmails, "so the app shows no verification banner")
		assert.Equal(t, 0, a.count(t, "account_tokens"), "no one was asked to verify")

		created := a.invite(t, "bob@example.com", "member", a.asAdmin)
		assert.NotEmpty(t, created.Link, "an invite is copied")
		a.settled(t)
	})
}

// AC2: the reset link answers the same for every address, and sends only for an account
func TestForgotPassword_AnswersTheSameForAnAddressWithoutAnAccountAndOnlyAnAccountGetsMail(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		unknown := a.forgot(t, "nobody@example.com")
		known := a.forgot(t, adminEmail)
		a.settled(t)

		assert.Equal(t, http.StatusAccepted, unknown.Code, unknown.Body.String())
		assert.Equal(t, unknown.Code, known.Code)
		assert.Equal(t, unknown.Body.String(), known.Body.String(), "the same body")

		messages := mail.Messages()
		require.Len(t, messages, 2, "the admin's verification link and the reset link, and nothing for the unknown address")
		reset := mailTo(mail, adminEmail)[1]
		assert.Equal(t, senderAddress, reset.From)
		assert.Equal(t, "Reset your Peekaping password", subjectOf(t, reset))
		token := tokenIn(t, reset, "pkr_")
		assert.Contains(t, textOf(t, reset), clientURL+"/reset-password/"+token, "the link opens the web app")
		assert.Contains(t, textOf(t, reset), "expires in 1 hour")
		assert.Empty(t, mailTo(mail, "nobody@example.com"))
	})
}

func TestForgotPassword_AnAddressInAnotherCaseFindsTheAccountAndAMalformedOneIsRefused(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		assert.Equal(t, http.StatusAccepted, a.forgot(t, "ADMIN@example.com").Code)
		a.settled(t)
		assert.Len(t, mailTo(mail, adminEmail), 2, "the verification link and the reset link")

		assert.Equal(t, http.StatusBadRequest, a.forgot(t, "not an email").Code)
		assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, forgotPath, map[string]any{}).Code)
	})
}

func TestForgotPassword_TheTokenIsStoredOnlyAsAHash(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		a.forgot(t, adminEmail)
		a.settled(t)
		token := tokenIn(t, mailTo(mail, adminEmail)[1], "pkr_")

		var stored []string
		require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
			return db.NewSelect().Table("account_tokens").Column("token_hash").Scan(context.Background(), &stored)
		}))
		require.NotEmpty(t, stored)
		for _, hash := range stored {
			assert.NotContains(t, hash, token)
			assert.True(t, strings.HasPrefix(hash, "sha256$"), hash)
		}
	})
}

// AC2: using the link sets the password once, and ends every session
func TestResetPassword_SetsThePasswordOnceAndEndsEverySession(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		old := decode[tokens](t, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword}), http.StatusOK)
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(old.AccessToken)).Code)
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, a.asAdmin).Code)
		a.forgot(t, adminEmail)
		a.settled(t)
		token := tokenIn(t, mailTo(mail, adminEmail)[1], "pkr_")

		done := a.reset(t, token, newPassword)

		require.Equal(t, http.StatusOK, done.Code, done.Body.String())
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refreshToken": old.RefreshToken}).Code, "refresh tokens issued before are refused")
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(old.AccessToken)).Code, "and so are access tokens")
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/me", nil, a.asAdmin).Code, "the session from signing up too")
		assert.Equal(t, http.StatusUnauthorized, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword}).Code, "the old password is gone")

		fresh := decode[tokens](t, a.login(t, map[string]any{"email": adminEmail, "password": newPassword}), http.StatusOK)
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(fresh.AccessToken)).Code, "signing in after the reset works, at once")
		refreshed := decode[tokens](t, a.request(t, http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refreshToken": fresh.RefreshToken}), http.StatusOK)
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(refreshed.AccessToken)).Code)

		again := a.reset(t, token, "Another-secret-3")
		assert.Equal(t, http.StatusBadRequest, again.Code, "a link works once")
		assert.Equal(t, http.StatusOK, a.login(t, map[string]any{"email": adminEmail, "password": newPassword}).Code, "and the second use changed nothing")
	})
}

func TestResetPassword_RefusesAnExpiredUnknownOrForeignToken(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		a.forgot(t, adminEmail)
		a.settled(t)
		reset := tokenIn(t, mailTo(mail, adminEmail)[1], "pkr_")
		verify := tokenIn(t, mailTo(mail, adminEmail)[0], "pkv_")

		assert.Equal(t, http.StatusBadRequest, a.reset(t, "pkr_"+strings.Repeat("A", 43), newPassword).Code, "unknown")
		assert.Equal(t, http.StatusBadRequest, a.reset(t, "nonsense", newPassword).Code, "not a token")
		assert.Equal(t, http.StatusBadRequest, a.reset(t, verify, newPassword).Code, "a verification token is not a reset token")

		a.expireTokens(t, "reset")
		expired := a.reset(t, reset, newPassword)
		assert.Equal(t, http.StatusBadRequest, expired.Code, "expired")
		assert.Contains(t, expired.Body.String(), "invalid or has expired")
		assert.Equal(t, http.StatusOK, a.login(t, map[string]any{"email": adminEmail, "password": adminPassword}).Code, "nothing changed")
	})
}

// The link came to the address, so following it proves the address as a verification link does
func TestResetPassword_AlsoVerifiesAnAddressThatWasNotVerified(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		require.False(t, a.me(t, a.asAdmin).User.EmailVerified)
		a.forgot(t, adminEmail)
		a.settled(t)
		token := tokenIn(t, mailTo(mail, adminEmail)[1], "pkr_")

		require.Equal(t, http.StatusOK, a.reset(t, token, newPassword).Code)

		fresh := decode[tokens](t, a.login(t, map[string]any{"email": adminEmail, "password": newPassword}), http.StatusOK)
		assert.True(t, a.me(t, bearer(fresh.AccessToken)).User.EmailVerified)
	})
}

// A token that is not live is answered before the password is hashed: bcrypt refuses a password
// longer than 72 bytes, so a request that reached it would answer 500 instead of 400
func TestResetPassword_ARefusedTokenIsAnsweredBeforeThePasswordIsHashed(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		tooLongForBcrypt := "Aa1!" + strings.Repeat("x", 80)

		unknown := a.reset(t, "pkr_"+strings.Repeat("A", 43), tooLongForBcrypt)

		assert.Equal(t, http.StatusBadRequest, unknown.Code, unknown.Body.String())
		assert.Contains(t, unknown.Body.String(), "invalid or has expired")
	})
}

func TestResetPassword_AWeakPasswordIsRefusedAndTheLinkStillWorks(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		a.forgot(t, adminEmail)
		a.settled(t)
		token := tokenIn(t, mailTo(mail, adminEmail)[1], "pkr_")

		weak := a.reset(t, token, "short")
		assert.Equal(t, http.StatusBadRequest, weak.Code)
		assert.Contains(t, weak.Body.String(), "Password must be at least 8 characters")

		assert.Equal(t, http.StatusOK, a.reset(t, token, newPassword).Code)
	})
}

func TestResetPassword_UsingOneLinkEndsTheOthers(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		a.forgot(t, adminEmail)
		a.forgot(t, adminEmail)
		a.settled(t)
		sent := mailTo(mail, adminEmail)
		require.Len(t, sent, 3)
		first, second := tokenIn(t, sent[1], "pkr_"), tokenIn(t, sent[2], "pkr_")
		require.NotEqual(t, first, second)

		require.Equal(t, http.StatusOK, a.reset(t, first, newPassword).Code)

		assert.Equal(t, http.StatusBadRequest, a.reset(t, second, "Another-secret-3").Code, "the other link was deleted")
	})
}

func TestResetPassword_ConcurrentUsesOfOneLinkSucceedOnce(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		a.forgot(t, adminEmail)
		a.settled(t)
		token := tokenIn(t, mailTo(mail, adminEmail)[1], "pkr_")
		const attempts = 6

		codes := make(chan int, attempts)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range attempts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				codes <- a.reset(t, token, newPassword).Code
			}()
		}
		close(start)
		wg.Wait()
		close(codes)

		succeeded := 0
		for code := range codes {
			if code == http.StatusOK {
				succeeded++
			} else {
				assert.Equal(t, http.StatusBadRequest, code)
			}
		}
		assert.Equal(t, 1, succeeded, "uses that worked")
	})
}

func TestForgotPassword_AtMostFiveEmailsAnHourPerAddressAndTwentyPerClient(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		for range 7 {
			assert.Equal(t, http.StatusAccepted, a.forgot(t, adminEmail).Code, "the answer does not change, so it reveals nothing")
		}
		a.settled(t)
		assert.Len(t, mailTo(mail, adminEmail), 1+5, "the verification link and five reset links")

		// the client has asked 7 times; the 20th request in the hour is the last
		for i := range 13 {
			require.Equal(t, http.StatusAccepted, a.forgot(t, fmt.Sprintf("someone%d@example.com", i)).Code, "request %d", i+8)
		}
		limited := a.forgot(t, "one-more@example.com")
		assert.Equal(t, http.StatusTooManyRequests, limited.Code, limited.Body.String())
		a.settled(t)
	})
}

func TestResetPasswordAndVerifyEmail_AreLimitedToTwentyAttemptsAnHourPerClient(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		unknown := "pkr_" + strings.Repeat("A", 43)
		for i := range 20 {
			require.Equal(t, http.StatusBadRequest, a.reset(t, unknown, newPassword).Code, "attempt %d", i+1)
		}

		limited := a.reset(t, unknown, newPassword)
		assert.Equal(t, http.StatusTooManyRequests, limited.Code, limited.Body.String())

		// verify-email has a count of its own
		unknownVerify := map[string]any{"token": "pkv_" + strings.Repeat("A", 43)}
		for i := range 20 {
			require.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, verifyPath, unknownVerify).Code, "attempt %d", i+1)
		}
		assert.Equal(t, http.StatusTooManyRequests, a.request(t, http.MethodPost, verifyPath, unknownVerify).Code)

		// another client is not affected
		other := func(req *http.Request) { req.RemoteAddr = "198.51.100.9:4000" }
		assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, resetPath, map[string]any{"token": unknown, "password": newPassword}, other).Code)
	})
}

// A request whose address cannot be read is counted under that raw address, not under one bucket
// shared with every other such request
func TestResetPassword_RequestsWithoutAClientAddressAreCountedByTheirRawAddress(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		unknown := map[string]any{"token": "pkr_" + strings.Repeat("A", 43), "password": newPassword}
		from := func(remote string) credentials { return func(req *http.Request) { req.RemoteAddr = remote } }
		for i := range 20 {
			require.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, resetPath, unknown, from("@peek-a")).Code, "attempt %d", i+1)
		}

		assert.Equal(t, http.StatusTooManyRequests, a.request(t, http.MethodPost, resetPath, unknown, from("@peek-a")).Code, "the same raw address is limited")
		assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, resetPath, unknown, from("@peek-b")).Code, "another raw address is not")
	})
}

// AC3: every new account gets a link, and following it records when
func TestVerifyEmail_TheFirstUserGetsALinkAndFollowingItVerifiesTheAccount(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		require.Equal(t, 1, len(mail.Messages()))
		sent := mail.Messages()[0]
		assert.Equal(t, []string{adminEmail}, sent.To)
		assert.Equal(t, "Verify your email address for Peekaping", subjectOf(t, sent))
		token := tokenIn(t, sent, "pkv_")
		assert.Contains(t, textOf(t, sent), clientURL+"/verify-email/"+token)
		assert.Contains(t, textOf(t, sent), "expires in 3 days")
		before := a.me(t, a.asAdmin)
		assert.True(t, before.AccountEmails)
		assert.False(t, before.User.EmailVerified)

		done := a.request(t, http.MethodPost, verifyPath, map[string]any{"token": token})

		require.Equal(t, http.StatusOK, done.Code, done.Body.String())
		assert.True(t, a.me(t, a.asAdmin).User.EmailVerified)
		var verifiedAt *time.Time
		require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
			return db.NewSelect().Table("users").Column("email_verified_at").Where("email = ?", adminEmail).Scan(context.Background(), &verifiedAt)
		}))
		require.NotNil(t, verifiedAt, "the account records when it was verified")
		assert.WithinDuration(t, time.Now(), *verifiedAt, time.Minute)

		assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, verifyPath, map[string]any{"token": token}).Code, "a link works once")
	})
}

func TestVerifyEmail_RefusesAnExpiredUnknownOrForeignToken(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		token := tokenIn(t, mail.Messages()[0], "pkv_")
		a.forgot(t, adminEmail)
		a.settled(t)
		reset := tokenIn(t, mailTo(mail, adminEmail)[1], "pkr_")

		assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, verifyPath, map[string]any{"token": "pkv_" + strings.Repeat("A", 43)}).Code, "unknown")
		assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, verifyPath, map[string]any{"token": reset}).Code, "a reset token is not a verification token")
		a.expireTokens(t, "verify")
		assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, verifyPath, map[string]any{"token": token}).Code, "expired")
		assert.False(t, a.me(t, a.asAdmin).User.EmailVerified)
	})
}

func TestVerifyEmail_ResendSendsANewLinkAndRetiresTheOldOne(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		first := tokenIn(t, mail.Messages()[0], "pkv_")

		answer := a.request(t, http.MethodPost, resendVerifyPath, nil, a.asAdmin)

		require.Equal(t, http.StatusAccepted, answer.Code, answer.Body.String())
		a.settled(t)
		sent := mailTo(mail, adminEmail)
		require.Len(t, sent, 2)
		second := tokenIn(t, sent[1], "pkv_")
		assert.NotEqual(t, first, second)
		assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, verifyPath, map[string]any{"token": first}).Code, "the earlier link stopped working")
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodPost, verifyPath, map[string]any{"token": second}).Code)
	})
}

func TestVerifyEmail_ResendIsRefusedWhenVerifiedAndWithoutSignIn(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, resendVerifyPath, nil).Code, "it takes a signed-in user")
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, resendVerifyPath, nil, a.withAPIKey).Code, "and not an API key")
		require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, verifyPath, map[string]any{"token": tokenIn(t, mail.Messages()[0], "pkv_")}).Code)

		refused := a.request(t, http.MethodPost, resendVerifyPath, nil, a.asAdmin)

		assert.Equal(t, http.StatusConflict, refused.Code, refused.Body.String())
		a.settled(t)
		assert.Len(t, mail.Messages(), 1, "no new link")
	})
}

func TestVerifyEmail_ResendIsLimitedToFivePerHour(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		for i := range 5 {
			require.Equal(t, http.StatusAccepted, a.request(t, http.MethodPost, resendVerifyPath, nil, a.asAdmin).Code, "request %d", i+1)
		}

		limited := a.request(t, http.MethodPost, resendVerifyPath, nil, a.asAdmin)

		assert.Equal(t, http.StatusTooManyRequests, limited.Code, limited.Body.String())
		a.settled(t)
		assert.Len(t, mail.Messages(), 1+5)
	})
}

func TestVerifyEmail_ASignupThroughAnInviteGetsALinkToo(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		created := a.invite(t, "bob@example.com", "member", a.asAdmin)

		session := decode[registered](t, a.signUpThrough(t, created.Token, "bob@example.com"), http.StatusCreated)

		a.settled(t)
		sent := mailTo(mail, "bob@example.com")
		require.Len(t, sent, 2, "the invite and the verification link")
		token := tokenIn(t, sent[1], "pkv_")
		assert.False(t, a.me(t, bearer(session.AccessToken)).User.EmailVerified)
		require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, verifyPath, map[string]any{"token": token}).Code)
		assert.True(t, a.me(t, bearer(session.AccessToken)).User.EmailVerified)
		assert.False(t, a.me(t, a.asAdmin).User.EmailVerified, "and the admin's address is still not")
	})
}

func TestVerifyEmail_ASendFailureDoesNotFailTheSignup(t *testing.T) {
	server := smtptest.Start(t)
	server.RejectMessages("554 5.7.1 no")
	a := newApp(t, withAccountSMTP(server))

	w := a.request(t, http.MethodPost, registerPath, map[string]any{"email": adminEmail, "password": adminPassword})

	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	a.settled(t)
	assert.Empty(t, server.Messages())
}

func TestVerifyEmail_ASenderThatIsDownDoesNotFailTheSignup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	a := newApp(t, func(cfg *config.Config) {
		cfg.AccountSMTPHost, cfg.AccountSMTPPort, cfg.AccountSMTPTLS, cfg.AccountSMTPFrom = "127.0.0.1", port, "none", senderAddress
	}, withSignupMode(config.SignupModeInvite))

	w := a.request(t, http.MethodPost, registerPath, map[string]any{"email": adminEmail, "password": adminPassword})

	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	a.settled(t)
}

func TestVerifyEmail_WithoutASenderASignupAsksNoOne(t *testing.T) {
	forEachDatabaseWith(t, []func(*config.Config){selfHosted}, func(t *testing.T, a *app) {
		created := a.invite(t, "bob@example.com", "member", a.asAdmin)
		session := decode[registered](t, a.signUpThrough(t, created.Token, "bob@example.com"), http.StatusCreated)

		a.settled(t)
		me := a.me(t, bearer(session.AccessToken))
		assert.False(t, me.AccountEmails)
		assert.Equal(t, 0, a.count(t, "account_tokens"))
	})
}

// AC4: an invite is also emailed, and the copyable link stays
func TestInvites_WithASenderTheInviteIsEmailedAndTheLinkStaysInTheAnswer(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		created := a.invite(t, "Bob@Example.com", "admin", a.asAdmin)

		require.NotEmpty(t, created.Link, "the copyable link stays")
		a.settled(t)
		sent := mailTo(mail, "bob@example.com")
		require.Len(t, sent, 1)
		text := textOf(t, sent[0])
		assert.Contains(t, text, created.Link)
		assert.Contains(t, text, adminEmail+" invited you")
		assert.Contains(t, text, "as an admin")
		assert.Contains(t, text, "My workspace")
		assert.Contains(t, text, "expires in 7 days")
		assert.Contains(t, subjectOf(t, sent[0]), "My workspace")
		assert.Equal(t, 1, len(mail.Messages())-1, "only the invite, besides the admin's verification link")
	})
}

func TestInvites_AWorkspaceNameWithLineBreaksStillGetsItsInviteEmailed(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		made := decode[workspaceResponse](t, a.request(t, http.MethodPost, workspacesURL,
			map[string]any{"name": "Side\r\nBcc: victim@example.com\tproject"}, a.asAdmin), http.StatusCreated)

		w := a.request(t, http.MethodPost, invitesPath, map[string]any{"email": "bob@example.com", "role": "member"}, a.asAdmin, inWorkspace(workspace.ID(made.ID)))
		decode[createdInviteResponse](t, w, http.StatusCreated)

		a.settled(t)
		sent := mailTo(mail, "bob@example.com")
		require.Len(t, sent, 1, "a line break in the name must not stop the invite email")
		assert.Equal(t, "You're invited to Side  Bcc: victim@example.com project on Peekaping", subjectOf(t, sent[0]))
		assert.NotContains(t, sent[0].Data, "\r\nBcc:", "and must not add a header")
	})
}

func TestInvites_AnInviteMadeWithAnAPIKeyIsEmailedWithoutAnInviter(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		created := a.invite(t, "bob@example.com", "member", a.withAPIKey)

		a.settled(t)
		sent := mailTo(mail, "bob@example.com")
		require.Len(t, sent, 1)
		assert.Contains(t, textOf(t, sent[0]), "You were invited to join")
		assert.Contains(t, textOf(t, sent[0]), created.Link)
	})
}

func TestInvites_ASendFailureStillAnswersWithTheLink(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		mail.RejectMessages("554 5.7.1 no")

		created := a.invite(t, "bob@example.com", "member", a.asAdmin)

		assert.NotEmpty(t, created.Link)
		a.settled(t)
		assert.Empty(t, mailTo(mail, "bob@example.com"))
	})
}
