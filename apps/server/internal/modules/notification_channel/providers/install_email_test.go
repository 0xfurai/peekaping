package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAccountMailer records what it is asked to send
type fakeAccountMailer struct {
	enabled bool
	err     error

	mu   sync.Mutex
	sent []sentEmail
}

type sentEmail struct{ to, subject, text, html string }

func (f *fakeAccountMailer) Enabled() bool { return f.enabled }

func (f *fakeAccountMailer) Send(_ context.Context, to, subject, text, html string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, sentEmail{to, subject, text, html})
	return nil
}

// fakeMembers holds the members' emails by workspace, and compares them as the repository does. A
// member is verified unless the address is in unverified (in lower case).
type fakeMembers struct {
	emails     map[workspace.ID][]string
	unverified map[string]bool
	err        error
}

func (f *fakeMembers) HasVerifiedMemberWithEmail(ctx context.Context, id workspace.ID, email string) (bool, error) {
	member, err := f.HasMemberWithEmail(ctx, id, email)
	return member && !f.unverified[strings.ToLower(email)], err
}

func (f *fakeMembers) HasMemberWithEmail(_ context.Context, id workspace.ID, email string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	for _, e := range f.emails[id] {
		if strings.EqualFold(e, email) {
			return true, nil
		}
	}
	return false, nil
}

const installEmailWorkspace = workspace.ID("ws-1")

func newInstallEmailSender(enabled bool, members ...string) (*InstallEmailSender, *fakeAccountMailer, *fakeMembers) {
	mail := &fakeAccountMailer{enabled: enabled}
	m := &fakeMembers{emails: map[workspace.ID][]string{installEmailWorkspace: members}}
	return NewInstallEmailSender(nopLogger(), goldenAppConfig, mail, m), mail, m
}

func inInstallEmailWorkspace() context.Context {
	return WithWorkspace(context.Background(), installEmailWorkspace)
}

// installEmailGoldenCases cover what the listener sends to every channel type
var installEmailGoldenCases = []goldenCase{
	{name: "install_email_down", event: downEvent, config: `{"to": "oncall@example.com"}`},
	{name: "install_email_up", event: upEvent, config: `{"to": "oncall@example.com"}`},
	{name: "install_email_certificate_expiry", event: certExpiryEvent, config: `{"to": "oncall@example.com"}`},
	{name: "install_email_probe_offline", event: probeOfflineEvent, config: `{"to": "oncall@example.com"}`},
}

func TestInstallEmailChannel_SendsTheNotificationToAMember(t *testing.T) {
	for _, tc := range installEmailGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			sender, mail, _ := newInstallEmailSender(true, "oncall@example.com")
			message, mon, hb := goldenNotification(tc.event)

			require.NoError(t, sender.Validate(tc.config))
			require.NoError(t, sender.ValidateInWorkspace(inInstallEmailWorkspace(), tc.config))
			require.NoError(t, sender.Send(inInstallEmailWorkspace(), tc.config, message, mon, hb))

			require.Len(t, mail.sent, 1)
			sent := mail.sent[0]
			assertGolden(t, tc.name, fmt.Sprintf("To: %s\nSubject: %s\n\n%s\n--- HTML\n%s", sent.to, sent.subject, sent.text, sent.html))
		})
	}
}

// The recipient tells from the subject which monitor it is about and what happened, and the body
// says it again with the message, the time and a link to the monitor's page
func TestInstallEmailChannel_TheEmailNamesTheMonitorAndItsStatus(t *testing.T) {
	sender, mail, _ := newInstallEmailSender(true, "oncall@example.com")
	message, mon, hb := goldenNotification(downEvent)
	require.NoError(t, sender.Send(inInstallEmailWorkspace(), `{"to": "oncall@example.com"}`, message, mon, hb))
	message, mon, hb = goldenNotification(upEvent)
	require.NoError(t, sender.Send(inInstallEmailWorkspace(), `{"to": "oncall@example.com"}`, message, mon, hb))

	require.Len(t, mail.sent, 2)
	down, up := mail.sent[0], mail.sent[1]
	assert.Equal(t, "Public API is Down", down.subject)
	assert.Equal(t, "Public API is Up", up.subject)
	link := "https://peekaping.example.com/w/ws-1/monitors/mon-1"
	for _, body := range []string{down.text, down.html} {
		assert.Contains(t, body, "Public API")
		assert.Contains(t, body, "Down")
		assert.Contains(t, body, "connection refused")
		assert.Contains(t, body, "2026-10-17 09:30:00 UTC")
		assert.Contains(t, body, link)
	}
	assert.Contains(t, up.text, "200 - OK")
}

