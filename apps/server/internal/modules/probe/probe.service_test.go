package probe_test

import (
	"context"
	"errors"
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
	logger := zap.NewNop().Sugar()
	channels := probe_notification.NewService(probe_notification.NewSQLRepository(db), logger)
	return fixture{svc: probe.NewService(wrap(probe.NewSQLRepository(db)), dbtx.NewSQLTransactor(db), channels, logger), db: db}
}

// failingRepo is the SQL repository with statements that fail, to see what a failed step leaves
// behind in the steps around it
type failingRepo struct {
	probe.Repository
	deleteErr error
	updateErr error
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
