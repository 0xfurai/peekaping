package probe_notification_test

import (
	"context"
	"testing"

	"peekaping/internal/modules/probe_notification"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A database that fails leaves the links as they were: the failure is the caller's to see, and is
// not taken for an unknown channel
func TestProbeNotifications_ReplaceFailsWhenTheDatabaseDoes(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		edge := f.probe()
		slack, email := f.channel(), f.channel()
		require.NoError(t, f.svc.Replace(ctx, edge, []string{slack}, f.members))
		gone, cancel := context.WithCancel(ctx)
		cancel()

		for name, channels := range map[string][]string{"naming channels": {email}, "naming none": {}} {
			err := f.svc.Replace(gone, edge, channels, f.members)

			require.Error(t, err, name)
			assert.NotErrorIs(t, err, probe_notification.ErrUnknownChannel, name)
			assert.Equal(t, []string{slack}, channelsOf(t, f.svc, edge), name)
		}
	})
}

// Of several channels that are not there, the error names the first the caller gave
func TestProbeNotifications_ReplaceNamesTheFirstChannelThatIsNotThere(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		edge := f.probe()
		slack := f.channel()
		first, second := uuid.NewString(), uuid.NewString()

		err := f.svc.Replace(context.Background(), edge, []string{slack, first, second}, f.members)

		require.ErrorIs(t, err, probe_notification.ErrUnknownChannel)
		assert.EqualError(t, err, probe_notification.ErrUnknownChannel.Error()+": "+first)
		assert.Empty(t, channelsOf(t, f.svc, edge))

		err = f.svc.Replace(context.Background(), edge, []string{slack, second, first}, f.members)

		assert.EqualError(t, err, probe_notification.ErrUnknownChannel.Error()+": "+second)
	})
}

// The channel list is read before anything else: a list with a name that is not an id is refused
// whatever the caller's workspaces are
func TestProbeNotifications_ReplaceRefusesANameThatIsNotAnIDBeforeLookingAtWorkspaces(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		err := f.svc.Replace(context.Background(), f.probe(), []string{"nope"}, nil)

		assert.EqualError(t, err, probe_notification.ErrUnknownChannel.Error()+": nope")
	})
}

// The links of a replaced probe are all new, and carry one time, the swap's
func TestProbeNotifications_ReplaceGivesTheNewLinksOneTime(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		edge := f.probe()
		a, b, c := f.channel(), f.channel(), f.channel()

		require.NoError(t, f.svc.Replace(ctx, edge, []string{c, a, b}, f.members))

		links, err := f.svc.FindByProbeID(ctx, edge)
		require.NoError(t, err)
		require.Len(t, links, 3)
		assert.Equal(t, links[0].CreatedAt, links[2].CreatedAt, "one swap, one time")
		assert.ElementsMatch(t, []string{a, b, c}, []string{links[0].NotificationID, links[1].NotificationID, links[2].NotificationID})
	})
}