// The mailer refuses a subject with a line break, so a monitor name with one is put on one line in
// the subject, and the body has the name as it is
func TestInstallEmailChannel_AMonitorNameWithALineBreakIsOneLineInTheSubject(t *testing.T) {
	sender, mail, _ := newInstallEmailSender(true, "oncall@example.com")
	const channel = `{"to": "oncall@example.com"}`
	message, golden, hb := goldenNotification(downEvent)
	mon := *golden
	mon.Name = "Public\nAPI\r\n"
	require.NoError(t, sender.Send(inInstallEmailWorkspace(), channel, message, &mon, hb))
	certificate := &monitor.Model{Name: "\tEdge\rproxy", Type: "http"}
	require.NoError(t, sender.Send(inInstallEmailWorkspace(), channel, "the certificate expires", certificate, nil))

	require.Len(t, mail.sent, 2)
	assert.Equal(t, "Public API is Down", mail.sent[0].subject)
	assert.Contains(t, mail.sent[0].text, "Monitor: Public\nAPI\r\n")
	assert.Equal(t, defaultEmailSubject+": Edge proxy", mail.sent[1].subject)
	assert.Contains(t, mail.sent[1].text, "Monitor: \tEdge\rproxy")
}

// A test notification has a monitor that isn't saved and a heartbeat with no time: the email has
// no link and no time. Without a monitor or a public URL it has neither link nor name.
func TestInstallEmailChannel_AnEmailWithoutAMonitorPageOrHeartbeatLeavesThemOut(t *testing.T) {
	sender, mail, _ := newInstallEmailSender(true, "oncall@example.com")
	const channel = `{"to": "oncall@example.com"}`
	testMonitor := &monitor.Model{Name: "Test Monitor", Type: "http"}
	testHeartbeat := &heartbeat.Model{Status: shared.MonitorStatusDown, Msg: "This is a test notification from Peekaping"}
	require.NoError(t, sender.Send(inInstallEmailWorkspace(), channel, testHeartbeat.Msg, testMonitor, testHeartbeat))
	require.NoError(t, sender.Send(inInstallEmailWorkspace(), channel, "general", nil, nil))
	noURL := NewInstallEmailSender(nopLogger(), &config.Config{}, mail, &fakeMembers{emails: map[workspace.ID][]string{installEmailWorkspace: {"oncall@example.com"}}})
	message, mon, hb := goldenNotification(downEvent)
	require.NoError(t, noURL.Send(inInstallEmailWorkspace(), channel, message, mon, hb))

	require.Len(t, mail.sent, 3)
	test, general, unlinked := mail.sent[0], mail.sent[1], mail.sent[2]
	assert.Equal(t, "Test Monitor is Down", test.subject)
	assert.Contains(t, test.text, "This is a test notification from Peekaping")
	assert.NotContains(t, test.text, "Time:")
	assert.NotContains(t, test.text, "http")
	assert.Equal(t, defaultEmailSubject, general.subject)
	assert.Contains(t, general.text, "general")
	assert.Equal(t, "Public API is Down", unlinked.subject)
	assert.NotContains(t, unlinked.text, "/monitors/")
}

func TestInstallEmailChannel_TheAddressIsComparedTrimmedAndIgnoringCase(t *testing.T) {
	sender, mail, _ := newInstallEmailSender(true, "Oncall@Example.com")

	require.NoError(t, sender.ValidateInWorkspace(inInstallEmailWorkspace(), `{"to": "  oncall@example.COM "}`))
	require.NoError(t, sender.Send(inInstallEmailWorkspace(), `{"to": "  oncall@example.COM "}`, "down", nil, nil))

	require.Len(t, mail.sent, 1)
	assert.Equal(t, "oncall@example.COM", mail.sent[0].to, "the address is trimmed")
}

func TestInstallEmailChannel_AnAddressThatIsNoMemberIsRefusedAndNothingIsSent(t *testing.T) {
	sender, mail, _ := newInstallEmailSender(true, "oncall@example.com")
	const stranger = `{"to": "stranger@example.com"}`

	err := sender.ValidateInWorkspace(inInstallEmailWorkspace(), stranger)
	assert.ErrorIs(t, err, ErrRecipientNotMember)

	err = sender.Send(inInstallEmailWorkspace(), stranger, "down", nil, nil)
	assert.ErrorIs(t, err, ErrRecipientNotMember)
	assert.Empty(t, mail.sent)

	// a member of another workspace is no member of this one
	other := WithWorkspace(context.Background(), "ws-2")
	assert.ErrorIs(t, sender.ValidateInWorkspace(other, `{"to": "oncall@example.com"}`), ErrRecipientNotMember)
	assert.ErrorIs(t, sender.Send(other, `{"to": "oncall@example.com"}`, "down", nil, nil), ErrRecipientNotMember)
	assert.Empty(t, mail.sent)
}

