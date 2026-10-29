// Package installsettings resolves the install settings that an instance admin can change in the
// web app (docs/adr/0005 section 5): the signup mode (SIGNUP_MODE), the sender of account emails
// (ACCOUNT_SMTP_*) and the public URL (CLIENT_URL). Each value comes from the environment when its
// variable is set, then from the value saved in the settings table, then from the default. A
// setting the environment sets is locked: Update refuses to change it. The sender is one setting
// for this: any ACCOUNT_SMTP_* variable that is set locks all of it. A saved SMTP password is kept
// only while the host, port, TLS mode and user stay as they were (GLOSSARY.md, Endpoint field).
//
// Every reader of these settings goes through a Resolver at the moment it needs the value, so a
// saved change takes effect without a restart. The stored values are kept for a few seconds
// (DefaultCacheLifetime); a write through the Resolver takes effect at once in its own process and
// within that lifetime on the other api replicas.
package installsettings

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/shared"

	"go.uber.org/zap"
)

// DefaultCacheLifetime is how long a Resolver keeps the values it read from the store
const DefaultCacheLifetime = 5 * time.Second

// The keys the settings are stored under in the settings table
const (
	KeySignupMode   = "install.signup_mode"
	KeyPublicURL    = "install.public_url"
	KeySMTPHost     = "install.account_smtp.host"
	KeySMTPPort     = "install.account_smtp.port"
	KeySMTPUser     = "install.account_smtp.user"
	KeySMTPPassword = "install.account_smtp.password"
	KeySMTPFrom     = "install.account_smtp.from"
	KeySMTPTLS      = "install.account_smtp.tls"
)

// defaultSMTPPort is ACCOUNT_SMTP_PORT's default, for a config built without LoadConfig
const defaultSMTPPort = 587

// Source is where a setting's value comes from
type Source string

const (
	// SourceEnvironment is a variable of the system environment or the .env file. It locks the setting.
	SourceEnvironment Source = "environment"
	// SourceSaved is a value an instance admin saved in the web app
	SourceSaved Source = "saved"
	// SourceDefault is the default, when neither sets it
	SourceDefault Source = "default"
)

// Store is the settings table (setting.Service is one)
type Store interface {
	GetByKey(ctx context.Context, key string) (*shared.SettingModel, error)
	SetByKey(ctx context.Context, key string, entity *shared.SettingModel) (*shared.SettingModel, error)
	DeleteByKey(ctx context.Context, key string) error
}

// AccountSMTP is the install's sender for account emails
type AccountSMTP struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
	// TLS is starttls, tls or none (config.AccountSMTPStartTLS and the like)
	TLS string
}

// Field is a setting's value and where it comes from. EnvVar is the variable that sets it.
type Field[T any] struct {
	Value  T
	Source Source
	EnvVar string
}

// Locked is whether the environment sets the value, so that it can't be changed here
func (f Field[T]) Locked() bool { return f.Source == SourceEnvironment }

// View is every install setting as it resolves now. The SMTP password's value is whether one is set:
// the password itself is never shown.
type View struct {
	SignupMode   Field[string]
	PublicURL    Field[string]
	SMTPHost     Field[string]
	SMTPPort     Field[int]
	SMTPUser     Field[string]
	SMTPPassword Field[bool]
	SMTPFrom     Field[string]
	SMTPTLS      Field[string]
}

// SenderSet is whether the install has a sender for account emails
func (v *View) SenderSet() bool { return strings.TrimSpace(v.SMTPHost.Value) != "" }

// Change is a partial update: a nil field stays as it is, and an empty string (or a port of 0)
// clears the saved value, so that the setting falls back to its default. The password is changed
// only when it is sent.
type Change struct {
	SignupMode   *string
	PublicURL    *string
	SMTPHost     *string
	SMTPPort     *int
	SMTPUser     *string
	SMTPPassword *string
	SMTPFrom     *string
	SMTPTLS      *string
}

