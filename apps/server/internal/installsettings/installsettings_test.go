package installsettings

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// memoryStore is the settings table in memory. Another replica's write is a direct put.
type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
	types  map[string]string
	reads  int
	fail   error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{values: map[string]string{}, types: map[string]string{}}
}

func (s *memoryStore) GetByKey(_ context.Context, key string) (*shared.SettingModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.fail != nil {
		return nil, s.fail
	}
	value, ok := s.values[key]
	if !ok {
		return nil, nil
	}
	return &shared.SettingModel{Key: key, Value: value, Type: s.types[key]}, nil
}

func (s *memoryStore) SetByKey(_ context.Context, key string, entity *shared.SettingModel) (*shared.SettingModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key], s.types[key] = entity.Value, entity.Type
	return entity, nil
}

func (s *memoryStore) DeleteByKey(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}

func (s *memoryStore) put(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
}

func (s *memoryStore) get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	return value, ok
}

// defaults is the config of an install that sets nothing, as LoadConfig leaves it
func defaults() *config.Config {
	return &config.Config{
		ClientURL: "http://localhost:3000", SignupMode: config.SignupModeInvite,
		AccountSMTPPort: 587, AccountSMTPTLS: config.AccountSMTPStartTLS,
	}
}

// clock is a time that a test moves
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func newResolver(t *testing.T, cfg *config.Config, store Store) (*Resolver, *clock) {
	t.Helper()
	r := New(cfg, store, zap.NewNop().Sugar())
	c := &clock{at: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	r.now = c.now
	return r, c
}

func ptr[T any](v T) *T { return &v }

var ctx = context.Background()

func TestResolver_AnInstallThatSetsNothingHasTheDefaults(t *testing.T) {
	r, _ := newResolver(t, defaults(), newMemoryStore())

	assert.Equal(t, config.SignupModeInvite, r.SignupMode(ctx))
	assert.False(t, r.SignupOpen(ctx))
	assert.Nil(t, r.AccountSMTP(ctx))
	assert.Equal(t, "http://localhost:3000", r.PublicURL(ctx))

	view, err := r.Describe(ctx)
	require.NoError(t, err)
	assert.Equal(t, Field[string]{Value: "invite", Source: SourceDefault, EnvVar: "SIGNUP_MODE"}, view.SignupMode)
	assert.Equal(t, Field[string]{Value: "http://localhost:3000", Source: SourceDefault, EnvVar: "CLIENT_URL"}, view.PublicURL)
	assert.Equal(t, Field[int]{Value: 587, Source: SourceDefault, EnvVar: "ACCOUNT_SMTP_PORT"}, view.SMTPPort)
	assert.Equal(t, Field[string]{Value: "starttls", Source: SourceDefault, EnvVar: "ACCOUNT_SMTP_TLS"}, view.SMTPTLS)
	assert.Equal(t, Field[bool]{Value: false, Source: SourceDefault, EnvVar: "ACCOUNT_SMTP_PASSWORD"}, view.SMTPPassword)
	assert.False(t, view.SMTPHost.Locked())
	assert.False(t, view.SenderSet())
}

func TestResolver_AStoredValueIsUsedWhenTheEnvironmentDoesNotSetIt(t *testing.T) {
	store := newMemoryStore()
	store.put(KeySignupMode, "open")
	store.put(KeyPublicURL, "https://peek.example")
	store.put(KeySMTPHost, "smtp.peek.example")
	store.put(KeySMTPPort, "2525")
	store.put(KeySMTPUser, "mailer")
	store.put(KeySMTPPassword, "secret")
	store.put(KeySMTPFrom, "Peekaping <no-reply@peek.example>")
	store.put(KeySMTPTLS, "none")
	r, _ := newResolver(t, defaults(), store)

	assert.True(t, r.SignupOpen(ctx))
	assert.Equal(t, "https://peek.example", r.PublicURL(ctx))
	assert.Equal(t, &AccountSMTP{Host: "smtp.peek.example", Port: 2525, User: "mailer", Password: "secret", From: "Peekaping <no-reply@peek.example>", TLS: "none"}, r.AccountSMTP(ctx))

	view, err := r.Describe(ctx)
	require.NoError(t, err)
	assert.Equal(t, SourceSaved, view.SignupMode.Source)
	assert.Equal(t, Field[int]{Value: 2525, Source: SourceSaved, EnvVar: "ACCOUNT_SMTP_PORT"}, view.SMTPPort)
	assert.Equal(t, Field[bool]{Value: true, Source: SourceSaved, EnvVar: "ACCOUNT_SMTP_PASSWORD"}, view.SMTPPassword)
	assert.True(t, view.SenderSet())
}

func TestResolver_TheEnvironmentWinsOverAStoredValueAndLocksIt(t *testing.T) {
	cfg := defaults()
	cfg.ClientURL = "https://env.peek.example"
	cfg.AccountSMTPHost, cfg.AccountSMTPFrom = "smtp.env.example", "no-reply@env.example"
	cfg.SetFromEnvironment("CLIENT_URL", "ACCOUNT_SMTP_HOST", "ACCOUNT_SMTP_FROM")
	store := newMemoryStore()
	store.put(KeyPublicURL, "https://stored.peek.example")
	store.put(KeySMTPHost, "smtp.stored.example")
	store.put(KeySMTPUser, "stored-user")
	r, _ := newResolver(t, cfg, store)

	assert.Equal(t, "https://env.peek.example", r.PublicURL(ctx))
	sender := r.AccountSMTP(ctx)
	require.NotNil(t, sender)
	assert.Equal(t, "smtp.env.example", sender.Host)
	// The environment sets the whole sender: a sender variable it doesn't set takes its default, not
	// the stored value
	assert.Equal(t, "", sender.User)

	view, err := r.Describe(ctx)
	require.NoError(t, err)
	assert.Equal(t, Field[string]{Value: "https://env.peek.example", Source: SourceEnvironment, EnvVar: "CLIENT_URL"}, view.PublicURL)
	assert.True(t, view.PublicURL.Locked())
	assert.True(t, view.SMTPHost.Locked())
	assert.Equal(t, Field[string]{Value: "", Source: SourceEnvironment, EnvVar: "ACCOUNT_SMTP_USER"}, view.SMTPUser)
	assert.False(t, view.SignupMode.Locked(), "a setting outside the sender isn't locked by it")
}

func TestResolver_AnEnvironmentPasswordIsNeverSentToAStoredHost(t *testing.T) {
	cfg := defaults()
	cfg.AccountSMTPPassword = "env-secret"
	cfg.SetFromEnvironment("ACCOUNT_SMTP_PASSWORD")
	store := newMemoryStore()
	store.put(KeySMTPHost, "smtp.stored.example")
	store.put(KeySMTPUser, "stored-user")
	store.put(KeySMTPFrom, "no-reply@stored.example")
	r, _ := newResolver(t, cfg, store)

	assert.Nil(t, r.AccountSMTP(ctx), "the environment sets no host, so there is no sender")

	view, err := r.Describe(ctx)
	require.NoError(t, err)
	for name, field := range map[string]Field[string]{"host": view.SMTPHost, "user": view.SMTPUser, "from": view.SMTPFrom, "tls": view.SMTPTLS} {
		assert.True(t, field.Locked(), name)
	}
	assert.True(t, view.SMTPPort.Locked())
	assert.Equal(t, Field[bool]{Value: true, Source: SourceEnvironment, EnvVar: "ACCOUNT_SMTP_PASSWORD"}, view.SMTPPassword)
}

func TestResolver_KeepsStoredValuesForItsLifetime(t *testing.T) {
	store := newMemoryStore()
	r, c := newResolver(t, defaults(), store)
	assert.Equal(t, "http://localhost:3000", r.PublicURL(ctx))
	reads := store.reads

	// Another replica saves a value
	store.put(KeyPublicURL, "https://peek.example")
	c.at = c.at.Add(DefaultCacheLifetime - time.Millisecond)
	assert.Equal(t, "http://localhost:3000", r.PublicURL(ctx))
	assert.Equal(t, reads, store.reads, "a read within the lifetime doesn't reach the store")

	c.at = c.at.Add(time.Millisecond)
	assert.Equal(t, "https://peek.example", r.PublicURL(ctx))
}

func TestResolver_TheConfigShortensTheLifetime(t *testing.T) {
	cfg := defaults()
	cfg.InstallSettingsCacheLifetime = time.Second
	store := newMemoryStore()
	r, c := newResolver(t, cfg, store)
	assert.False(t, r.SignupOpen(ctx))

	store.put(KeySignupMode, "open")
	store.put(KeySMTPHost, "smtp.peek.example")
	c.at = c.at.Add(time.Second)

	assert.True(t, r.SignupOpen(ctx))
}

func TestResolver_ItsOwnWriteTakesEffectAtOnce(t *testing.T) {
	r, _ := newResolver(t, defaults(), newMemoryStore())
	assert.Equal(t, "http://localhost:3000", r.PublicURL(ctx))

	changed, err := r.Update(ctx, Change{PublicURL: ptr("https://peek.example")})

	require.NoError(t, err)
	assert.Equal(t, []string{"publicUrl"}, changed)
	assert.Equal(t, "https://peek.example", r.PublicURL(ctx))
}

func TestResolver_AFailedReadKeepsWhatWasReadBefore(t *testing.T) {
	store := newMemoryStore()
	store.put(KeyPublicURL, "https://peek.example")
	r, c := newResolver(t, defaults(), store)
	assert.Equal(t, "https://peek.example", r.PublicURL(ctx))

	store.fail = errors.New("database is gone")
	c.at = c.at.Add(time.Hour)

	assert.Equal(t, "https://peek.example", r.PublicURL(ctx))
	_, err := r.Describe(ctx)
	assert.Error(t, err)
}

func TestResolver_StaticHasTheConfigsValuesAndStoresNothing(t *testing.T) {
	cfg := defaults()
	cfg.ClientURL = "https://peek.example"
	r := Static(cfg)

	assert.Equal(t, "https://peek.example", r.PublicURL(ctx))
	_, err := r.Update(ctx, Change{PublicURL: ptr("https://other.example")})
	assert.Error(t, err)
}

func TestUpdate_StoresUnderTheInstallNamespace(t *testing.T) {
	store := newMemoryStore()
	r, _ := newResolver(t, defaults(), store)

	_, err := r.Update(ctx, Change{
		SignupMode: ptr("invite"), PublicURL: ptr(" https://peek.example "),
		SMTPHost: ptr("smtp.peek.example"), SMTPPort: ptr(465), SMTPUser: ptr("mailer"),
		SMTPPassword: ptr(" secret "), SMTPFrom: ptr("no-reply@peek.example"), SMTPTLS: ptr("tls"),
	})

	require.NoError(t, err)
	for key, want := range map[string]string{
		"install.signup_mode": "invite", "install.public_url": "https://peek.example",
		"install.account_smtp.host": "smtp.peek.example", "install.account_smtp.port": "465",
		"install.account_smtp.user": "mailer", "install.account_smtp.password": " secret ",
		"install.account_smtp.from": "no-reply@peek.example", "install.account_smtp.tls": "tls",
	} {
		got, ok := store.get(key)
		assert.True(t, ok, key)
		assert.Equal(t, want, got, key)
	}
}

func TestUpdate_RefusesAKeyTheEnvironmentSets(t *testing.T) {
	cfg := defaults()
	cfg.SetFromEnvironment("SIGNUP_MODE")
	store := newMemoryStore()
	r, _ := newResolver(t, cfg, store)

	_, err := r.Update(ctx, Change{PublicURL: ptr("https://peek.example"), SignupMode: ptr("invite")})

	var refused *Error
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, CodeLocked, refused.Code)
	assert.Equal(t, "signupMode", refused.Field)
	_, stored := store.get(KeyPublicURL)
	assert.False(t, stored, "a refused change stores nothing")
}

