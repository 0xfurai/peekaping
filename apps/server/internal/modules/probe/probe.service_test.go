package probe_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/probe_notification"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

const (
	// bootstrapToken is 43 characters. Its SHA-256 is checked against a literal from shasum.
	bootstrapToken     = "bootstrap-token-0123456789-abcdefghijklmnop"
	bootstrapTokenHash = "sha256$c09db648bc15dcddd60d448a1656f5f1903113dd1269267909251a4ee6d090ab"
	otherToken         = "another-bootstrap-token-0123456789-abcdefgh"
)

// fixture is a probe service over a migrated SQLite database
type fixture struct {
	svc probe.Service
	db  *bun.DB
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return newFixtureWith(t, func(repo probe.Repository) probe.Repository { return repo })
}

// newFixtureWith is newFixture over the SQL repository that wrap returns, to make a step fail
func newFixtureWith(t *testing.T, wrap func(probe.Repository) probe.Repository) fixture {
	t.Helper()
	db := sqliteDB(t)
	migrateDB(t, db)
	return fixture{svc: serviceOn(db, wrap), db: db}
}

// serviceOn is a probe service over the migrated database and the SQL repository that wrap returns
func serviceOn(db *bun.DB, wrap func(probe.Repository) probe.Repository) probe.Service {
	logger := zap.NewNop().Sugar()
	channels := probe_notification.NewService(probe_notification.NewSQLRepository(db), logger)
	return probe.NewService(wrap(probe.NewSQLRepository(db)), dbtx.NewSQLTransactor(db), channels, logger)
}

// failingRepo is the SQL repository with statements that fail, to see what a failed step leaves
// behind in the steps around it
type failingRepo struct {
	probe.Repository
	deleteErr   error
	updateErr   error
	setTypesErr error
}

// countedBeforeAssigned is the SQL repository whose count of a probe's monitors is read before a
// monitor is assigned to the probe: it finds none
type countedBeforeAssigned struct {
	probe.Repository
	counted bool
}

func (r *countedBeforeAssigned) CountMonitors(ctx context.Context, id string) (int, error) {
	if !r.counted {
		r.counted = true
		return 0, nil
	}
	return r.Repository.CountMonitors(ctx, id)
}

func (r failingRepo) Delete(ctx context.Context, id string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	return r.Repository.Delete(ctx, id)
}

func (r failingRepo) Update(ctx context.Context, id string, update *probe.UpdateModel) (*probe.Model, error) {
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	return r.Repository.Update(ctx, id, update)
}

func (r failingRepo) SetSupportedTypes(ctx context.Context, probeID string, types []string) error {
	if r.setTypesErr != nil {
		return r.setTypesErr
	}
	return r.Repository.SetSupportedTypes(ctx, probeID, types)
}

// probeCount is how many probe rows have the name
func (f fixture) probeCount(t *testing.T, name string) int {
	t.Helper()
	n, err := f.db.NewSelect().Table("probes").Where("name = ?", name).Count(context.Background())
	require.NoError(t, err)
	return n
}

// channel inserts a notification channel and returns its id
func (f fixture) channel(t *testing.T) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.db.Exec("INSERT INTO notification_channels (id, name, type) VALUES (?, 'Ops', 'webhook')", id)
	require.NoError(t, err)
	return id
}

// storedHash reads the token hash stored for the probe, which is empty when it has none
func (f fixture) storedHash(t *testing.T, id string) string {
	t.Helper()
	var hash *string
	require.NoError(t, f.db.NewSelect().Table("probes").Column("token_hash").Where("id = ?", id).Scan(context.Background(), &hash))
	if hash == nil {
		return ""
	}
	return *hash
}

func (f fixture) create(t *testing.T, name string) *probe.ModelWithToken {
	t.Helper()
	created, err := f.svc.Create(context.Background(), &probe.CreateRequest{Name: name})
	require.NoError(t, err)
	return created
}