// The codes of the errors Update returns
const (
	// CodeLocked is a change to a setting the environment sets
	CodeLocked = "SETTING_LOCKED"
	// CodeInvalid is a value the setting can't hold
	CodeInvalid = "INVALID_SETTING"
	// CodeOpenSignupNeedsSender is open signup without a sender: each new account must verify its
	// email address
	CodeOpenSignupNeedsSender = "OPEN_SIGNUP_NEEDS_SENDER"
	// CodeSenderNeededForOpenSignup is removing the sender while signup is open
	CodeSenderNeededForOpenSignup = "SENDER_NEEDED_FOR_OPEN_SIGNUP"
)

// Error is why Update refused a change. Field is the setting it is about, as the admin API names it
// (signupMode, publicUrl, accountSmtp.host and the like).
type Error struct {
	Code    string
	Field   string
	Message string
}

func (e *Error) Error() string { return e.Field + ": " + e.Message }

// setting is one install setting: its name in the admin API, its variable, where it is stored, the
// config's value (the environment's, or the default when the variable is unset), and its check
type setting struct {
	field    string
	envVar   string
	key      string
	kind     string
	fallback func(*config.Config) string
	check    func(string) string
}

var (
	signupMode = setting{"signupMode", "SIGNUP_MODE", KeySignupMode, "string", func(c *config.Config) string {
		if c.SignupMode == "" {
			return config.SignupModeInvite
		}
		return c.SignupMode
	}, checkSignupMode}
	publicURL = setting{"publicUrl", "CLIENT_URL", KeyPublicURL, "string", func(c *config.Config) string { return c.ClientURL }, checkPublicURL}
	smtpHost  = setting{"accountSmtp.host", "ACCOUNT_SMTP_HOST", KeySMTPHost, "string", func(c *config.Config) string { return c.AccountSMTPHost }, checkHost}
	smtpPort  = setting{"accountSmtp.port", "ACCOUNT_SMTP_PORT", KeySMTPPort, "int", func(c *config.Config) string {
		if c.AccountSMTPPort == 0 {
			return strconv.Itoa(defaultSMTPPort)
		}
		return strconv.Itoa(c.AccountSMTPPort)
	}, checkPort}
	smtpUser     = setting{"accountSmtp.user", "ACCOUNT_SMTP_USER", KeySMTPUser, "string", func(c *config.Config) string { return c.AccountSMTPUser }, checkOneLine}
	smtpPassword = setting{"accountSmtp.password", "ACCOUNT_SMTP_PASSWORD", KeySMTPPassword, "string", func(c *config.Config) string { return c.AccountSMTPPassword }, checkOneLine}
	smtpFrom     = setting{"accountSmtp.from", "ACCOUNT_SMTP_FROM", KeySMTPFrom, "string", func(c *config.Config) string { return c.AccountSMTPFrom }, checkFrom}
	smtpTLS      = setting{"accountSmtp.tls", "ACCOUNT_SMTP_TLS", KeySMTPTLS, "string", func(c *config.Config) string {
		if c.AccountSMTPTLS == "" {
			return config.AccountSMTPStartTLS
		}
		return c.AccountSMTPTLS
	}, checkTLS}

	// all is every setting, in the order Update reports changes
	all = []setting{signupMode, publicURL, smtpHost, smtpPort, smtpUser, smtpPassword, smtpFrom, smtpTLS}
	// senderSettings is the settings of the sender, which the environment sets as one
	senderSettings = []setting{smtpHost, smtpPort, smtpUser, smtpPassword, smtpFrom, smtpTLS}
)

// isSender is whether s is a setting of the sender
func (s setting) isSender() bool {
	for _, other := range senderSettings {
		if other.key == s.key {
			return true
		}
	}
	return false
}

// endpoint is where the sender signs in: a saved password is kept only while it stays the same
type endpoint struct {
	host string
	port int
	user string
	tls  string
}

// Resolver resolves the install settings. It is safe for concurrent use.
type Resolver struct {
	cfg      *config.Config
	store    Store
	logger   *zap.SugaredLogger
	lifetime time.Duration
	now      func() time.Time

	// mu guards the cache: values is nil until a read succeeded. stale makes the next call read
	// again, and values stays what it answers when that read fails. generation counts this
	// process's writes, so that a read that started before one is not kept.
	mu         sync.Mutex
	values     map[string]string
	loadedAt   time.Time
	stale      bool
	generation uint64
	// writing makes this process's updates one at a time, so that each checks what the last stored
	writing sync.Mutex
}

