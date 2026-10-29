package apitest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// PP-373: an instance admin changes the install settings in the web app: the signup mode, the
// sender of account emails and the public URL. A variable the environment sets wins and locks its
// setting (docs/adr/0005 section 5).

const (
	adminSettingsPath  = "/api/v1/admin/settings"
	adminTestEmailPath = "/api/v1/admin/settings/test-email"
	// smtpPassword is the password the tests save; no answer may carry it
	smtpPassword = "smtp-pass-0123456789"
)

type settingValue[T any] struct {
	Value  T      `json:"value"`
	Source string `json:"source"`
	Locked bool   `json:"locked"`
	EnvVar string `json:"envVar"`
}

type passwordSetting struct {
	Set    bool   `json:"set"`
	Source string `json:"source"`
	Locked bool   `json:"locked"`
	EnvVar string `json:"envVar"`
}

type installSettings struct {
	SignupMode  settingValue[string] `json:"signupMode"`
	PublicURL   settingValue[string] `json:"publicUrl"`
	AccountSMTP struct {
		Host     settingValue[string] `json:"host"`
		Port     settingValue[int]    `json:"port"`
		User     settingValue[string] `json:"user"`
		Password passwordSetting      `json:"password"`
		From     settingValue[string] `json:"from"`
		TLS      settingValue[string] `json:"tls"`
	} `json:"accountSmtp"`
	AccountEmails bool `json:"accountEmails"`
}

type settingError struct {
	Message string `json:"message"`
	Error   struct {
		Code  string `json:"code"`
		Field string `json:"field"`
	} `json:"error"`
}

// withEnvironment marks variables as set by the server's environment, which locks their settings
func withEnvironment(names ...string) func(*config.Config) {
	return func(cfg *config.Config) { cfg.SetFromEnvironment(names...) }
}

// withSettingsCacheLifetime shortens how long the app keeps the install settings it read
func withSettingsCacheLifetime(lifetime time.Duration) func(*config.Config) {
	return func(cfg *config.Config) { cfg.InstallSettingsCacheLifetime = lifetime }
}

// senderBody is the PUT body that saves the fake SMTP server as the sender
func senderBody(server *smtptest.Server) map[string]any {
	return map[string]any{
		"host": server.Host(), "port": server.Port(), "tls": "none",
		"user": "mailer", "password": smtpPassword, "from": "Saved <saved@peek.example>",
	}
}

func (a *app) installSettings(t *testing.T) installSettings {
	t.Helper()
	return decode[installSettings](t, a.request(t, http.MethodGet, adminSettingsPath, nil, a.asAdmin), http.StatusOK)
}

func (a *app) putSettings(t *testing.T, body map[string]any) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPut, adminSettingsPath, body, a.asAdmin)
}

func (a *app) savedSettings(t *testing.T, body map[string]any) installSettings {
	t.Helper()
	return decode[installSettings](t, a.putSettings(t, body), http.StatusOK)
}

