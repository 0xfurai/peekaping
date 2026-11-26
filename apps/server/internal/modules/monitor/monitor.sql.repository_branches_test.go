package monitor

import (
	"context"
	"errors"
	"testing"

	"peekaping/internal/limits"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace/workspaceid"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func TestSQLRepository_UpdatePartialWritesEachFieldToItsColumn(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		proxyID := uuid.NewString()
		_, err := db.Exec("INSERT INTO proxies (id, workspace_id, protocol, host, port) VALUES (?, ?, 'http', 'proxy.example.com', 8080)", proxyID, string(testWorkspace))
		require.NoError(t, err)
		created, err := repo.Create(ctx, testWorkspace, createTestMonitor("Before", true, shared.MonitorStatusPending))
		require.NoError(t, err)
		require.Equal(t, 60, created.Interval)
		require.Equal(t, 30, created.Timeout)
		require.Equal(t, 3, created.MaxRetries)
		require.Equal(t, 5, created.ResendInterval)

		monitorType, name, config, pushToken := "tcp", "After", `{"host":"example.com","port":443}`, "after-token"
		interval, timeout, maxRetries, retryInterval, resendInterval := 90, 45, 7, 80, 9
		active, status := false, shared.MonitorStatusDown
		rule, threshold := shared.LocationRuleAtLeast, 2
		require.NoError(t, repo.UpdatePartial(ctx, testWorkspace, created.ID, &UpdateModel{
			Type: &monitorType, Name: &name, Interval: &interval, Timeout: &timeout, MaxRetries: &maxRetries,
			RetryInterval: &retryInterval, ResendInterval: &resendInterval,
			LocationRule: &rule, LocationThreshold: &threshold,
			Active: &active, Status: &status, Config: &config, ProxyId: &proxyID, PushToken: &pushToken,
		}))

		found, err := repo.FindByID(ctx, testWorkspace, created.ID)
		require.NoError(t, err)
		assert.Equal(t, "tcp", found.Type)
		assert.Equal(t, "After", found.Name)
		assert.Equal(t, 90, found.Interval)
		assert.Equal(t, 45, found.Timeout)
		assert.Equal(t, 7, found.MaxRetries)
		assert.Equal(t, 80, found.RetryInterval)
		assert.Equal(t, 9, found.ResendInterval)
		assert.Equal(t, shared.LocationRuleAtLeast, found.LocationRule)
		require.NotNil(t, found.LocationThreshold)
		assert.Equal(t, 2, *found.LocationThreshold)
		assert.False(t, found.Active)
		assert.Equal(t, shared.MonitorStatusDown, found.Status)
		assert.JSONEq(t, config, found.Config, "PostgreSQL keeps JSON in its own form")
		assert.Equal(t, proxyID, found.ProxyId)
		assert.Equal(t, "after-token", found.PushToken)
	})
}

func TestSQLRepository_UpdatePartialLeavesWhatItDoesNotSet(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		before := createTestMonitor("Before", true, shared.MonitorStatusUp)
		created, err := repo.Create(ctx, testWorkspace, before)
		require.NoError(t, err)
		for _, tc := range []struct {
			name   string
			update *UpdateModel
		}{
			{"max_retries", &UpdateModel{MaxRetries: new(int)}},
			{"resend_interval", &UpdateModel{ResendInterval: new(int)}},
			{"interval", &UpdateModel{Interval: new(int)}},
		} {
			require.NoError(t, repo.UpdatePartial(ctx, testWorkspace, created.ID, tc.update), tc.name)
		}

		found, err := repo.FindByID(ctx, testWorkspace, created.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, found.MaxRetries, "a zero is a value to write")
		assert.Equal(t, 0, found.ResendInterval)
		assert.Equal(t, 0, found.Interval)
		assert.Equal(t, 30, found.Timeout, "and the fields the updates leave out keep their values")
		assert.Equal(t, 60, found.RetryInterval)
		assert.Equal(t, "Before", found.Name)
		assert.Equal(t, before.Config, found.Config)
		assert.Equal(t, before.PushToken, found.PushToken)
		assert.Equal(t, shared.MonitorStatusUp, found.Status)
		assert.True(t, found.Active)
	})
}