func TestService_CreateStoresHashAndPrefix(t *testing.T) {
	f := newFixture(t)

	created, err := f.svc.Create(context.Background(), &probe.CreateRequest{Name: "Frankfurt", Location: "eu-central"})

	require.NoError(t, err)
	assert.Equal(t, "Frankfurt", created.Name)
	assert.Equal(t, "eu-central", created.Location)
	assert.Regexp(t, `^pkp_[A-Za-z0-9_-]{43}$`, created.Token, "pkp_ and 32 random bytes in base64url, without padding")
	assert.Len(t, created.Token, 47)
	assert.Equal(t, created.Token[:8], created.TokenPrefix, "the prefix is the token's first 8 characters")
	hash := f.storedHash(t, created.ID)
	assert.Regexp(t, `^sha256\$[0-9a-f]{64}$`, hash)
	assert.NotContains(t, hash, created.Token, "the database holds the hash, never the token")
	assert.False(t, created.Disabled)
	assert.Equal(t, probe.StatusOffline, created.Status)
	assert.Equal(t, []string{}, created.NotificationIDs)
	assert.Equal(t, []*probe.Instance{}, created.Instances)

	second := f.create(t, "Paris")
	assert.NotEqual(t, created.Token, second.Token)
	assert.NotEqual(t, created.ID, second.ID)
}

func TestService_FindByToken(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created := f.create(t, "Frankfurt")

	found, err := f.svc.FindByToken(ctx, created.Token)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)

	for name, token := range map[string]string{
		"empty":                    "",
		"unknown":                  "pkp_" + created.Token[4:len(created.Token)-1] + "x",
		"the stored hash":          f.storedHash(t, created.ID),
		"the token with a suffix":  created.Token + "x",
		"the token's prefix alone": created.TokenPrefix,
	} {
		other, err := f.svc.FindByToken(ctx, token)
		require.NoError(t, err, name)
		assert.Nil(t, other, name)
	}

	disabled := true
	_, err = f.svc.Update(ctx, created.ID, &probe.UpdateRequest{Disabled: &disabled})
	require.NoError(t, err)
	none, err := f.svc.FindByToken(ctx, created.Token)
	require.NoError(t, err)
	assert.Nil(t, none, "a disabled probe never authenticates")

	enabled := false
	_, err = f.svc.Update(ctx, created.ID, &probe.UpdateRequest{Disabled: &enabled})
	require.NoError(t, err)
	again, err := f.svc.FindByToken(ctx, created.Token)
	require.NoError(t, err)
	require.NotNil(t, again)
	assert.Equal(t, created.ID, again.ID)
}

func TestService_RotateToken(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created := f.create(t, "Frankfurt")
	oldHash := f.storedHash(t, created.ID)

	rotated, err := f.svc.RotateToken(ctx, created.ID)

	require.NoError(t, err)
	assert.Equal(t, created.ID, rotated.ID)
	assert.Regexp(t, `^pkp_[A-Za-z0-9_-]{43}$`, rotated.Token)
	assert.NotEqual(t, created.Token, rotated.Token)
	assert.Equal(t, rotated.Token[:8], rotated.TokenPrefix)
	assert.NotEqual(t, oldHash, f.storedHash(t, created.ID))
	old, err := f.svc.FindByToken(ctx, created.Token)
	require.NoError(t, err)
	assert.Nil(t, old, "the old token no longer finds the probe")
	found, err := f.svc.FindByToken(ctx, rotated.Token)
	require.NoError(t, err)
	require.NotNil(t, found, "the new token finds it at once")
	assert.Equal(t, created.ID, found.ID)

	_, err = f.svc.RotateToken(ctx, uuid.NewString())
	assert.ErrorIs(t, err, probe.ErrNotFound)
}

func TestService_BootstrapSetsLocalToken(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	require.NoError(t, f.svc.Bootstrap(ctx, bootstrapToken))

	assert.Equal(t, bootstrapTokenHash, f.storedHash(t, probe.LocalID))
	local, err := f.svc.FindByToken(ctx, bootstrapToken)
	require.NoError(t, err)
	require.NotNil(t, local)
	assert.Equal(t, "local", local.ID)
	assert.Equal(t, "Local", local.Name)
	assert.Equal(t, "bootstra", local.TokenPrefix)

	require.NoError(t, f.svc.Bootstrap(ctx, bootstrapToken), "a restart with the same token changes nothing")
	assert.Equal(t, bootstrapTokenHash, f.storedHash(t, probe.LocalID))

	require.NoError(t, f.svc.Bootstrap(ctx, otherToken), "a new token replaces the old one")
	old, err := f.svc.FindByToken(ctx, bootstrapToken)
	require.NoError(t, err)
	assert.Nil(t, old)
	found, err := f.svc.FindByToken(ctx, otherToken)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "local", found.ID)
}