func settingRefusal(t *testing.T, w *httpRecorder, status int) settingError {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	var out settingError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

// MARK: Sources

func TestAdminSettings_AnInstallThatSetsNothingShowsTheDefaults(t *testing.T) {
	a := newAppWithAdmin(t, selfHosted)

	got := a.installSettings(t)

	assert.Equal(t, settingValue[string]{Value: "invite", Source: "default", EnvVar: "SIGNUP_MODE"}, got.SignupMode)
	assert.Equal(t, settingValue[string]{Value: clientURL, Source: "default", EnvVar: "CLIENT_URL"}, got.PublicURL)
	assert.Equal(t, settingValue[string]{Value: "", Source: "default", EnvVar: "ACCOUNT_SMTP_HOST"}, got.AccountSMTP.Host)
	assert.Equal(t, settingValue[int]{Value: 587, Source: "default", EnvVar: "ACCOUNT_SMTP_PORT"}, got.AccountSMTP.Port)
	assert.Equal(t, passwordSetting{Set: false, Source: "default", EnvVar: "ACCOUNT_SMTP_PASSWORD"}, got.AccountSMTP.Password)
	assert.Equal(t, "starttls", got.AccountSMTP.TLS.Value)
	assert.False(t, got.AccountEmails)

	// and the public config and /me say what they always did
	assert.Equal(t, signupConfig{SignupMode: "invite", SignupOpen: false}, a.authConfig(t))
	assert.False(t, a.me(t, a.asAdmin).AccountEmails)
}

func TestAdminSettings_AVariableTheEnvironmentSetsIsShownLockedAndCanNotBeChanged(t *testing.T) {
	a := newAppWithAdmin(t, selfHosted, func(cfg *config.Config) { cfg.ClientURL = "https://env.peek.example" }, withEnvironment("SIGNUP_MODE", "CLIENT_URL"))

	got := a.installSettings(t)
	assert.Equal(t, settingValue[string]{Value: "invite", Source: "environment", Locked: true, EnvVar: "SIGNUP_MODE"}, got.SignupMode)
	assert.Equal(t, settingValue[string]{Value: "https://env.peek.example", Source: "environment", Locked: true, EnvVar: "CLIENT_URL"}, got.PublicURL)
	assert.False(t, got.AccountSMTP.Host.Locked)

	refused := settingRefusal(t, a.putSettings(t, map[string]any{"publicUrl": "https://other.example"}), http.StatusConflict)
	assert.Equal(t, "SETTING_LOCKED", refused.Error.Code)
	assert.Equal(t, "publicUrl", refused.Error.Field)
	assert.Contains(t, refused.Message, "set by the server's environment")
	assert.Equal(t, "https://env.peek.example", a.installSettings(t).PublicURL.Value)
}

func TestAdminSettings_ASavedValueIsShownAsSavedAndClearingItFallsBackToTheDefault(t *testing.T) {
	a := newAppWithAdmin(t, selfHosted)

	saved := a.savedSettings(t, map[string]any{"publicUrl": "https://saved.peek.example", "accountSmtp": map[string]any{"port": 2525}})
	assert.Equal(t, settingValue[string]{Value: "https://saved.peek.example", Source: "saved", EnvVar: "CLIENT_URL"}, saved.PublicURL)
	assert.Equal(t, settingValue[int]{Value: 2525, Source: "saved", EnvVar: "ACCOUNT_SMTP_PORT"}, saved.AccountSMTP.Port)

	cleared := a.savedSettings(t, map[string]any{"publicUrl": "", "accountSmtp": map[string]any{"port": 0}})
	assert.Equal(t, settingValue[string]{Value: clientURL, Source: "default", EnvVar: "CLIENT_URL"}, cleared.PublicURL)
	assert.Equal(t, settingValue[int]{Value: 587, Source: "default", EnvVar: "ACCOUNT_SMTP_PORT"}, cleared.AccountSMTP.Port)
}

func TestAdminSettings_AnInvalidValueIsRefusedNamingTheField(t *testing.T) {
	a := newAppWithAdmin(t, selfHosted)

	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"mode":       {map[string]any{"signupMode": "everyone"}, "signupMode"},
		"url":        {map[string]any{"publicUrl": "not a url"}, "publicUrl"},
		"tls":        {map[string]any{"accountSmtp": map[string]any{"tls": "ssl"}}, "accountSmtp.tls"},
		"from":       {map[string]any{"accountSmtp": map[string]any{"host": "smtp.peek.example", "from": "nobody"}}, "accountSmtp.from"},
		"no from":    {map[string]any{"accountSmtp": map[string]any{"host": "smtp.peek.example"}}, "accountSmtp.from"},
		"port range": {map[string]any{"accountSmtp": map[string]any{"port": 70000}}, "accountSmtp.port"},
	} {
		t.Run(name, func(t *testing.T) {
			refused := settingRefusal(t, a.putSettings(t, tc.body), http.StatusBadRequest)
			assert.Equal(t, "INVALID_SETTING", refused.Error.Code)
			assert.Equal(t, tc.field, refused.Error.Field)
		})
	}
	assert.Equal(t, "default", a.installSettings(t).PublicURL.Source, "nothing was saved")
}

// MARK: Readers

func TestAdminSettings_ASavedSenderSendsThePasswordResetEmail(t *testing.T) {
	mail := smtptest.Start(t)
	a := newAppWithAdmin(t, selfHosted)
	assert.Equal(t, http.StatusNotFound, a.forgot(t, adminEmail).Code, "no sender yet")

	saved := a.savedSettings(t, map[string]any{"accountSmtp": senderBody(mail)})
	assert.True(t, saved.AccountEmails)
	assert.True(t, a.authConfig(t).AccountEmails)

	require.Equal(t, http.StatusAccepted, a.forgot(t, adminEmail).Code)
	a.settled(t)
	sent := mailTo(mail, adminEmail)
	require.Len(t, sent, 1)
	assert.Equal(t, "saved@peek.example", sent[0].From)
	assert.True(t, sent[0].Authenticated, "with the saved user and password")
	assert.Contains(t, textOf(t, sent[0]), clientURL+"/reset-password/pkr_")
}

func TestAdminSettings_ASavedPublicURLIsInTheLinks(t *testing.T) {
	mail := smtptest.Start(t)
	a := newAppWithAdmin(t, selfHosted)

	a.savedSettings(t, map[string]any{"publicUrl": "https://saved.peek.example/", "accountSmtp": senderBody(mail)})

	created := a.invite(t, "bob@example.com", "member", a.asAdmin)
	assert.True(t, strings.HasPrefix(created.Link, "https://saved.peek.example/invite/"), created.Link)
	require.Equal(t, http.StatusAccepted, a.forgot(t, adminEmail).Code)
	a.settled(t)
	sent := mailTo(mail, adminEmail)
	require.NotEmpty(t, sent)
	assert.Contains(t, textOf(t, sent[len(sent)-1]), "https://saved.peek.example/reset-password/pkr_")
}