func TestUpdate_RefusesInvalidValuesNamingTheField(t *testing.T) {
	for name, tc := range map[string]struct {
		change Change
		field  string
	}{
		"mode":           {Change{SignupMode: ptr("everyone")}, "signupMode"},
		"url":            {Change{PublicURL: ptr("peek.example")}, "publicUrl"},
		"url scheme":     {Change{PublicURL: ptr("ftp://peek.example")}, "publicUrl"},
		"port":           {Change{SMTPHost: ptr("smtp.peek.example"), SMTPFrom: ptr("a@peek.example"), SMTPPort: ptr(70000)}, "accountSmtp.port"},
		"tls":            {Change{SMTPTLS: ptr("ssl")}, "accountSmtp.tls"},
		"from":           {Change{SMTPHost: ptr("smtp.peek.example"), SMTPFrom: ptr("not an address")}, "accountSmtp.from"},
		"host":           {Change{SMTPHost: ptr("smtp peek"), SMTPFrom: ptr("a@peek.example")}, "accountSmtp.host"},
		"host with from": {Change{SMTPHost: ptr("smtp.peek.example")}, "accountSmtp.from"},
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := newResolver(t, defaults(), newMemoryStore())

			_, err := r.Update(ctx, tc.change)

			var refused *Error
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, CodeInvalid, refused.Code)
			assert.Equal(t, tc.field, refused.Field)
			assert.NotEmpty(t, refused.Message)
		})
	}
}