func TestInstallEmailChannel_ARecipientWhoLeftTheWorkspaceGetsNothingAndTheSendFails(t *testing.T) {
	sender, mail, members := newInstallEmailSender(true, "oncall@example.com")
	const config = `{"to": "oncall@example.com"}`
	require.NoError(t, sender.Send(inInstallEmailWorkspace(), config, "down", nil, nil))
	require.Len(t, mail.sent, 1)

	members.emails[installEmailWorkspace] = nil // they left
	err := sender.Send(inInstallEmailWorkspace(), config, "down again", nil, nil)

	assert.ErrorIs(t, err, ErrRecipientNotMember)
	assert.Len(t, mail.sent, 1, "nothing more went out")
}

func TestInstallEmailChannel_AnUnverifiedMembersChannelIsAcceptedButNothingIsSentToThem(t *testing.T) {
	sender, mail, members := newInstallEmailSender(true, "oncall@example.com")
	members.unverified = map[string]bool{"oncall@example.com": true}
	const config = `{"to": "oncall@example.com"}`

	// a cloud signup sets the channel up before verifying
	require.NoError(t, sender.ValidateInWorkspace(inInstallEmailWorkspace(), config))
	err := sender.Send(inInstallEmailWorkspace(), config, "down", nil, nil)

	assert.ErrorIs(t, err, ErrRecipientNotVerified)
	assert.NotErrorIs(t, err, ErrRecipientNotMember)
	assert.Empty(t, mail.sent)

	// once they verify, the same channel sends
	members.unverified = nil
	require.NoError(t, sender.Send(inInstallEmailWorkspace(), config, "down", nil, nil))
	assert.Len(t, mail.sent, 1)
}

func TestInstallEmailChannel_WithoutASenderNothingIsAcceptedOrSent(t *testing.T) {
	sender, mail, _ := newInstallEmailSender(false, "oncall@example.com")
	const config = `{"to": "oncall@example.com"}`

	assert.ErrorIs(t, sender.ValidateInWorkspace(inInstallEmailWorkspace(), config), ErrNoInstallSender)
	assert.ErrorIs(t, sender.Send(inInstallEmailWorkspace(), config, "down", nil, nil), ErrNoInstallSender)
	assert.Empty(t, mail.sent)
}

func TestInstallEmailChannel_ASendWithoutAWorkspaceSendsNothing(t *testing.T) {
	sender, mail, _ := newInstallEmailSender(true, "oncall@example.com")

	err := sender.Send(context.Background(), `{"to": "oncall@example.com"}`, "down", nil, nil)

	require.Error(t, err)
	assert.Empty(t, mail.sent)
}

func TestInstallEmailChannel_AFailureOfTheSenderOrTheLookupIsReportedWithoutTheAddress(t *testing.T) {
	sender, mail, members := newInstallEmailSender(true, "oncall@example.com")
	const config = `{"to": "oncall@example.com"}`

	mail.err = errors.New("550 5.1.1 <oncall@example.com>: user unknown")
	err := sender.Send(inInstallEmailWorkspace(), config, "down", nil, nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "oncall@example.com")

	mail.err = nil
	members.err = errors.New("database is down")
	err = sender.Send(inInstallEmailWorkspace(), config, "down", nil, nil)
	require.Error(t, err)
	assert.Empty(t, mail.sent)
	assert.NotErrorIs(t, err, ErrRecipientNotMember, "a lookup that failed is not a non-member")
}

func TestInstallEmailChannel_ValidateChecksTheShapeOfTheConfig(t *testing.T) {
	sender, _, _ := newInstallEmailSender(true)

	for name, config := range map[string]string{
		"no recipient":  `{}`,
		"not an email":  `{"to": "oncall"}`,
		"a name":        `{"to": "On Call <oncall@example.com>"}`,
		"not json":      `nope`,
		"two addresses": `{"to": "a@example.com, b@example.com"}`,
	} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, sender.Validate(config))
		})
	}
	assert.NoError(t, sender.Validate(`{"to": "oncall@example.com"}`))
}

// The real mailer is the sender of an install: off without ACCOUNT_SMTP_HOST, and nil-safe
func TestInstallEmailChannel_TheRealMailerFitsTheInterface(t *testing.T) {
	var _ AccountMailer = (*mailer.Mailer)(nil)
	var _ WorkspaceMembers = workspace.MemberRepository(nil)
	var none *mailer.Mailer
	sender := NewInstallEmailSender(nopLogger(), goldenAppConfig, none, &fakeMembers{})

	assert.ErrorIs(t, sender.ValidateInWorkspace(inInstallEmailWorkspace(), `{"to": "a@example.com"}`), ErrNoInstallSender)
}