func TestAdminSettings_SavedOpenSignupWithASavedSenderLetsAStrangerIn(t *testing.T) {
	mail := smtptest.Start(t)
	a := newAppWithAdmin(t, selfHosted)
	refused := a.signUp(t, "stranger@example.com")
	require.Equal(t, http.StatusBadRequest, refused.Code, "invite mode: %s", refused.Body.String())

	saved := a.savedSettings(t, map[string]any{"signupMode": "open", "accountSmtp": senderBody(mail)})
	assert.Equal(t, settingValue[string]{Value: "open", Source: "saved", EnvVar: "SIGNUP_MODE"}, saved.SignupMode)
	assert.Equal(t, "open", a.authConfig(t).SignupMode)
	assert.True(t, a.authConfig(t).SignupOpen)

	stranger := a.signedUp(t, "stranger@example.com")
	me := a.meOf(t, stranger)
	require.Len(t, me.Workspaces, 1)
	assert.Equal(t, "owner", me.Workspaces[0].Role, "a workspace of their own")
	assert.True(t, me.Workspaces[0].Suspended, "until they verify their email")
	a.verifyWithMail(t, mail, "stranger@example.com")
	assert.False(t, a.meOf(t, stranger).Workspaces[0].Suspended)
}

func TestAdminSettings_OpenSignupWithoutASenderIsRefusedBothWays(t *testing.T) {
	mail := smtptest.Start(t)
	a := newAppWithAdmin(t, selfHosted)

	refused := settingRefusal(t, a.putSettings(t, map[string]any{"signupMode": "open"}), http.StatusBadRequest)
	assert.Equal(t, "OPEN_SIGNUP_NEEDS_SENDER", refused.Error.Code)
	assert.Equal(t, "signupMode", refused.Error.Field)
	assert.Equal(t, "invite", a.authConfig(t).SignupMode)

	a.savedSettings(t, map[string]any{"signupMode": "open", "accountSmtp": senderBody(mail)})
	refused = settingRefusal(t, a.putSettings(t, map[string]any{"accountSmtp": map[string]any{"host": ""}}), http.StatusBadRequest)
	assert.Equal(t, "SENDER_NEEDED_FOR_OPEN_SIGNUP", refused.Error.Code)
	assert.Equal(t, "accountSmtp.host", refused.Error.Field)
	assert.True(t, a.installSettings(t).AccountEmails, "the sender stays")
}

// MARK: Password

func TestAdminSettings_ThePasswordIsNeverInAnAnswerAndIsKeptWhenLeftOut(t *testing.T) {
	mail := smtptest.Start(t)
	a := newAppWithAdmin(t, selfHosted)

	put := a.putSettings(t, map[string]any{"accountSmtp": senderBody(mail)})
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	assert.NotContains(t, put.Body.String(), smtpPassword)
	get := a.request(t, http.MethodGet, adminSettingsPath, nil, a.asAdmin)
	assert.NotContains(t, get.Body.String(), smtpPassword)
	assert.Equal(t, passwordSetting{Set: true, Source: "saved", EnvVar: "ACCOUNT_SMTP_PASSWORD"}, a.installSettings(t).AccountSMTP.Password)

	// A later change that leaves the password out keeps it
	again := a.putSettings(t, map[string]any{"accountSmtp": map[string]any{"user": "mailer2"}})
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	assert.NotContains(t, again.Body.String(), smtpPassword)
	assert.True(t, a.installSettings(t).AccountSMTP.Password.Set)
	test := a.request(t, http.MethodPost, adminTestEmailPath, map[string]any{"to": "ops@example.com"}, a.asAdmin)
	require.Equal(t, http.StatusOK, test.Code, test.Body.String())
	assert.NotContains(t, test.Body.String(), smtpPassword)
	assert.True(t, mailTo(mail, "ops@example.com")[0].Authenticated, "the kept password still signs in")

	// An empty one clears it
	cleared := a.savedSettings(t, map[string]any{"accountSmtp": map[string]any{"password": ""}})
	assert.Equal(t, passwordSetting{Set: false, Source: "default", EnvVar: "ACCOUNT_SMTP_PASSWORD"}, cleared.AccountSMTP.Password)
}

// MARK: Test email