func TestUpdate_OpenSignupNeedsASender(t *testing.T) {
	r, _ := newResolver(t, defaults(), newMemoryStore())

	_, err := r.Update(ctx, Change{SignupMode: ptr("open")})

	var refused *Error
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, CodeOpenSignupNeedsSender, refused.Code)
	assert.Equal(t, "signupMode", refused.Field)

	// With a sender in the same change it is accepted
	_, err = r.Update(ctx, Change{SignupMode: ptr("open"), SMTPHost: ptr("smtp.peek.example"), SMTPFrom: ptr("no-reply@peek.example")})
	require.NoError(t, err)
	assert.True(t, r.SignupOpen(ctx))
}

func TestUpdate_TheSenderCanNotBeRemovedWhileSignupIsOpen(t *testing.T) {
	store := newMemoryStore()
	store.put(KeySignupMode, "open")
	store.put(KeySMTPHost, "smtp.peek.example")
	store.put(KeySMTPFrom, "no-reply@peek.example")
	r, _ := newResolver(t, defaults(), store)

	_, err := r.Update(ctx, Change{SMTPHost: ptr("")})

	var refused *Error
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, CodeSenderNeededForOpenSignup, refused.Code)
	assert.Equal(t, "accountSmtp.host", refused.Field)

	// Back to invite first, and the sender can go
	_, err = r.Update(ctx, Change{SignupMode: ptr(""), SMTPHost: ptr(""), SMTPFrom: ptr("")})
	require.NoError(t, err)
	assert.Nil(t, r.AccountSMTP(ctx))
}

