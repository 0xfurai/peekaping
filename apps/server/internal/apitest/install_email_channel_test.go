package apitest

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/smtpclient/smtptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PP-370: the install email channel sends through the install's account-email sender, to a member
// of the channel's workspace

const (
	channelsPath       = "/api/v1/notification-channels"
	channelTestPath    = "/api/v1/notification-channels/test"
	installEmailMember = "member@example.com"
)

func installEmailBody(name, to string) map[string]any {
	return map[string]any{"name": name, "type": "install_email", "active": true, "config": `{"to":"` + to + `"}`}
}

// installEmailTeam is the first workspace with a member besides its owner
func installEmailTeam(t *testing.T, a *app) workspace.ID {
	t.Helper()
	id := a.firstWorkspace(t)
	a.userIn(t, installEmailMember, id, workspace.RoleMember)
	return id
}

func TestInstallEmailChannel_AMembersAddressIsSavedAndTheNotificationGoesToIt(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		installEmailTeam(t, a)
		a.takeNotificationsFromTheQueue(t)

		// the address is compared trimmed and ignoring case
		w := a.request(t, http.MethodPost, channelsPath, installEmailBody("Mail the team", " Member@Example.COM "), a.asAdmin)
		channel := decode[created](t, w, http.StatusCreated).ID
		decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors",
			with(pushMonitorBody("Nightly backup", "install-email-token"), "notification_ids", []string{channel}), a.asAdmin), http.StatusCreated)

		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/push/install-email-token?status=down&msg=disk%20full", nil).Code)

		// the address goes out as it was typed, trimmed
		const typed = "Member@Example.COM"
		require.Eventually(t, func() bool { return len(mailTo(mail, typed)) >= 1 }, 10*time.Second, 20*time.Millisecond, "the notification is mailed to the member")
		sent := mailTo(mail, typed)[0]
		assert.Equal(t, senderAddress, sent.From, "from the install's sender")
		assert.Equal(t, "Peekaping Notification", subjectOf(t, sent))
		assert.Contains(t, textOf(t, sent), "disk full")
	})
}

func TestInstallEmailChannel_AnotherAddressIsRefusedOnCreateUpdateAndTest(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		installEmailTeam(t, a)
		// someone with an account in another workspace is a stranger to this one
		second := a.createWorkspace(t, "Second")
		a.userIn(t, "bob@example.com", second, workspace.RoleOwner)
		channel := decode[created](t, a.request(t, http.MethodPost, channelsPath, installEmailBody("Mail the team", installEmailMember), a.asAdmin), http.StatusCreated).ID
		before := len(mail.Messages())

		for _, stranger := range []string{"stranger@example.com", "bob@example.com"} {
			t.Run(stranger, func(t *testing.T) {
				create := a.request(t, http.MethodPost, channelsPath, installEmailBody("Mail a stranger", stranger), a.asAdmin)
				assert.Equal(t, http.StatusBadRequest, create.Code, create.Body.String())
				assert.Contains(t, create.Body.String(), "member of the workspace")

				update := a.request(t, http.MethodPut, channelsPath+"/"+channel, installEmailBody("Mail a stranger", stranger), a.asAdmin)
				assert.Equal(t, http.StatusBadRequest, update.Code, update.Body.String())
				patch := a.request(t, http.MethodPatch, channelsPath+"/"+channel, map[string]any{"config": `{"to":"` + stranger + `"}`}, a.asAdmin)
				assert.Equal(t, http.StatusBadRequest, patch.Code, patch.Body.String())

				test := a.request(t, http.MethodPost, channelTestPath, installEmailBody("Mail a stranger", stranger), a.asAdmin)
				assert.Equal(t, http.StatusBadRequest, test.Code, test.Body.String())
			})
		}

		// a malformed address is a 400 as well, and the channel is as it was
		assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, channelsPath, installEmailBody("Broken", "not-an-address"), a.asAdmin).Code)
		kept := decode[struct {
			Config string `json:"config"`
		}](t, a.request(t, http.MethodGet, channelsPath+"/"+channel, nil, a.asAdmin), http.StatusOK)
		assert.JSONEq(t, `{"to":"`+installEmailMember+`"}`, kept.Config)
		a.settled(t)
		assert.Len(t, mail.Messages(), before, "nothing was sent to a stranger")

		// a member's address is still fine, for a test as well
		test := a.request(t, http.MethodPost, channelTestPath, installEmailBody("Mail the team", installEmailMember), a.asAdmin)
		assert.Equal(t, http.StatusOK, test.Code, test.Body.String())
		assert.Len(t, mailTo(mail, installEmailMember), 1)
	})
}

