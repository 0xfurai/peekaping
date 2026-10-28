package monitor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// A monitor links only to the notification channels, tags and proxy of its own workspace
// (docs/adr/0005 section 2). A record of another workspace is refused as one that doesn't exist, and
// everything is checked before the monitor or a link is written (AC4).

func linkedCreateDto() *CreateUpdateDto {
	return &CreateUpdateDto{
		Type: "http", Name: "Checkout", Interval: 60, Timeout: 16, RetryInterval: 60, Active: true, Config: "{}",
		NotificationIds: []string{"channel-a", "channel-a", "channel-b"}, TagIds: []string{"tag-a"}, ProxyId: "8d3f0a56-2c30-4d6d-9c85-2b8f4e5c7a33",
	}
}

func TestMonitorService_ALinkOutsideTheWorkspaceFailsTheWriteBeforeAnythingIsWritten(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		arm   func(notifications *MockMonitorNotificationService, tags *MockMonitorTagService, repo *MockMonitorRepository)
		check error
	}{
		{"channel", func(n *MockMonitorNotificationService, _ *MockMonitorTagService, _ *MockMonitorRepository) {
			n.On("RequireChannels", mock.Anything, testWorkspace, []string{"channel-a", "channel-b"}).Return(fmt.Errorf("%w: channel-b", monitor_notification.ErrUnknownChannel))
		}, monitor_notification.ErrUnknownChannel},
		{"tag", func(_ *MockMonitorNotificationService, t *MockMonitorTagService, _ *MockMonitorRepository) {
			t.On("RequireTags", mock.Anything, testWorkspace, []string{"tag-a"}).Return(fmt.Errorf("%w: tag-a", monitor_tag.ErrUnknownTag))
		}, monitor_tag.ErrUnknownTag},
		{"proxy", func(_ *MockMonitorNotificationService, _ *MockMonitorTagService, r *MockMonitorRepository) {
			r.On("ProxyInWorkspace", mock.Anything, testWorkspace, "8d3f0a56-2c30-4d6d-9c85-2b8f4e5c7a33").Return(false, nil)
		}, ErrProxyNotFound},
	}
	for _, tc := range cases {
		t.Run("create, "+tc.name, func(t *testing.T) {
			service, repo, _, bus, notifications, tags, _, _ := setupMonitorService()
			tc.arm(notifications, tags, repo)

			result, err := service.Create(ctx, testWorkspace, linkedCreateDto())

			assert.ErrorIs(t, err, tc.check)
			assert.Nil(t, result)
			repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
			notifications.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
			tags.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
			assert.Empty(t, bus.Topics(), "no MonitorCreated")
		})
		t.Run("full update, "+tc.name, func(t *testing.T) {
			service, repo, _, bus, notifications, tags, _, _ := setupMonitorService()
			repo.On("FindByID", mock.Anything, testWorkspace, "mon-1").Return(&Model{ID: "mon-1", Type: "http", Interval: 60, Timeout: 16, LocationRule: shared.LocationRuleAny}, nil)
			tc.arm(notifications, tags, repo)

			result, err := service.UpdateFull(ctx, testWorkspace, "mon-1", linkedCreateDto())

			assert.ErrorIs(t, err, tc.check)
			assert.Nil(t, result)
			repo.AssertNotCalled(t, "UpdateFull", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			notifications.AssertNotCalled(t, "DeleteByMonitorID", mock.Anything, mock.Anything)
			tags.AssertNotCalled(t, "DeleteByMonitorID", mock.Anything, mock.Anything)
			assert.Empty(t, bus.Topics(), "no MonitorUpdated")
		})
	}
}

func TestMonitorService_APartialUpdateChecksOnlyTheLinksItNames(t *testing.T) {
	ctx := context.Background()
	current := &Model{ID: "mon-1", Type: "http", Interval: 60, Timeout: 16, LocationRule: shared.LocationRuleAny}

	t.Run("a channel of another workspace", func(t *testing.T) {
		service, repo, _, bus, notifications, _, _, _ := setupMonitorService()
		repo.On("FindByID", mock.Anything, testWorkspace, "mon-1").Return(current, nil)
		notifications.On("RequireChannels", mock.Anything, testWorkspace, []string{"channel-b"}).Return(fmt.Errorf("%w: channel-b", monitor_notification.ErrUnknownChannel))

		_, err := service.UpdatePartial(ctx, testWorkspace, "mon-1", &PartialUpdateDto{NotificationIds: []string{"channel-b"}}, false)

		assert.ErrorIs(t, err, monitor_notification.ErrUnknownChannel)
		repo.AssertNotCalled(t, "UpdatePartial", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		assert.Empty(t, bus.Topics())
	})

	t.Run("a proxy of another workspace", func(t *testing.T) {
		service, repo, _, _, _, _, _, _ := setupMonitorService()
		proxyID := "8d3f0a56-2c30-4d6d-9c85-2b8f4e5c7a33"
		repo.On("FindByID", mock.Anything, testWorkspace, "mon-1").Return(current, nil)
		repo.On("ProxyInWorkspace", mock.Anything, testWorkspace, proxyID).Return(false, nil)

		_, err := service.UpdatePartial(ctx, testWorkspace, "mon-1", &PartialUpdateDto{ProxyId: &proxyID}, false)

		assert.ErrorIs(t, err, ErrProxyNotFound)
		repo.AssertNotCalled(t, "UpdatePartial", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("clearing the proxy looks nothing up", func(t *testing.T) {
		service, repo, _, _, _, _, _, _ := setupMonitorService()
		none := ""
		repo.On("FindByID", mock.Anything, testWorkspace, "mon-1").Return(current, nil)
		repo.On("UpdatePartial", mock.Anything, testWorkspace, "mon-1", mock.Anything).Return(nil)
		repo.On("ProxyInWorkspace", mock.Anything, mock.Anything, mock.Anything).Return(false, errors.New("must not be asked")).Maybe()

		_, err := service.UpdatePartial(ctx, testWorkspace, "mon-1", &PartialUpdateDto{ProxyId: &none}, false)

		require.NoError(t, err)
		repo.AssertNotCalled(t, "ProxyInWorkspace", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("a background update names no link and checks none", func(t *testing.T) {
		service, repo, _, _, notifications, tags, _, _ := setupMonitorService()
		status := shared.MonitorStatusDown
		repo.On("FindByIDAnyWorkspace", mock.Anything, "mon-1").Return(current, nil)
		repo.On("UpdatePartialAnyWorkspace", mock.Anything, "mon-1", mock.Anything).Return(nil)

		_, err := service.UpdatePartialAnyWorkspace(ctx, "mon-1", &PartialUpdateDto{Status: &status}, true)

		require.NoError(t, err)
		notifications.AssertNotCalled(t, "RequireChannels", mock.Anything, mock.Anything, mock.Anything)
		tags.AssertNotCalled(t, "RequireTags", mock.Anything, mock.Anything, mock.Anything)
		repo.AssertNotCalled(t, "ProxyInWorkspace", mock.Anything, mock.Anything, mock.Anything)
	})
}