func TestUpdate_ClearingAStoredValueFallsBackToTheDefault(t *testing.T) {
	store := newMemoryStore()
	r, _ := newResolver(t, defaults(), store)
	_, err := r.Update(ctx, Change{PublicURL: ptr("https://peek.example"), SMTPPort: ptr(2525)})
	require.NoError(t, err)

	changed, err := r.Update(ctx, Change{PublicURL: ptr(""), SMTPPort: ptr(0)})

	require.NoError(t, err)
	assert.Equal(t, []string{"publicUrl", "accountSmtp.port"}, changed)
	view, err := r.Describe(ctx)
	require.NoError(t, err)
	assert.Equal(t, Field[string]{Value: "http://localhost:3000", Source: SourceDefault, EnvVar: "CLIENT_URL"}, view.PublicURL)
	assert.Equal(t, Field[int]{Value: 587, Source: SourceDefault, EnvVar: "ACCOUNT_SMTP_PORT"}, view.SMTPPort)
	_, stored := store.get(KeyPublicURL)
	assert.False(t, stored)
}

func TestUpdate_ThePasswordChangesOnlyWhenSent(t *testing.T) {
	store := newMemoryStore()
	r, _ := newResolver(t, defaults(), store)
	_, err := r.Update(ctx, Change{SMTPHost: ptr("smtp.peek.example"), SMTPFrom: ptr("no-reply@peek.example"), SMTPPassword: ptr("secret")})
	require.NoError(t, err)

	_, err = r.Update(ctx, Change{SMTPFrom: ptr("Peekaping <no-reply@peek.example>")})
	require.NoError(t, err)
	assert.Equal(t, "secret", r.AccountSMTP(ctx).Password)

	_, err = r.Update(ctx, Change{SMTPPassword: ptr("")})
	require.NoError(t, err)
	assert.Equal(t, "", r.AccountSMTP(ctx).Password)
	_, stored := store.get(KeySMTPPassword)
	assert.False(t, stored)
}