func TestService_BootstrapWithoutTokenLeavesLocal(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	require.NoError(t, f.svc.Bootstrap(ctx, ""))
	assert.Equal(t, "", f.storedHash(t, probe.LocalID), "local has no token until a bootstrap token sets one")

	require.NoError(t, f.svc.Bootstrap(ctx, bootstrapToken))
	require.NoError(t, f.svc.Bootstrap(ctx, ""))
	found, err := f.svc.FindByToken(ctx, bootstrapToken)
	require.NoError(t, err)
	require.NotNil(t, found, "a restart without the token keeps the one that was set")
}

func TestService_BootstrapRefusesATokenUnder32Characters(t *testing.T) {
	f := newFixture(t)

	err := f.svc.Bootstrap(context.Background(), "too-short-to-be-a-token")

	assert.ErrorContains(t, err, "at least 32 characters")
	assert.Equal(t, "", f.storedHash(t, probe.LocalID))
}

func TestService_BootstrapCreatesLocalWhenTheSeedIsMissing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	_, err := f.db.Exec("DELETE FROM probes WHERE id = 'local'")
	require.NoError(t, err)

	require.NoError(t, f.svc.Bootstrap(ctx, bootstrapToken))

	found, err := f.svc.FindByToken(ctx, bootstrapToken)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "local", found.ID)
	assert.Equal(t, "Local", found.Name)
}

func TestService_LocalProbeCanBeRenamedButNotDeletedRotatedOrDisabled(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	require.NoError(t, f.svc.Bootstrap(ctx, bootstrapToken))
	slack := f.channel(t)

	name, location := "Headquarters", "rack 4"
	updated, err := f.svc.Update(ctx, probe.LocalID, &probe.UpdateRequest{Name: &name, Location: &location, NotificationIDs: []string{slack}})
	require.NoError(t, err)
	assert.Equal(t, "Headquarters", updated.Name)
	assert.Equal(t, "rack 4", updated.Location)
	assert.Equal(t, []string{slack}, updated.NotificationIDs)

	disabled := true
	_, err = f.svc.Update(ctx, probe.LocalID, &probe.UpdateRequest{Disabled: &disabled})
	assert.ErrorIs(t, err, probe.ErrLocalProbe)
	enabled := false
	_, err = f.svc.Update(ctx, probe.LocalID, &probe.UpdateRequest{Disabled: &enabled})
	assert.NoError(t, err, "local is never disabled, so enabling it is no change")
	_, err = f.svc.RotateToken(ctx, probe.LocalID)
	assert.ErrorIs(t, err, probe.ErrLocalProbe)
	assert.ErrorIs(t, f.svc.Delete(ctx, probe.LocalID), probe.ErrLocalProbe)

	found, err := f.svc.FindByToken(ctx, bootstrapToken)
	require.NoError(t, err)
	require.NotNil(t, found, "none of that touched local or its token")
	assert.False(t, found.Disabled)
	assert.Equal(t, "Headquarters", found.Name)
}

func TestService_UpdateChangesOnlyWhatItIsGiven(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	slack, email := f.channel(t), f.channel(t)
	created, err := f.svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt", Location: "eu-central", NotificationIDs: []string{slack}})
	require.NoError(t, err)
	assert.Equal(t, []string{slack}, created.NotificationIDs)

	name := "Paris"
	renamed, err := f.svc.Update(ctx, created.ID, &probe.UpdateRequest{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, "Paris", renamed.Name)
	assert.Equal(t, "eu-central", renamed.Location)
	assert.Equal(t, []string{slack}, renamed.NotificationIDs, "channels not named stay")

	replaced, err := f.svc.Update(ctx, created.ID, &probe.UpdateRequest{NotificationIDs: []string{email}})
	require.NoError(t, err)
	assert.Equal(t, []string{email}, replaced.NotificationIDs)

	cleared, err := f.svc.Update(ctx, created.ID, &probe.UpdateRequest{NotificationIDs: []string{}})
	require.NoError(t, err)
	assert.Equal(t, []string{}, cleared.NotificationIDs)

	_, err = f.svc.Update(ctx, uuid.NewString(), &probe.UpdateRequest{Name: &name})
	assert.ErrorIs(t, err, probe.ErrNotFound)
}

func TestService_CreateWithUnknownChannelLeavesNothing(t *testing.T) {
	ctx := context.Background()
	// A delete that fails: a create that undoes itself with a second statement would leave the
	// probe behind. It must be one transaction that never held the probe.
	f := newFixtureWith(t, func(repo probe.Repository) probe.Repository {
		return failingRepo{Repository: repo, deleteErr: errors.New("the probe can't be deleted")}
	})
	slack := f.channel(t)

	_, err := f.svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt", NotificationIDs: []string{slack, uuid.NewString()}})

	require.ErrorIs(t, err, probe_notification.ErrUnknownChannel)
	assert.Equal(t, 0, f.probeCount(t, "Frankfurt"), "no probe is left behind by a create that failed")
	all, err := f.svc.FindAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "local", all[0].ID)
}