func TestAdminSettings_TheTestEmailGoesThroughTheCurrentSender(t *testing.T) {
	a := newAppWithAdmin(t, selfHosted)

	none := settingRefusal(t, a.request(t, http.MethodPost, adminTestEmailPath, map[string]any{"to": "ops@example.com"}, a.asAdmin), http.StatusBadRequest)
	assert.Equal(t, "NO_SENDER", none.Error.Code)

	mail := smtptest.Start(t)
	a.savedSettings(t, map[string]any{"accountSmtp": senderBody(mail)})
	w := a.request(t, http.MethodPost, adminTestEmailPath, map[string]any{"to": "ops@example.com"}, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	sent := mailTo(mail, "ops@example.com")
	require.Len(t, sent, 1, "sent before the answer")
	assert.Equal(t, "saved@peek.example", sent[0].From)
	assert.Contains(t, subjectOf(t, sent[0]), "test email")

	bad := a.request(t, http.MethodPost, adminTestEmailPath, map[string]any{"to": "not an address"}, a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, bad.Code, bad.Body.String())

	// A server that refuses the message answers 502 with the reply code, never the server's text
	mail.RejectMessages("554 5.7.1 <ops@example.com> refused by policy")
	refused := a.request(t, http.MethodPost, adminTestEmailPath, map[string]any{"to": "ops@example.com"}, a.asAdmin)
	require.Equal(t, http.StatusBadGateway, refused.Code, refused.Body.String())
	assert.Contains(t, refused.Body.String(), "554")
	assert.NotContains(t, refused.Body.String(), "refused by policy")
}

// MARK: Replicas

func TestAdminSettings_AnotherReplicaSeesAChangeWithinTheCacheLifetime(t *testing.T) {
	mail := smtptest.Start(t)
	first := newAppWithAdmin(t, selfHosted)
	replica := newReplica(t, first, selfHosted, withSettingsCacheLifetime(200*time.Millisecond))
	assert.Equal(t, "invite", replica.authConfig(t).SignupMode)

	first.savedSettings(t, map[string]any{"signupMode": "open", "accountSmtp": senderBody(mail)})

	assert.Equal(t, "open", first.authConfig(t).SignupMode, "at once on the replica that saved it")
	assert.Eventually(t, func() bool {
		got := replica.authConfig(t)
		return got.SignupMode == "open" && got.AccountEmails
	}, 5*time.Second, 50*time.Millisecond)
}

// MARK: Access and log

func TestAdminSettings_OnlyAnInstanceAdminSignedInAsAUserReachesThem(t *testing.T) {
	a := newAppWithAdmin(t, selfHosted)
	owner := a.userIn(t, "owner@example.com", a.firstWorkspace(t), workspace.RoleOwner)

	for _, route := range []call{
		{"read", http.MethodGet, adminSettingsPath, nil},
		{"change", http.MethodPut, adminSettingsPath, map[string]any{"publicUrl": "https://other.example"}},
		{"test email", http.MethodPost, adminTestEmailPath, map[string]any{"to": "ops@example.com"}},
	} {
		t.Run(route.name, func(t *testing.T) {
			assert.Equal(t, http.StatusForbidden, a.request(t, route.method, route.path, route.body, owner).Code)
			assert.Equal(t, http.StatusForbidden, a.request(t, route.method, route.path, route.body, a.withAPIKey).Code)
			assert.Equal(t, http.StatusUnauthorized, a.request(t, route.method, route.path, route.body).Code)
		})
	}
	assert.Equal(t, clientURL, a.installSettings(t).PublicURL.Value, "nothing changed")
}

func TestAdminSettings_AChangeLogsWhoMadeItAndTheKeysAndNoPassword(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(*zap.SugaredLogger) *zap.SugaredLogger { return zap.New(core).Sugar() })
	}, selfHosted)
	a.registerAdmin(t)
	adminID := a.userID(t, adminEmail)
	mail := smtptest.Start(t)

	a.savedSettings(t, map[string]any{"publicUrl": "https://saved.peek.example", "accountSmtp": senderBody(mail)})

	lines := logs.FilterMessage("Admin action").All()
	require.Len(t, lines, 1)
	fields := lines[0].ContextMap()
	assert.Equal(t, adminID, fields["actorUserId"])
	assert.Equal(t, "update_install_settings", fields["action"])
	assert.Equal(t, []any{"publicUrl", "accountSmtp.host", "accountSmtp.port", "accountSmtp.user", "accountSmtp.password", "accountSmtp.from", "accountSmtp.tls"}, fields["settings"])
	for _, entry := range logs.All() {
		assert.NotContains(t, fmt.Sprint(entry.ContextMap()), smtpPassword)
	}

	// a refused change logs nothing
	logs.TakeAll()
	settingRefusal(t, a.putSettings(t, map[string]any{"signupMode": "everyone"}), http.StatusBadRequest)
	assert.Empty(t, logs.FilterMessage("Admin action").All())
}
