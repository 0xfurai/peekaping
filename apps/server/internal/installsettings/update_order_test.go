package installsettings

import (
	"errors"
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdate_StopsWithTheReadErrorWhenTheSavedValuesCantBeRead(t *testing.T) {
	store := newMemoryStore()
	r, _ := newResolver(t, defaults(), store)
	store.fail = errors.New("database is gone")

	changed, err := r.Update(ctx, Change{PublicURL: ptr("https://peek.example")})

	assert.EqualError(t, err, "database is gone")
	assert.Nil(t, changed)
	_, stored := store.get(KeyPublicURL)
	assert.False(t, stored)
}

// Update stops at the first change it refuses: a limit it doesn't know, then the other settings
// in the order of the admin API's form, then the default limits, in the order of their names
func TestUpdate_NamesTheFirstChangeItRefuses(t *testing.T) {
	locked := defaults()
	locked.SetFromEnvironment("SIGNUP_MODE")
	locked.SetFromEnvironment("CLIENT_URL")
	for name, tc := range map[string]struct {
		cfg    func() *config.Config
		change Change
		code   string
		field  string
	}{
		"a limit that isn't one before an invalid mode": {nil, Change{SignupMode: ptr("everyone"), WorkspaceLimits: map[string]*int{"robots": ptr(1)}},
			CodeInvalid, "workspaceLimits.robots"},
		"the mode before the public URL": {nil, Change{SignupMode: ptr("everyone"), PublicURL: ptr("peek.example")},
			CodeInvalid, "signupMode"},
		"the public URL before a limit": {nil, Change{PublicURL: ptr("peek.example"), WorkspaceLimits: map[string]*int{LimitMonitors: ptr(0)}},
			CodeInvalid, "publicUrl"},
		"the name of the limits before a limit": {nil, Change{WorkspaceLimitsName: ptr("a\nb"), WorkspaceLimits: map[string]*int{LimitMonitors: ptr(0)}},
			CodeInvalid, "workspaceLimitsName"},
		"the first limit in the order of their names": {nil, Change{WorkspaceLimits: map[string]*int{LimitStatusPages: ptr(0), LimitMembers: ptr(0), LimitMonitors: ptr(0)}},
			CodeInvalid, "workspaceLimits.monitors"},
		"a locked mode before an invalid public URL": {func() *config.Config { return locked }, Change{PublicURL: ptr("peek.example"), SignupMode: ptr("invite")},
			CodeLocked, "signupMode"},
		"a locked setting is locked whatever the value": {func() *config.Config { return locked }, Change{SignupMode: ptr("everyone")},
			CodeLocked, "signupMode"},
		"a locked setting can't be cleared": {func() *config.Config { return locked }, Change{PublicURL: ptr("")},
			CodeLocked, "publicUrl"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := defaults()
			if tc.cfg != nil {
				cfg = tc.cfg()
			}
			r, _ := newResolver(t, cfg, newMemoryStore())

			_, err := r.Update(ctx, tc.change)

			var refused *Error
			require.ErrorAs(t, err, &refused)
			assert.Equal(t, tc.code, refused.Code)
			assert.Equal(t, tc.field, refused.Field)
		})
	}
}

// A password is taken as sent, a space included, so a password of spaces is one; any other setting
// is trimmed, and one of spaces is empty
func TestUpdate_TrimsEverySettingButThePassword(t *testing.T) {
	store := savedSender()
	r, _ := newResolver(t, defaults(), store)

	changed, err := r.Update(ctx, Change{SMTPPassword: ptr("   "), SMTPUser: ptr("   "), SMTPTLS: ptr(" none ")})

	require.NoError(t, err)
	assert.Equal(t, []string{"accountSmtp.user", "accountSmtp.password", "accountSmtp.tls"}, changed)
	password, _ := store.get(KeySMTPPassword)
	assert.Equal(t, "   ", password)
	_, userStored := store.get(KeySMTPUser)
	assert.False(t, userStored, "a user of spaces is no user")
	tls, _ := store.get(KeySMTPTLS)
	assert.Equal(t, "none", tls)
}