func TestService_UpdateWithUnknownChannelChangesNothing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	slack := f.channel(t)
	created, err := f.svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt", Location: "eu-central", NotificationIDs: []string{slack}})
	require.NoError(t, err)

	name, location := "Renamed", "us-east"
	_, err = f.svc.Update(ctx, created.ID, &probe.UpdateRequest{Name: &name, Location: &location, NotificationIDs: []string{uuid.NewString()}})

	require.ErrorIs(t, err, probe_notification.ErrUnknownChannel)
	unchanged, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Frankfurt", unchanged.Name, "the name stays")
	assert.Equal(t, "eu-central", unchanged.Location)
	assert.Equal(t, []string{slack}, unchanged.NotificationIDs, "and so do the channels")
}

func TestService_UpdateKeepsTheChannelsWhenTheProbeUpdateFails(t *testing.T) {
	ctx := context.Background()
	f := newFixtureWith(t, func(repo probe.Repository) probe.Repository {
		return failingRepo{Repository: repo, updateErr: errors.New("the probe can't be updated")}
	})
	slack, email := f.channel(t), f.channel(t)
	created, err := f.svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt", NotificationIDs: []string{slack}})
	require.NoError(t, err)

	name := "Renamed"
	_, err = f.svc.Update(ctx, created.ID, &probe.UpdateRequest{Name: &name, NotificationIDs: []string{email}})

	require.Error(t, err)
	unchanged, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Frankfurt", unchanged.Name)
	assert.Equal(t, []string{slack}, unchanged.NotificationIDs, "a channel change is kept only with the update that came with it")
}

func TestService_FindAllListsLocalFirstWithInstancesAndChannels(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	slack := f.channel(t)
	created, err := f.svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt", NotificationIDs: []string{slack}})
	require.NoError(t, err)
	seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
	_, err = f.db.Exec(`INSERT INTO probe_instances (id, probe_id, hostname, os, arch, version, protocol_version, last_seen_at) VALUES (?, ?, 'host-1', 'linux', 'arm64', '1.2.3', 1, ?)`, "instance-1", created.ID, seen)
	require.NoError(t, err)

	all, err := f.svc.FindAll(ctx)

	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "local", all[0].ID)
	assert.Equal(t, []*probe.Instance{}, all[0].Instances)
	assert.Equal(t, []string{}, all[0].NotificationIDs)
	assert.Equal(t, created.ID, all[1].ID)
	assert.Equal(t, []string{slack}, all[1].NotificationIDs)
	require.Len(t, all[1].Instances, 1)
	assert.Equal(t, "instance-1", all[1].Instances[0].ID)
	assert.Equal(t, "host-1", all[1].Instances[0].Hostname)
	assert.Equal(t, 1, all[1].Instances[0].ProtocolVersion)
	assert.WithinDuration(t, seen, all[1].Instances[0].LastSeenAt, time.Second)
	assert.Equal(t, probe.StatusOffline, all[1].Status, "nothing speaks the probe protocol yet, so a probe is offline")
}

func TestService_DeleteRemovesTheProbe(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created := f.create(t, "Frankfurt")

	require.NoError(t, f.svc.Delete(ctx, created.ID))

	gone, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, gone)
	assert.ErrorIs(t, f.svc.Delete(ctx, created.ID), probe.ErrNotFound)
	none, err := f.svc.FindByToken(ctx, created.Token)
	require.NoError(t, err)
	assert.Nil(t, none)
}