// savedSender is a store with a whole sender saved, its password too
func savedSender() *memoryStore {
	store := newMemoryStore()
	store.put(KeySMTPHost, "smtp.peek.example")
	store.put(KeySMTPPort, "2525")
	store.put(KeySMTPUser, "mailer")
	store.put(KeySMTPPassword, "secret")
	store.put(KeySMTPFrom, "no-reply@peek.example")
	store.put(KeySMTPTLS, "tls")
	return store
}

func TestUpdate_ChangingWhereThePasswordGoesWithoutOneClearsTheSavedOne(t *testing.T) {
	for name, change := range map[string]Change{
		"host":          {SMTPHost: ptr("smtp.other.example")},
		"port":          {SMTPPort: ptr(465)},
		"port cleared":  {SMTPPort: ptr(0)},
		"tls":           {SMTPTLS: ptr("starttls")},
		"user":          {SMTPUser: ptr("someone-else")},
		"user cleared":  {SMTPUser: ptr("")},
		"host and from": {SMTPHost: ptr("smtp.other.example"), SMTPFrom: ptr("no-reply@other.example")},
	} {
		t.Run(name, func(t *testing.T) {
			store := savedSender()
			r, _ := newResolver(t, defaults(), store)

			changed, err := r.Update(ctx, change)

			require.NoError(t, err)
			assert.Contains(t, changed, "accountSmtp.password")
			assert.Equal(t, "", r.AccountSMTP(ctx).Password, "the admin enters the password again")
			_, stored := store.get(KeySMTPPassword)
			assert.False(t, stored)
		})
	}
}

func TestUpdate_ThePasswordStaysWhileTheServerAndUserStayTheSame(t *testing.T) {
	for name, change := range map[string]Change{
		"from":            {SMTPFrom: ptr("Peekaping <no-reply@peek.example>")},
		"the same values": {SMTPHost: ptr(" smtp.peek.example "), SMTPPort: ptr(2525), SMTPUser: ptr("mailer"), SMTPTLS: ptr("tls")},
		"another setting": {PublicURL: ptr("https://peek.example")},
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := newResolver(t, defaults(), savedSender())

			changed, err := r.Update(ctx, change)

			require.NoError(t, err)
			assert.NotContains(t, changed, "accountSmtp.password")
			assert.Equal(t, "secret", r.AccountSMTP(ctx).Password)
		})
	}
}

func TestUpdate_ANewPasswordSentWithTheNewServerIsUsed(t *testing.T) {
	r, _ := newResolver(t, defaults(), savedSender())

	_, err := r.Update(ctx, Change{SMTPHost: ptr("smtp.other.example"), SMTPPassword: ptr("other-secret")})

	require.NoError(t, err)
	assert.Equal(t, &AccountSMTP{Host: "smtp.other.example", Port: 2525, User: "mailer", Password: "other-secret", From: "no-reply@peek.example", TLS: "tls"}, r.AccountSMTP(ctx))
}

func TestUpdate_AnySenderVariableInTheEnvironmentLocksTheWholeSender(t *testing.T) {
	for name, tc := range map[string]struct {
		change Change
		field  string
	}{
		"host":     {Change{SMTPHost: ptr("smtp.peek.example"), SMTPFrom: ptr("no-reply@peek.example")}, "accountSmtp.host"},
		"port":     {Change{SMTPPort: ptr(2525)}, "accountSmtp.port"},
		"user":     {Change{SMTPUser: ptr("mailer")}, "accountSmtp.user"},
		"from":     {Change{SMTPFrom: ptr("no-reply@peek.example")}, "accountSmtp.from"},
		"tls":      {Change{SMTPTLS: ptr("none")}, "accountSmtp.tls"},
		"password": {Change{SMTPPassword: ptr("")}, "accountSmtp.password"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := defaults()
			cfg.AccountSMTPPassword = "env-secret"
			cfg.SetFromEnvironment("ACCOUNT_SMTP_PASSWORD")
			store := newMemoryStore()
			r, _ := newResolver(t, cfg, store)

			_, err := r.Update(ctx, tc.change)

			var refused *Error
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, CodeLocked, refused.Code)
			assert.Equal(t, tc.field, refused.Field)
			assert.Contains(t, refused.Message, "ACCOUNT_SMTP_PASSWORD")
			assert.Empty(t, store.values, "a refused change stores nothing")
		})
	}

	// A setting outside the sender can still change
	cfg := defaults()
	cfg.SetFromEnvironment("ACCOUNT_SMTP_PASSWORD")
	r, _ := newResolver(t, cfg, newMemoryStore())
	_, err := r.Update(ctx, Change{PublicURL: ptr("https://peek.example")})
	require.NoError(t, err)
}

