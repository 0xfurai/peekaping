package app

import (
	"context"
	"testing"

	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe_notification"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

// A MongoDB install has no probes (ADR 0001), and must still start: the probe services resolve,
// without a Mongo repository, and list no probes.
func TestRegister_MongoInstallResolvesTheProbeServices(t *testing.T) {
	cfg := clusterConfig(t)
	cfg.DBType, cfg.DBName = "mongo", "peekaping"
	container := dig.New()
	require.NoError(t, Register(container, cfg))

	require.NoError(t, container.Invoke(func(probes probe.Service, channels probe_notification.Service) {
		all, err := probes.FindAll(context.Background())
		require.NoError(t, err)
		assert.Empty(t, all)
		assert.NoError(t, channels.DeleteByNotificationID(context.Background(), "any"))
	}))
}

func TestRegister_SQLiteInstallResolvesTheProbeServices(t *testing.T) {
	container := dig.New()
	require.NoError(t, Register(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(probes probe.Service, channels probe_notification.Service) {
		assert.NotNil(t, probes)
		assert.NotNil(t, channels)
	}))
}
