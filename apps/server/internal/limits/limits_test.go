package limits_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/installsettings"
	"peekaping/internal/limits"
	"peekaping/internal/modules/workspace/workspaceid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore is a Store in memory that counts its reads
type fakeStore struct {
	own   map[workspaceid.ID]limits.Limits
	reads int
	err   error
}

func (s *fakeStore) AllOwn(context.Context) (map[workspaceid.ID]limits.Limits, error) {
	s.reads++
	if s.err != nil {
		return nil, s.err
	}
	copied := make(map[workspaceid.ID]limits.Limits, len(s.own))
	for id, l := range s.own {
		copied[id] = l
	}
	return copied, nil
}

func (s *fakeStore) SetOwn(_ context.Context, id workspaceid.ID, own *limits.Limits) error {
	if s.own == nil {
		s.own = map[workspaceid.ID]limits.Limits{}
	}
	if own == nil {
		delete(s.own, id)
		return nil
	}
	s.own[id] = *own
	return nil
}

func ptr(n int) *int { return &n }

func TestChain_TheWorkspacesOwnLimitsComeFirstThenTheInstallsThenNone(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{own: map[workspaceid.ID]limits.Limits{"own": {Monitors: ptr(50)}}}
	chain := limits.NewChain(store, limits.FixedDefaults{Monitors: ptr(10), MinCheckInterval: ptr(300)})

	own, origin, err := chain.For(ctx, "own")
	require.NoError(t, err)
	assert.Equal(t, limits.OriginWorkspace, origin)
	assert.Equal(t, limits.Limits{Monitors: ptr(50)}, own, "its own limits replace the defaults whole")

	other, origin, err := chain.For(ctx, "other")
	require.NoError(t, err)
	assert.Equal(t, limits.OriginInstall, origin)
	assert.Equal(t, limits.Limits{Monitors: ptr(10), MinCheckInterval: ptr(300)}, other)

	none, origin, err := limits.NewChain(&fakeStore{}, limits.FixedDefaults{}).For(ctx, "other")
	require.NoError(t, err)
	assert.Equal(t, limits.OriginNone, origin)
	assert.True(t, none.IsZero())
}

func TestChain_NoLimitsOfItsOwnIsAValue(t *testing.T) {
	store := &fakeStore{own: map[workspaceid.ID]limits.Limits{"unlimited": {}}}
	chain := limits.NewChain(store, limits.FixedDefaults{Monitors: ptr(10)})

	got, origin, err := chain.For(context.Background(), "unlimited")

	require.NoError(t, err)
	assert.Equal(t, limits.OriginWorkspace, origin)
	assert.True(t, got.IsZero(), "the install's defaults don't apply")
}

func TestChain_AnInstallThatSetsNothingHasNoLimits(t *testing.T) {
	chain := limits.NewChain(&fakeStore{}, limits.SettingsDefaults{Settings: installsettings.Static(&config.Config{})})

	got, origin, err := chain.For(context.Background(), "any")

	require.NoError(t, err)
	assert.Equal(t, limits.OriginNone, origin)
	assert.True(t, got.IsZero())
	snapshot, err := chain.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Nil(t, snapshot.Lowest(limits.Monitors))
}

func TestSettingsDefaults_ReadsEachVariableAndZeroIsNoLimit(t *testing.T) {
	cfg := &config.Config{
		WorkspaceLimitMonitors:         10,
		WorkspaceLimitMinCheckInterval: 300,
		WorkspaceLimitStatusPages:      1,
		WorkspaceLimitCustomDomains:    0,
		WorkspaceLimitMembers:          3,
		WorkspaceLimitOwnedWorkspaces:  2,
		WorkspaceLimitProbesPerMonitor: 4,
		WorkspaceLimitRetentionDays:    30,
	}
	cfg.SetFromEnvironment("WORKSPACE_LIMIT_MONITORS", "WORKSPACE_LIMIT_CUSTOM_DOMAINS")
	got, err := limits.SettingsDefaults{Settings: installsettings.Static(cfg)}.Defaults(context.Background())

	require.NoError(t, err)
	assert.Equal(t, limits.Limits{
		Monitors: ptr(10), MinCheckInterval: ptr(300), StatusPages: ptr(1), Members: ptr(3),
		OwnedWorkspaces: ptr(2), ProbesPerMonitor: ptr(4), RetentionDays: ptr(30),
	}, got)
}