func TestSQLRepository_UpdatePartialClearsAProxyAPushTokenAndAThreshold(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		proxyID := uuid.NewString()
		_, err := db.Exec("INSERT INTO proxies (id, workspace_id, protocol, host, port) VALUES (?, ?, 'http', 'proxy.example.com', 8080)", proxyID, string(testWorkspace))
		require.NoError(t, err)
		viaProxy := createTestMonitor("Via proxy", true, shared.MonitorStatusPending)
		viaProxy.ProxyId = proxyID
		viaProxy.LocationRule = shared.LocationRuleAtLeast
		two := 2
		viaProxy.LocationThreshold = &two
		created, err := repo.Create(ctx, testWorkspace, viaProxy)
		require.NoError(t, err)
		empty, all := "", shared.LocationRuleAll

		require.NoError(t, repo.UpdatePartial(ctx, testWorkspace, created.ID, &UpdateModel{ProxyId: &empty, PushToken: &empty, LocationRule: &all}))

		found, err := repo.FindByID(ctx, testWorkspace, created.ID)
		require.NoError(t, err)
		assert.Empty(t, found.ProxyId)
		assert.Empty(t, found.PushToken)
		assert.Equal(t, shared.LocationRuleAll, found.LocationRule)
		assert.Nil(t, found.LocationThreshold, "a rule without a threshold clears it")
		var nulls struct {
			Proxy bool `bun:"proxy_null"`
			Token bool `bun:"token_null"`
		}
		require.NoError(t, db.NewSelect().Table("monitors").
			ColumnExpr("proxy_id IS NULL AS proxy_null").ColumnExpr("push_token IS NULL AS token_null").
			Where("id = ?", created.ID).Scan(ctx, &nulls))
		assert.True(t, nulls.Proxy, "the proxy is NULL, not an empty string")
		assert.True(t, nulls.Token, "and so is the push token")
	})
}

func TestSQLRepository_UpdatePartialOfAMonitorThatIsGone(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		name, missing := "Gone", uuid.NewString()

		assert.NoError(t, repo.UpdatePartialAnyWorkspace(ctx, missing, &UpdateModel{}), "nothing to write, nothing to find")
		assert.NoError(t, repo.UpdatePartialAnyWorkspace(ctx, missing, &UpdateModel{Name: &name}), "a background write of a monitor that is gone is not an error")
		assert.ErrorIs(t, repo.UpdatePartial(ctx, testWorkspace, missing, &UpdateModel{Name: &name}), ErrMonitorNotFound)
		assert.ErrorIs(t, repo.UpdatePartial(ctx, testWorkspace, missing, &UpdateModel{}), ErrMonitorNotFound)
	})
}

// failingLimits is a limits.Source that can't be read
type failingLimits struct{}

func (failingLimits) For(context.Context, workspaceid.ID) (limits.Limits, limits.Origin, error) {
	return limits.Limits{}, limits.OriginNone, errors.New("limits unavailable")
}

func (failingLimits) Snapshot(context.Context) (limits.Snapshot, error) {
	return limits.Snapshot{}, errors.New("limits unavailable")
}

func TestSQLRepository_LimitsThatCannotBeReadFailTheReadsThatNeedThem(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db, WithRepositoryLimits(failingLimits{}))
		ctx := context.Background()
		plain := NewSQLRepository(db)
		_, err := plain.Create(ctx, testWorkspace, monitorOn("Active", true, "local"))
		require.NoError(t, err)
		paused, err := plain.Create(ctx, testWorkspace, monitorOn("Paused", false, "local"))
		require.NoError(t, err)

		_, err = repo.FindActivePaginatedAnyWorkspace(ctx, 0, 10)
		assert.EqualError(t, err, "read the workspaces' limits: limits unavailable")
		_, _, err = repo.AddProbeToAllAnyWorkspace(ctx, "local", nil)
		assert.EqualError(t, err, "read the workspaces' limits: limits unavailable")
		_, err = repo.FindAll(ctx, testWorkspace, 0, 10, "", nil, nil, nil)
		assert.EqualError(t, err, "read the workspaces' limits: limits unavailable", "a read of an active monitor asks")

		only := false
		pausedOnly, err := repo.FindAll(ctx, testWorkspace, 0, 10, "", &only, nil, nil)
		require.NoError(t, err, "a read of paused monitors asks nothing")
		assert.Equal(t, []string{paused.ID}, idsOf(pausedOnly))
	})
}