// New resolves the settings of cfg against the values saved in store
func New(cfg *config.Config, store Store, logger *zap.SugaredLogger) *Resolver {
	lifetime := cfg.InstallSettingsCacheLifetime
	if lifetime <= 0 {
		lifetime = DefaultCacheLifetime
	}
	return &Resolver{cfg: cfg, store: store, logger: logger.Named("[install-settings]"), lifetime: lifetime, now: time.Now}
}

// NewFromStore is New for the app's graph, where setting.Service is the store
func NewFromStore(cfg *config.Config, store shared.SettingService, logger *zap.SugaredLogger) *Resolver {
	return New(cfg, store, logger)
}

// Static resolves the settings of cfg alone, as if nothing were saved: the config's values are the
// environment's and the defaults. Update refuses every change. Tests that build a service by hand
// use it.
func Static(cfg *config.Config) *Resolver {
	return New(cfg, nil, zap.NewNop().Sugar())
}

// SignupMode is who may create an account: config.SignupModeInvite or config.SignupModeOpen
func (r *Resolver) SignupMode(ctx context.Context) string {
	return r.resolve(r.stored(ctx), signupMode).Value
}

// SignupOpen is whether anyone may sign up and get a workspace of their own
func (r *Resolver) SignupOpen(ctx context.Context) bool {
	return r.SignupMode(ctx) == config.SignupModeOpen
}

// PublicURL is the address of the web app, which the links in emails and notifications open
func (r *Resolver) PublicURL(ctx context.Context) string {
	return r.resolve(r.stored(ctx), publicURL).Value
}

// AccountSMTP is the sender for account emails, or nil when the install has none (no host)
func (r *Resolver) AccountSMTP(ctx context.Context) *AccountSMTP {
	return r.sender(r.stored(ctx))
}

// Describe reads the saved values afresh and gives every setting with its source
func (r *Resolver) Describe(ctx context.Context) (*View, error) {
	values, err := r.fresh(ctx)
	if err != nil {
		return nil, err
	}
	return r.view(values), nil
}

// Update saves the change and returns the fields it changed, as the admin API names them. It
// refuses (an *Error) a setting the environment sets, a value the setting can't hold, open signup
// without a sender, and removing the sender while signup is open; a refused change stores nothing.
// The change takes effect at once in this process.
func (r *Resolver) Update(ctx context.Context, change Change) ([]string, error) {
	if r.store == nil {
		return nil, errors.New("the install settings have no store")
	}
	r.writing.Lock()
	defer r.writing.Unlock()

	current, err := r.fresh(ctx)
	if err != nil {
		return nil, err
	}
	next := make(map[string]string, len(current))
	for key, value := range current {
		next[key] = value
	}
	var port *string
	if change.SMTPPort != nil {
		port = new(string)
		if *change.SMTPPort != 0 {
			*port = strconv.Itoa(*change.SMTPPort)
		}
	}
	for _, step := range []struct {
		setting setting
		value   *string
	}{
		{signupMode, change.SignupMode}, {publicURL, change.PublicURL}, {smtpHost, change.SMTPHost},
		{smtpPort, port}, {smtpUser, change.SMTPUser}, {smtpPassword, change.SMTPPassword},
		{smtpFrom, change.SMTPFrom}, {smtpTLS, change.SMTPTLS},
	} {
		if step.value == nil {
			continue
		}
		s := step.setting
		if envVar := r.lockedBy(s); envVar != "" {
			message := "set by the server's environment (" + envVar + ")"
			if envVar != s.envVar {
				message = "the sender is set by the server's environment (" + envVar + ")"
			}
			return nil, &Error{Code: CodeLocked, Field: s.field, Message: message}
		}
		value := *step.value
		// A password is taken as sent: a space may be part of it
		if s.key != KeySMTPPassword {
			value = strings.TrimSpace(value)
		}
		if value == "" {
			delete(next, s.key)
			continue
		}
		if problem := s.check(value); problem != "" {
			return nil, &Error{Code: CodeInvalid, Field: s.field, Message: problem}
		}
		next[s.key] = value
	}
	// A saved password is kept only while the server and user it was entered for stay the same
	// (GLOSSARY.md, Endpoint field): a change of the host, port, TLS mode or user that sends no
	// password clears it, and the admin enters it again, so that it is never sent somewhere new
	if change.SMTPPassword == nil && r.endpoint(current) != r.endpoint(next) {
		delete(next, KeySMTPPassword)
	}

	// The checks that take the settings together, on what they will resolve to: the startup's
	// (config.ValidateAccountSMTP and config.ValidateSignup)
	resolvedHost := strings.TrimSpace(r.resolve(next, smtpHost).Value)
	if resolvedHost != "" && strings.TrimSpace(r.resolve(next, smtpFrom).Value) == "" {
		return nil, &Error{Code: CodeInvalid, Field: smtpFrom.field, Message: "a sender address, such as no-reply@example.com, is required with an SMTP host"}
	}
	if r.resolve(next, signupMode).Value == config.SignupModeOpen && resolvedHost == "" {
		if change.SignupMode != nil && strings.TrimSpace(*change.SignupMode) == config.SignupModeOpen {
			return nil, &Error{Code: CodeOpenSignupNeedsSender, Field: signupMode.field, Message: "open signup needs a sender for account emails, because new accounts must verify their email address"}
		}
		return nil, &Error{Code: CodeSenderNeededForOpenSignup, Field: smtpHost.field, Message: "signup is open, and new accounts need the sender to verify their email address: switch signup to invite first"}
	}

	var changed []string
	for _, s := range all {
		before, had := current[s.key]
		after, has := next[s.key]
		if had == has && before == after {
			continue
		}
		if has {
			_, err = r.store.SetByKey(ctx, s.key, &shared.SettingModel{Key: s.key, Value: after, Type: s.kind})
		} else {
			err = r.store.DeleteByKey(ctx, s.key)
		}
		if err != nil {
			r.invalidate(nil)
			return nil, fmt.Errorf("failed to save %s: %w", s.field, err)
		}
		changed = append(changed, s.field)
	}
	r.invalidate(next)
	return changed, nil
}