func TestInstallEmailChannel_WithoutASenderACreateAndATestAreRefused(t *testing.T) {
	forEachDatabaseWith(t, []func(*config.Config){selfHosted}, func(t *testing.T, a *app) {
		installEmailTeam(t, a)

		create := a.request(t, http.MethodPost, channelsPath, installEmailBody("Mail the team", installEmailMember), a.asAdmin)
		assert.Equal(t, http.StatusBadRequest, create.Code, create.Body.String())
		assert.Contains(t, create.Body.String(), "no sender for account emails")

		test := a.request(t, http.MethodPost, channelTestPath, installEmailBody("Mail the team", installEmailMember), a.asAdmin)
		assert.Equal(t, http.StatusBadRequest, test.Code, test.Body.String())
		assert.Contains(t, test.Body.String(), "no sender for account emails")

		list := decode[[]map[string]any](t, a.request(t, http.MethodGet, channelsPath, nil, a.asAdmin), http.StatusOK)
		assert.Empty(t, list, "nothing was saved")
	})
}

func TestInstallEmailChannel_WhenTheRecipientLeavesTheWorkspaceNothingIsSent(t *testing.T) {
	forEachDatabaseWithMail(t, func(t *testing.T, a *app, mail *smtptest.Server) {
		installEmailTeam(t, a)
		memberID := a.userID(t, installEmailMember)
		a.takeNotificationsFromTheQueue(t)
		channel := decode[created](t, a.request(t, http.MethodPost, channelsPath, installEmailBody("Mail the team", installEmailMember), a.asAdmin), http.StatusCreated).ID
		// a second channel to the owner, who stays: its mail shows that the second notification was due
		stays := decode[created](t, a.request(t, http.MethodPost, channelsPath, installEmailBody("Mail the owner", adminEmail), a.asAdmin), http.StatusCreated).ID
		decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors",
			with(pushMonitorBody("Nightly backup", "left-token"), "notification_ids", []string{channel, stays}), a.asAdmin), http.StatusCreated)
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/push/left-token?status=down&msg=first", nil).Code)
		require.Eventually(t, func() bool { return len(mailTo(mail, installEmailMember)) == 1 && len(mailTo(mail, adminEmail)) >= 1 }, 10*time.Second, 20*time.Millisecond)
		ownerBefore := len(mailTo(mail, adminEmail))

		require.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, membersPath+"/"+memberID, nil, a.asAdmin).Code)
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/push/left-token?status=up", nil).Code)
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/push/left-token?status=down&msg=second", nil).Code)

		require.Eventually(t, func() bool { return len(mailTo(mail, adminEmail)) > ownerBefore }, 10*time.Second, 20*time.Millisecond, "the owner is told of the second outage")
		time.Sleep(time.Second) // long enough for a send that shouldn't happen
		a.settled(t)
		assert.Len(t, mailTo(mail, installEmailMember), 1, "the former member got nothing more")
		// and the channel is refused for them now
		test := a.request(t, http.MethodPost, channelTestPath, installEmailBody("Mail the team", installEmailMember), a.asAdmin)
		assert.Equal(t, http.StatusBadRequest, test.Code, test.Body.String())
		assert.False(t, strings.Contains(test.Body.String(), "ACCOUNT_SMTP"), "the sender is there")
	})
}