func TestUpdate_ReportsOnlyTheFieldsItChanged(t *testing.T) {
	r, _ := newResolver(t, defaults(), newMemoryStore())
	_, err := r.Update(ctx, Change{PublicURL: ptr("https://peek.example")})
	require.NoError(t, err)

	changed, err := r.Update(ctx, Change{PublicURL: ptr("https://peek.example"), SMTPUser: ptr("mailer")})

	require.NoError(t, err)
	assert.Equal(t, []string{"accountSmtp.user"}, changed)
}

// MARK: Default limits (PP-160)

func TestResolver_EachDefaultLimitIsNoneWhenNothingSetsIt(t *testing.T) {
	r, _ := newResolver(t, defaults(), newMemoryStore())

	got, err := r.DefaultLimits(ctx)

	require.NoError(t, err)
	assert.Equal(t, DefaultLimits{
		Monitors:         Field[*int]{Source: SourceDefault, EnvVar: "WORKSPACE_LIMIT_MONITORS"},
		MinCheckInterval: Field[*int]{Source: SourceDefault, EnvVar: "WORKSPACE_LIMIT_MIN_CHECK_INTERVAL"},
		StatusPages:      Field[*int]{Source: SourceDefault, EnvVar: "WORKSPACE_LIMIT_STATUS_PAGES"},
		CustomDomains:    Field[*int]{Source: SourceDefault, EnvVar: "WORKSPACE_LIMIT_CUSTOM_DOMAINS"},
		Members:          Field[*int]{Source: SourceDefault, EnvVar: "WORKSPACE_LIMIT_MEMBERS"},
		OwnedWorkspaces:  Field[*int]{Source: SourceDefault, EnvVar: "WORKSPACE_LIMIT_OWNED_WORKSPACES"},
		ProbesPerMonitor: Field[*int]{Source: SourceDefault, EnvVar: "WORKSPACE_LIMIT_PROBES_PER_MONITOR"},
		RetentionDays:    Field[*int]{Source: SourceDefault, EnvVar: "WORKSPACE_LIMIT_RETENTION_DAYS"},
	}, got)
	view, err := r.Describe(ctx)
	require.NoError(t, err)
	assert.Equal(t, got, view.WorkspaceLimits)
}

func TestResolver_ADefaultLimitComesFromTheEnvironmentThenTheStoreThenNone(t *testing.T) {
	cfg := defaults()
	cfg.WorkspaceLimitMonitors = 10
	cfg.SetFromEnvironment("WORKSPACE_LIMIT_MONITORS", "WORKSPACE_LIMIT_MEMBERS")
	store := newMemoryStore()
	store.put("install.workspace_limits.monitors", "50")
	store.put("install.workspace_limits.members", "4")
	store.put("install.workspace_limits.status_pages", "3")
	store.put("install.workspace_limits.retention_days", "not a number")
	r, _ := newResolver(t, cfg, store)

	got, err := r.DefaultLimits(ctx)

	require.NoError(t, err)
	assert.Equal(t, Field[*int]{Value: ptr(10), Source: SourceEnvironment, EnvVar: "WORKSPACE_LIMIT_MONITORS"}, got.Monitors, "the environment wins")
	assert.True(t, got.Monitors.Locked())
	assert.Equal(t, Field[*int]{Source: SourceEnvironment, EnvVar: "WORKSPACE_LIMIT_MEMBERS"}, got.Members, "0 in the environment is no limit, and locked")
	assert.Equal(t, Field[*int]{Value: ptr(3), Source: SourceSaved, EnvVar: "WORKSPACE_LIMIT_STATUS_PAGES"}, got.StatusPages)
	assert.Nil(t, got.RetentionDays.Value, "a saved value that is no number is no limit")
	assert.Equal(t, Field[*int]{Source: SourceDefault, EnvVar: "WORKSPACE_LIMIT_CUSTOM_DOMAINS"}, got.CustomDomains)
}