func TestService_DeleteRefusesAProbeWithMonitors(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created := f.create(t, "Frankfurt")
	for _, active := range []bool{true, false, true} {
		id := uuid.NewString()
		_, err := f.db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, active) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0, ?)`, id, active)
		require.NoError(t, err)
		_, err = f.db.Exec("INSERT INTO monitor_probes (monitor_id, probe_id) VALUES (?, ?)", id, created.ID)
		require.NoError(t, err)
	}

	err := f.svc.Delete(ctx, created.ID)

	require.ErrorIs(t, err, probe.ErrProbeHasMonitors)
	var hasMonitors *probe.HasMonitorsError
	require.ErrorAs(t, err, &hasMonitors)
	assert.Equal(t, 3, hasMonitors.Monitors)
	stays, err := f.svc.FindByToken(ctx, created.Token)
	require.NoError(t, err)
	require.NotNil(t, stays, "the probe and its token are as they were")
	assert.Equal(t, 1, f.probeCount(t, "Frankfurt"))

	_, err = f.db.Exec("DELETE FROM monitor_probes WHERE probe_id = ?", created.ID)
	require.NoError(t, err)
	assert.NoError(t, f.svc.Delete(ctx, created.ID), "it can be deleted once no monitor runs on it")
}

func TestService_DeleteRefusesAProbeThatAMonitorWasAssignedToAfterTheCount(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		svc := serviceOn(db, func(repo probe.Repository) probe.Repository { return &countedBeforeAssigned{Repository: repo} })
		created, err := svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
		require.NoError(t, err)
		addMonitorOn(t, db, created.ID, true)
		addMonitorOn(t, db, created.ID, false)

		err = svc.Delete(ctx, created.ID)

		require.ErrorIs(t, err, probe.ErrProbeHasMonitors, "the foreign key refuses the delete, which is not a failure of the server")
		var hasMonitors *probe.HasMonitorsError
		require.ErrorAs(t, err, &hasMonitors)
		assert.Equal(t, 2, hasMonitors.Monitors, "the count is read again for the message")
		stays, err := svc.FindByToken(ctx, created.Token)
		require.NoError(t, err)
		assert.NotNil(t, stays, "the probe is as it was")
	})
}

func TestService_RegisterInstanceRecordsTheInstanceAndTheProbesTypes(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created := f.create(t, "Frankfurt")
	seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)

	err := f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{
		ID: "instance-1", Hostname: "host-1", OS: "linux", Arch: "arm64", Version: "1.2.3", ProtocolVersion: 1, LastSeenAt: seen, SupportedTypes: []string{"http", "tcp"}})

	require.NoError(t, err)
	got, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"http", "tcp"}, got.SupportedTypes)
	require.Len(t, got.Instances, 1)
	assert.Equal(t, "instance-1", got.Instances[0].ID)
	assert.Equal(t, "host-1", got.Instances[0].Hostname)
	assert.Equal(t, "linux", got.Instances[0].OS)
	assert.Equal(t, "arm64", got.Instances[0].Arch)
	assert.Equal(t, "1.2.3", got.Instances[0].Version)
	assert.Equal(t, 1, got.Instances[0].ProtocolVersion)
	assert.WithinDuration(t, seen, got.Instances[0].LastSeenAt, time.Second)
}

func TestService_RegisterInstanceAgainUpdatesTheSameInstance(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created := f.create(t, "Frankfurt")
	seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
	require.NoError(t, f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: "instance-1", Hostname: "host-1", Version: "1.0.0", LastSeenAt: seen, SupportedTypes: []string{"http", "tcp"}}))

	require.NoError(t, f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: "instance-1", Hostname: "host-2", Version: "1.1.0", LastSeenAt: seen.Add(time.Hour), SupportedTypes: []string{"http"}}))

	got, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, got.Instances, 1, "a probe that says hello again is the same instance")
	assert.Equal(t, "host-2", got.Instances[0].Hostname)
	assert.Equal(t, "1.1.0", got.Instances[0].Version)
	assert.WithinDuration(t, seen.Add(time.Hour), got.Instances[0].LastSeenAt, time.Second)
	assert.Equal(t, []string{"http"}, got.SupportedTypes, "the latest hello's types replace the earlier ones")
}

func TestService_RegisterInstanceKeepsSeveralInstancesOfOneProbe(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created := f.create(t, "Frankfurt")
	seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)

	require.NoError(t, f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: "instance-1", LastSeenAt: seen}))
	require.NoError(t, f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: "instance-2", LastSeenAt: seen.Add(time.Minute)}))

	got, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, got.Instances, 2)
	assert.Equal(t, "instance-2", got.Instances[0].ID, "the most recently seen first")
	assert.Equal(t, []string{}, got.SupportedTypes)
}

func TestService_RegisterInstanceRefusesAnotherProbesInstanceAndChangesNothing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	owner, intruder := f.create(t, "Owner"), f.create(t, "Intruder")
	seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
	require.NoError(t, f.svc.RegisterInstance(ctx, owner.ID, &probe.Instance{ID: "instance-1", Hostname: "owner-host", LastSeenAt: seen, SupportedTypes: []string{"http"}}))

	err := f.svc.RegisterInstance(ctx, intruder.ID, &probe.Instance{ID: "instance-1", Hostname: "intruder-host", LastSeenAt: seen, SupportedTypes: []string{"dns"}})

	assert.ErrorIs(t, err, probe.ErrInstanceTaken)
	got, err := f.svc.FindByID(ctx, intruder.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Instances)
	assert.Equal(t, []string{}, got.SupportedTypes, "a refused hello leaves the probe's types alone")
	kept, err := f.svc.FindByID(ctx, owner.ID)
	require.NoError(t, err)
	require.Len(t, kept.Instances, 1)
	assert.Equal(t, "owner-host", kept.Instances[0].Hostname)
}

func TestService_RegisterInstanceIsOneTransaction(t *testing.T) {
	ctx := context.Background()
	f := newFixtureWith(t, func(repo probe.Repository) probe.Repository {
		return failingRepo{Repository: repo, setTypesErr: errors.New("the types can't be stored")}
	})
	created := f.create(t, "Frankfurt")

	err := f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: "instance-1", Hostname: "host-1", LastSeenAt: time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC), SupportedTypes: []string{"http"}})

	require.Error(t, err)
	got, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Instances, "a hello whose types failed to store leaves no instance row behind")
}

// heldBy is how many instance rows the probe has
func heldBy(t *testing.T, db *bun.DB, probeID string) int {
	t.Helper()
	n, err := db.NewSelect().Table("probe_instances").Where("probe_id = ?", probeID).Count(context.Background())
	require.NoError(t, err)
	return n
}

// A probe restarts every minute for a day, each start with an instance id of its own: half of
// the starts say bye first and half just stop. Its rows must not pass the cap, which the sweep
// would not keep them under: it prunes only what has been silent for a day.
func TestService_RegisterInstanceKeepsAProbeThatRestartsEveryMinuteForADayAtTheCap(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		svc := serviceOn(db, func(repo probe.Repository) probe.Repository { return repo })
		created, err := svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
		require.NoError(t, err)
		start := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)

		const restarts = 24 * 60
		for minute := range restarts {
			now := start.Add(time.Duration(minute) * time.Minute)
			id := fmt.Sprintf("start-%04d", minute)
			require.NoError(t, svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: id, LastSeenAt: now, SupportedTypes: []string{"http"}}), "restart %d", minute)
			if minute%2 == 0 {
				require.NoError(t, svc.RetireInstance(ctx, created.ID, id, probe.RetiredAt(now.Add(30*time.Second)), now.Add(30*time.Second)))
			}

			require.LessOrEqual(t, heldBy(t, db, created.ID), 64, "after restart %d", minute)
		}

		got, err := svc.FindByID(ctx, created.ID)
		require.NoError(t, err)
		require.Len(t, got.Instances, 64, "the table is flat at the cap")
		assert.Equal(t, "start-1439", got.Instances[0].ID, "the newest start is the most recently seen")
		assert.Equal(t, "start-1376", got.Instances[63].ID, "and the 64 left are the 64 latest starts: the oldest made the room")
	})
}

// Of several hellos at once from new instances, as many as there are places get one. On PostgreSQL
// each of them would count 63 rows and take the last place, were it not for the lock.
func TestService_RegisterInstanceAtOnceFromManyNewInstancesFillsOnlyTheFreePlaces(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		svc := serviceOn(db, func(repo probe.Repository) probe.Repository { return repo })
		created, err := svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
		require.NoError(t, err)
		now := time.Now().UTC()
		for n := range 61 {
			require.NoError(t, svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: fmt.Sprintf("online-%d", n), LastSeenAt: now}))
		}

		const hellos = 12
		errs := make([]error, hellos)
		var wg sync.WaitGroup
		for n := range hellos {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[n] = svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: fmt.Sprintf("new-%d", n), LastSeenAt: now})
			}()
		}
		wg.Wait()

		accepted, refused := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				accepted++
			case errors.Is(err, probe.ErrTooManyInstances):
				refused++
			default:
				require.NoError(t, err)
			}
		}
		assert.Equal(t, 3, accepted, "61 held and a cap of 64 leave 3 places")
		assert.Equal(t, hellos-3, refused)
		assert.Equal(t, 64, heldBy(t, db, created.ID))
	})
}

func TestService_TouchInstanceStampsTheLastSeenTime(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created, other := f.create(t, "Frankfurt"), f.create(t, "Paris")
	seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
	require.NoError(t, f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: "instance-1", LastSeenAt: seen}))

	found, err := f.svc.TouchInstance(ctx, created.ID, "instance-1", seen.Add(time.Minute))
	require.NoError(t, err)
	assert.True(t, found)
	got, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.WithinDuration(t, seen.Add(time.Minute), got.Instances[0].LastSeenAt, time.Second)

	found, err = f.svc.TouchInstance(ctx, created.ID, "instance-2", seen.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, found, "an instance that never said hello is not found")
	found, err = f.svc.TouchInstance(ctx, other.ID, "instance-1", seen.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, found, "nor is another probe's")
}

func TestService_RetireInstanceLeavesTheInstanceLastSeenAtTheGivenTime(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created := f.create(t, "Frankfurt")
	seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
	retired := seen.Add(-time.Minute)
	require.NoError(t, f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: "instance-1", LastSeenAt: seen, SupportedTypes: []string{"http"}}))
	require.NoError(t, f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: "instance-2", LastSeenAt: seen, SupportedTypes: []string{"http"}}))

	require.NoError(t, f.svc.RetireInstance(ctx, created.ID, "instance-1", retired, retired))

	got, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, got.Instances, 2, "the instance stays: the sweep prunes it once it has been silent for a day")
	assert.Equal(t, "instance-2", got.Instances[0].ID)
	assert.Equal(t, "instance-1", got.Instances[1].ID)
	assert.WithinDuration(t, retired, got.Instances[1].LastSeenAt, time.Second)
	assert.WithinDuration(t, seen, got.Instances[0].LastSeenAt, time.Second)
}

func TestService_MarkOnlineAndOfflineChangeTheStatusOnce(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created := f.create(t, "Frankfurt")

	changed, err := f.svc.MarkOffline(ctx, created.ID, time.Now())
	require.NoError(t, err)
	assert.False(t, changed, "a new probe is offline already")

	changed, err = f.svc.MarkOnline(ctx, created.ID, time.Now())
	require.NoError(t, err)
	assert.True(t, changed)
	got, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, probe.StatusOnline, got.Status)

	changed, err = f.svc.MarkOnline(ctx, created.ID, time.Now())
	require.NoError(t, err)
	assert.False(t, changed, "only the caller that changes the status hears true, so that two replicas don't both announce it")

	changed, err = f.svc.MarkOffline(ctx, created.ID, time.Now())
	require.NoError(t, err)
	assert.True(t, changed)
	got, err = f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, probe.StatusOffline, got.Status)

	changed, err = f.svc.MarkOnline(ctx, "no-such-probe", time.Now())
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestService_MarkOnlineLeavesADisabledProbeOffline(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	created := f.create(t, "Frankfurt")
	disabled := true
	_, err := f.svc.Update(ctx, created.ID, &probe.UpdateRequest{Disabled: &disabled})
	require.NoError(t, err)

	changed, err := f.svc.MarkOnline(ctx, created.ID, time.Now())

	require.NoError(t, err)
	assert.False(t, changed, "a disabled probe's token is refused, so nothing has heard from it")
	got, err := f.svc.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, probe.StatusOffline, got.Status)

	enabled := false
	_, err = f.svc.Update(ctx, created.ID, &probe.UpdateRequest{Disabled: &enabled})
	require.NoError(t, err)
	changed, err = f.svc.MarkOnline(ctx, created.ID, time.Now())
	require.NoError(t, err)
	assert.True(t, changed, "enabled again, the probe comes online")
}