// lockedBy is the variable of the environment that locks the setting, or empty when none does. The
// environment sets the sender as one: any ACCOUNT_SMTP_* variable that is set locks every setting
// of the sender, so that a password from the environment is never sent to a saved host.
func (r *Resolver) lockedBy(s setting) string {
	if r.cfg.FromEnvironment(s.envVar) {
		return s.envVar
	}
	if s.isSender() {
		for _, other := range senderSettings {
			if r.cfg.FromEnvironment(other.envVar) {
				return other.envVar
			}
		}
	}
	return ""
}

// resolve is a setting's value: the environment's, then the saved one, then the default. A
// setting the environment locks takes the environment's value, or the default when its own
// variable is unset.
func (r *Resolver) resolve(values map[string]string, s setting) Field[string] {
	if r.lockedBy(s) != "" {
		return Field[string]{Value: s.fallback(r.cfg), Source: SourceEnvironment, EnvVar: s.envVar}
	}
	if value, ok := values[s.key]; ok {
		return Field[string]{Value: value, Source: SourceSaved, EnvVar: s.envVar}
	}
	return Field[string]{Value: s.fallback(r.cfg), Source: SourceDefault, EnvVar: s.envVar}
}

func (r *Resolver) port(values map[string]string) Field[int] {
	f := r.resolve(values, smtpPort)
	port, err := strconv.Atoi(f.Value)
	if err != nil {
		port = defaultSMTPPort
	}
	return Field[int]{Value: port, Source: f.Source, EnvVar: f.EnvVar}
}

func (r *Resolver) endpoint(values map[string]string) endpoint {
	return endpoint{
		host: strings.TrimSpace(r.resolve(values, smtpHost).Value),
		port: r.port(values).Value,
		user: r.resolve(values, smtpUser).Value,
		tls:  r.resolve(values, smtpTLS).Value,
	}
}

func (r *Resolver) sender(values map[string]string) *AccountSMTP {
	host := strings.TrimSpace(r.resolve(values, smtpHost).Value)
	if host == "" {
		return nil
	}
	return &AccountSMTP{
		Host:     host,
		Port:     r.port(values).Value,
		User:     r.resolve(values, smtpUser).Value,
		Password: r.resolve(values, smtpPassword).Value,
		From:     r.resolve(values, smtpFrom).Value,
		TLS:      r.resolve(values, smtpTLS).Value,
	}
}