func TestChain_KeepsWhatItReadForAFewSecondsAndForgetsItOnItsOwnWrite(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	chain := limits.NewChain(store, limits.FixedDefaults{}, limits.WithClock(func() time.Time { return now }))

	_, _, err := chain.For(ctx, "a")
	require.NoError(t, err)
	_, _, err = chain.For(ctx, "b")
	require.NoError(t, err)
	assert.Equal(t, 1, store.reads, "the second lookup is served from the cache")

	store.own = map[workspaceid.ID]limits.Limits{"a": {Monitors: ptr(1)}}
	_, origin, err := chain.For(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, limits.OriginNone, origin, "another process's change isn't seen within the cache's lifetime")

	now = now.Add(limits.DefaultCacheFor)
	got, origin, err := chain.For(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, limits.OriginWorkspace, origin, "and it is once the cache has expired")
	assert.Equal(t, 1, *got.Monitors)
	assert.Equal(t, 2, store.reads)

	require.NoError(t, chain.SetOwn(ctx, "a", &limits.Limits{Monitors: ptr(7)}))
	got, _, err = chain.For(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, 7, *got.Monitors, "this process's own write takes effect at once")

	require.NoError(t, chain.SetOwn(ctx, "a", nil))
	_, origin, err = chain.For(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, limits.OriginNone, origin, "removing them gives the workspace the install's defaults again")
}

// changingDefaults are install defaults that a test changes, as an instance admin saving a default
// does, and that count how often they are asked
type changingDefaults struct {
	limits limits.Limits
	asked  int
	err    error
}

func (d *changingDefaults) Defaults(context.Context) (limits.Limits, error) {
	d.asked++
	return d.limits, d.err
}

func TestChain_AsksForTheInstallsDefaultsAtEachLookup(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	defaults := &changingDefaults{limits: limits.Limits{Monitors: ptr(10)}}
	chain := limits.NewChain(store, defaults, limits.WithClock(func() time.Time { return now }))
	got, _, err := chain.For(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, 10, *got.Monitors)

	defaults.limits = limits.Limits{Monitors: ptr(3)}
	got, origin, err := chain.For(ctx, "a")

	require.NoError(t, err)
	assert.Equal(t, limits.OriginInstall, origin)
	assert.Equal(t, 3, *got.Monitors, "a changed default applies within the cache of the own limits")
	assert.Equal(t, 1, store.reads, "the own limits still come from the cache")
	assert.Equal(t, 2, defaults.asked)

	defaults.err = errors.New("settings are gone")
	_, _, err = chain.For(ctx, "a")
	assert.Error(t, err, "defaults it can't read are an error, not no limits")
}

func TestChain_AFailedReadIsAnErrorAndIsNotKept(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{err: errors.New("database is gone")}
	chain := limits.NewChain(store, limits.FixedDefaults{})

	_, _, err := chain.For(ctx, "a")
	require.Error(t, err)

	store.err = nil
	_, _, err = chain.For(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, 2, store.reads)
}

func TestSnapshot_LowestIsTheSmallestValueAnyWorkspaceHas(t *testing.T) {
	snapshot := limits.Snapshot{
		Defaults: limits.Limits{Monitors: ptr(10)},
		Own:      map[workspaceid.ID]limits.Limits{"a": {Monitors: ptr(3)}, "b": {}},
	}

	assert.Equal(t, 3, *snapshot.Lowest(limits.Monitors))
	assert.Nil(t, snapshot.Lowest(limits.Probes))
}

func TestReachedError_NamesTheLimitAndItsValue(t *testing.T) {
	err := limits.Reached(limits.Monitors, 10)

	assert.Equal(t, "LIMIT_REACHED", err.Code)
	assert.Equal(t, "the workspace has reached its limit of 10 active monitors", err.Error())
	assert.Equal(t, "a monitor of this workspace runs on at most 2 probes", limits.Reached(limits.Probes, 2).Error())
}