func TestResolver_AFailedReadOfTheDefaultLimitsIsAnError(t *testing.T) {
	store := newMemoryStore()
	store.put("install.workspace_limits.monitors", "5")
	r, c := newResolver(t, defaults(), store)
	got, err := r.DefaultLimits(ctx)
	require.NoError(t, err)
	assert.Equal(t, ptr(5), got.Monitors.Value)

	store.fail = errors.New("database is gone")
	c.at = c.at.Add(time.Hour)

	_, err = r.DefaultLimits(ctx)
	assert.Error(t, err, "limits it can't read are no reason to have none")
}

func TestUpdate_SavesAndClearsADefaultLimit(t *testing.T) {
	cfg := defaults()
	cfg.MinCheckInterval = 30
	store := newMemoryStore()
	r, _ := newResolver(t, cfg, store)

	changed, err := r.Update(ctx, Change{WorkspaceLimits: map[string]*int{
		LimitMonitors: ptr(25), LimitMinCheckInterval: ptr(30), LimitRetentionDays: ptr(MaxRetentionDays),
	}})

	require.NoError(t, err)
	assert.Equal(t, []string{"workspaceLimits.monitors", "workspaceLimits.min_check_interval", "workspaceLimits.retention_days"}, changed)
	saved, _ := store.get("install.workspace_limits.monitors")
	assert.Equal(t, "25", saved)
	got, err := r.DefaultLimits(ctx)
	require.NoError(t, err)
	assert.Equal(t, Field[*int]{Value: ptr(25), Source: SourceSaved, EnvVar: "WORKSPACE_LIMIT_MONITORS"}, got.Monitors, "at once in this process")

	changed, err = r.Update(ctx, Change{WorkspaceLimits: map[string]*int{LimitMonitors: nil}})

	require.NoError(t, err)
	assert.Equal(t, []string{"workspaceLimits.monitors"}, changed)
	_, stored := store.get("install.workspace_limits.monitors")
	assert.False(t, stored)
	got, err = r.DefaultLimits(ctx)
	require.NoError(t, err)
	assert.Nil(t, got.Monitors.Value, "cleared, there is no limit")
	assert.Equal(t, ptr(30), got.MinCheckInterval.Value, "a limit the change leaves out stays")
}

func TestUpdate_RefusesADefaultLimitTheEnvironmentSets(t *testing.T) {
	cfg := defaults()
	cfg.SetFromEnvironment("WORKSPACE_LIMIT_MEMBERS")
	store := newMemoryStore()
	r, _ := newResolver(t, cfg, store)

	for name, value := range map[string]*int{"set": ptr(5), "clear": nil} {
		_, err := r.Update(ctx, Change{WorkspaceLimits: map[string]*int{LimitStatusPages: ptr(2), LimitMembers: value}})

		var refused *Error
		require.ErrorAs(t, err, &refused, name)
		assert.Equal(t, CodeLocked, refused.Code, name)
		assert.Equal(t, "workspaceLimits.members", refused.Field, name)
	}
	_, stored := store.get("install.workspace_limits.status_pages")
	assert.False(t, stored, "a refused change stores nothing")
}

func TestUpdate_RefusesAnInvalidDefaultLimit(t *testing.T) {
	cfg := defaults()
	cfg.MinCheckInterval = 60
	for name, tc := range map[string]struct {
		limit string
		value int
	}{
		"zero":                     {LimitMonitors, 0},
		"negative":                 {LimitProbesPerMonitor, -2},
		"below MIN_CHECK_INTERVAL": {LimitMinCheckInterval, 59},
		"no retention":             {LimitRetentionDays, 0},
		"too long a retention":     {LimitRetentionDays, MaxRetentionDays + 1},
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := newResolver(t, cfg, newMemoryStore())

			_, err := r.Update(ctx, Change{WorkspaceLimits: map[string]*int{tc.limit: ptr(tc.value)}})

			var refused *Error
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, CodeInvalid, refused.Code)
			assert.Equal(t, "workspaceLimits."+tc.limit, refused.Field)
			assert.NotEmpty(t, refused.Message)
		})
	}
	t.Run("not a limit", func(t *testing.T) {
		r, _ := newResolver(t, cfg, newMemoryStore())
		_, err := r.Update(ctx, Change{WorkspaceLimits: map[string]*int{"robots": ptr(1)}})
		var refused *Error
		require.ErrorAs(t, err, &refused)
		assert.Equal(t, "workspaceLimits.robots", refused.Field)
	})
}