func (r *Resolver) view(values map[string]string) *View {
	password := r.resolve(values, smtpPassword)
	return &View{
		SignupMode:   r.resolve(values, signupMode),
		PublicURL:    r.resolve(values, publicURL),
		SMTPHost:     r.resolve(values, smtpHost),
		SMTPPort:     r.port(values),
		SMTPUser:     r.resolve(values, smtpUser),
		SMTPPassword: Field[bool]{Value: password.Value != "", Source: password.Source, EnvVar: password.EnvVar},
		SMTPFrom:     r.resolve(values, smtpFrom),
		SMTPTLS:      r.resolve(values, smtpTLS),
	}
}

// stored is the saved values, from the cache while it is fresh. A read that fails keeps what was
// read before or what this process saved since (nothing, the first time), logs, and is tried again
// on the next call.
func (r *Resolver) stored(ctx context.Context) map[string]string {
	if r.store == nil {
		return nil
	}
	r.mu.Lock()
	if r.values != nil && !r.stale && r.now().Sub(r.loadedAt) < r.lifetime {
		values := r.values
		r.mu.Unlock()
		return values
	}
	previous := r.values
	r.mu.Unlock()
	values, err := r.fresh(ctx)
	if err != nil {
		r.logger.Errorw("Failed to read the install settings: using what was read before", "error", err)
		return previous
	}
	return values
}

// fresh reads the saved values from the store and keeps them in the cache, unless this process
// wrote while it read
func (r *Resolver) fresh(ctx context.Context) (map[string]string, error) {
	if r.store == nil {
		return map[string]string{}, nil
	}
	r.mu.Lock()
	generation := r.generation
	r.mu.Unlock()
	values := make(map[string]string, len(all))
	for _, s := range all {
		saved, err := r.store.GetByKey(ctx, s.key)
		if err != nil {
			return nil, err
		}
		if saved != nil {
			values[s.key] = saved.Value
		}
	}
	r.mu.Lock()
	// A write that came in while the keys were read may have changed some of them: the caller gets
	// what was read, but the cache keeps it only when no write came, or it would hold the old values
	if r.generation == generation {
		r.values, r.loadedAt, r.stale = values, r.now(), false
	}
	r.mu.Unlock()
	return values, nil
}

// invalidate makes the next call read again, so that it sees this process's write. The cache keeps
// saved, the values the write stored, or what was read before when saved is nil, so that a failed
// read answers them and not the defaults.
func (r *Resolver) invalidate(saved map[string]string) {
	r.mu.Lock()
	r.generation++
	r.stale = true
	if saved != nil {
		r.values = saved
	}
	r.mu.Unlock()
}

func checkSignupMode(value string) string {
	if value != config.SignupModeInvite && value != config.SignupModeOpen {
		return fmt.Sprintf("must be %s or %s", config.SignupModeInvite, config.SignupModeOpen)
	}
	return ""
}

func checkPublicURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "must be the web app's address, such as https://peekaping.example.com"
	}
	return ""
}

func checkHost(value string) string {
	if strings.ContainsAny(value, " \t\r\n/") {
		return "must be a host name or an IP address, such as smtp.example.com"
	}
	return ""
}

func checkPort(value string) string {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return "must be a port number from 1 to 65535"
	}
	return ""
}

func checkOneLine(value string) string {
	if strings.ContainsAny(value, "\r\n") {
		return "must not have a line break"
	}
	return ""
}

func checkFrom(value string) string {
	if strings.ContainsAny(value, "\r\n") {
		return "must not have a line break"
	}
	if _, err := mail.ParseAddress(value); err != nil {
		return "must be an email address such as no-reply@example.com or Peekaping <no-reply@example.com>"
	}
	return ""
}

func checkTLS(value string) string {
	switch value {
	case config.AccountSMTPStartTLS, config.AccountSMTPTLS, config.AccountSMTPNone:
		return ""
	}
	return fmt.Sprintf("must be one of: %s, %s, %s", config.AccountSMTPStartTLS, config.AccountSMTPTLS, config.AccountSMTPNone)
}
