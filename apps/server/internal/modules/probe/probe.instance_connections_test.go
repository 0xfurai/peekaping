package probe_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// describedConnections writes connections as "instance start-last seen", with "(bye hh:mm:ss)" for
// one that said bye, for comparing them with a literal
func describedConnections(connections []*probe.InstanceConnection) []string {
	lines := make([]string, len(connections))
	for i, c := range connections {
		lines[i] = c.InstanceID + " " + c.StartedAt.UTC().Format("15:04:05") + "-" + c.LastSeenAt.UTC().Format("15:04:05")
		if c.EndedAt != nil {
			lines[i] += " (bye " + c.EndedAt.UTC().Format("15:04:05") + ")"
		}
	}
	return lines
}

func connectionsOf(t *testing.T, repo probe.Repository, probeID string) []string {
	t.Helper()
	connections, err := repo.FindInstanceConnections(context.Background(), probeID, longAgo)
	require.NoError(t, err)
	return describedConnections(connections)
}

func hello(t *testing.T, repo probe.Repository, probeID, instanceID string, when time.Time) {
	t.Helper()
	require.NoError(t, repo.UpsertInstance(context.Background(), probeID, &probe.Instance{ID: instanceID, LastSeenAt: when}))
}

func touch(t *testing.T, repo probe.Repository, probeID, instanceID string, when time.Time) {
	t.Helper()
	found, err := repo.TouchInstance(context.Background(), probeID, instanceID, when)
	require.NoError(t, err)
	require.True(t, found)
}

func TestSQLRepository_HelloStartsAConnection(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)

		hello(t, repo, owner.ID, "instance-1", at(12, 0, 0))

		connections, err := repo.FindInstanceConnections(context.Background(), owner.ID, longAgo)
		require.NoError(t, err)
		require.Len(t, connections, 1)
		assert.Equal(t, owner.ID, connections[0].ProbeID)
		assert.Equal(t, []string{"instance-1 12:00:00-12:00:00"}, describedConnections(connections))
		assert.Nil(t, connections[0].EndedAt, "it is open")
	})
}

func TestSQLRepository_ATouchWithinTheOnlineWindowExtendsTheConnection(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		hello(t, repo, owner.ID, "instance-1", at(12, 0, 0))

		touch(t, repo, owner.ID, "instance-1", at(12, 0, 30))
		touch(t, repo, owner.ID, "instance-1", at(12, 1, 30)) // 60 seconds after the last request: still inside
		touch(t, repo, owner.ID, "instance-1", at(12, 1, 10)) // a request that was slower than the one before it changes nothing

		assert.Equal(t, []string{"instance-1 12:00:00-12:01:30"}, connectionsOf(t, repo, owner.ID))
	})
}

func TestSQLRepository_ATouchAfterMoreThanTheWindowOfQuietStartsANewConnection(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		hello(t, repo, owner.ID, "instance-1", at(12, 0, 0))
		touch(t, repo, owner.ID, "instance-1", at(12, 0, 30))

		touch(t, repo, owner.ID, "instance-1", at(12, 1, 31)) // 61 seconds after the last request
		touch(t, repo, owner.ID, "instance-1", at(12, 2, 0))

		assert.Equal(t, []string{"instance-1 12:00:00-12:00:30", "instance-1 12:01:31-12:02:00"}, connectionsOf(t, repo, owner.ID))
	})
}

func TestSQLRepository_AHelloAfterQuietStartsANewConnectionAndWithinTheWindowExtends(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		hello(t, repo, owner.ID, "instance-1", at(12, 0, 0))

		hello(t, repo, owner.ID, "instance-1", at(12, 0, 20)) // the same instance says hello again, as a probe that lost the server does
		hello(t, repo, owner.ID, "instance-1", at(12, 5, 0))

		assert.Equal(t, []string{"instance-1 12:00:00-12:00:20", "instance-1 12:05:00-12:05:00"}, connectionsOf(t, repo, owner.ID))
	})
}

func TestSQLRepository_AByeEndsTheConnectionAtTheTimeOfTheBye(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		hello(t, repo, owner.ID, "instance-1", at(12, 0, 0))
		touch(t, repo, owner.ID, "instance-1", at(12, 0, 50))
		bye := at(12, 1, 10)

		require.NoError(t, repo.RetireInstance(context.Background(), owner.ID, "instance-1", probe.RetiredAt(bye), bye))

		assert.Equal(t, []string{"instance-1 12:00:00-12:01:10 (bye 12:01:10)"}, connectionsOf(t, repo, owner.ID), "not at RetiredAt, which the instance's own last seen time is left with")
		instance, err := repo.FindInstance(context.Background(), owner.ID, "instance-1")
		require.NoError(t, err)
		assert.True(t, instance.LastSeenAt.Equal(probe.RetiredAt(bye)))

		// The restarted probe's next request starts a connection of its own
		touch(t, repo, owner.ID, "instance-1", at(12, 1, 40))
		assert.Equal(t, []string{"instance-1 12:00:00-12:01:10 (bye 12:01:10)", "instance-1 12:01:40-12:01:40"}, connectionsOf(t, repo, owner.ID))
	})
}

func TestSQLRepository_AByeFromAnInstanceThatIsNotConnectedChangesNothing(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		hello(t, repo, owner.ID, "instance-1", at(12, 0, 0))

		require.NoError(t, repo.RetireInstance(context.Background(), owner.ID, "never-there", probe.RetiredAt(at(12, 0, 10)), at(12, 0, 10)))
		require.NoError(t, repo.RetireInstance(context.Background(), other.ID, "instance-1", probe.RetiredAt(at(12, 0, 10)), at(12, 0, 10)), "another probe's token ends nothing")
		require.NoError(t, repo.RetireInstance(context.Background(), owner.ID, "instance-1", probe.RetiredAt(at(12, 9, 0)), at(12, 9, 0)), "an instance quiet for minutes has no open connection to end")

		assert.Equal(t, []string{"instance-1 12:00:00-12:00:00"}, connectionsOf(t, repo, owner.ID))
	})
}

func TestSQLRepository_EachInstanceHasItsOwnConnections(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		hello(t, repo, owner.ID, "old-container", at(12, 0, 0))
		touch(t, repo, owner.ID, "old-container", at(12, 0, 40))
		// The container restarts: a new id, which the old instance's last request is the end of
		hello(t, repo, owner.ID, "new-container", at(12, 0, 50))
		touch(t, repo, owner.ID, "new-container", at(12, 1, 20))
		hello(t, repo, other.ID, "elsewhere", at(12, 0, 10))

		assert.Equal(t, []string{"new-container 12:00:50-12:01:20", "old-container 12:00:00-12:00:40"}, connectionsOf(t, repo, owner.ID), "ordered by instance")
		assert.Equal(t, []string{"elsewhere 12:00:10-12:00:10"}, connectionsOf(t, repo, other.ID))
	})
}

func TestSQLRepository_FindInstanceConnectionsStartsWithThoseLastSeenAtOrAfterSince(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		hello(t, repo, owner.ID, "instance-1", at(8, 0, 0))
		touch(t, repo, owner.ID, "instance-1", at(8, 0, 30)) // 08:00:00-08:00:30
		touch(t, repo, owner.ID, "instance-1", at(10, 0, 0))
		touch(t, repo, owner.ID, "instance-1", at(10, 0, 30)) // 10:00:00-10:00:30
		bye := at(10, 0, 40)
		require.NoError(t, repo.RetireInstance(context.Background(), owner.ID, "instance-1", probe.RetiredAt(bye), bye))

		list := func(since time.Time) []string {
			connections, err := repo.FindInstanceConnections(context.Background(), owner.ID, since)
			require.NoError(t, err)
			return describedConnections(connections)
		}
		assert.Equal(t, []string{"instance-1 08:00:00-08:00:30", "instance-1 10:00:00-10:00:40 (bye 10:00:40)"}, list(at(8, 0, 30)), "last seen at since counts")
		assert.Equal(t, []string{"instance-1 10:00:00-10:00:40 (bye 10:00:40)"}, list(at(8, 0, 31)))
		assert.Equal(t, []string{"instance-1 10:00:00-10:00:40 (bye 10:00:40)"}, list(at(10, 0, 40)), "so does the end")
		assert.Empty(t, list(at(10, 0, 41)))
	})
}

func TestSQLRepository_AFailedHelloLeavesNoConnection(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		hello(t, repo, owner.ID, "instance-1", at(12, 0, 0))

		err := repo.UpsertInstance(context.Background(), other.ID, &probe.Instance{ID: "instance-1", LastSeenAt: at(12, 0, 30)})

		require.ErrorIs(t, err, probe.ErrInstanceTaken)
		assert.Equal(t, []string{"instance-1 12:00:00-12:00:00"}, connectionsOf(t, repo, owner.ID))
		assert.Empty(t, connectionsOf(t, repo, other.ID), "the probe that was refused has no connection")
	})
}

func TestSQLRepository_ATouchOfAnUnknownInstanceStartsNoConnection(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		hello(t, repo, owner.ID, "instance-1", at(12, 0, 0))

		found, err := repo.TouchInstance(context.Background(), owner.ID, "never-said-hello", at(12, 0, 10))
		require.NoError(t, err)
		assert.False(t, found)
		found, err = repo.TouchInstance(context.Background(), other.ID, "instance-1", at(12, 0, 10))
		require.NoError(t, err)
		assert.False(t, found, "another probe's token")

		assert.Equal(t, []string{"instance-1 12:00:00-12:00:00"}, connectionsOf(t, repo, owner.ID))
		assert.Empty(t, connectionsOf(t, repo, other.ID))
	})
}

func TestSQLRepository_ForgettingAnInstanceDeletesItsConnections(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		hello(t, repo, owner.ID, "forgotten", at(8, 0, 0))
		hello(t, repo, owner.ID, "kept", at(11, 0, 0))
		hello(t, repo, owner.ID, "replaced", at(7, 0, 0))

		removed, err := repo.DeleteInstancesUnseenSince(ctx, at(9, 0, 0))
		require.NoError(t, err)
		require.Equal(t, 2, removed)

		assert.Equal(t, []string{"kept 11:00:00-11:00:00"}, connectionsOf(t, repo, owner.ID), "the forgotten instances' connections go with them")
	})
}

func TestSQLRepository_MakingRoomForAnInstanceDeletesTheConnectionsOfTheOneItReplaces(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		hello(t, repo, owner.ID, "oldest", at(8, 0, 0))
		hello(t, repo, owner.ID, "newer", at(11, 0, 0))

		require.NoError(t, repo.MakeRoomForInstance(ctx, owner.ID, "new", 2, at(10, 0, 0)))

		assert.Equal(t, []string{"newer 11:00:00-11:00:00"}, connectionsOf(t, repo, owner.ID))
	})
}

func TestSQLRepository_DeletingAProbeDeletesItsConnections(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		hello(t, repo, owner.ID, "instance-1", at(12, 0, 0))

		require.NoError(t, repo.Delete(ctx, owner.ID))

		count, err := db.NewSelect().Table("probe_instance_connections").Count(ctx)
		require.NoError(t, err)
		assert.Zero(t, count)
	})
}

func TestService_TheHelloAndTheTouchAreOneTransactionWithTheirConnection(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
	require.NoError(t, err)

	require.NoError(t, f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: "instance-1", LastSeenAt: at(12, 0, 0)}))
	found, err := f.svc.TouchInstance(ctx, created.ID, "instance-1", at(12, 0, 45))
	require.NoError(t, err)
	require.True(t, found)

	connections, err := f.svc.FindInstanceConnections(ctx, created.ID, longAgo)
	require.NoError(t, err)
	assert.Equal(t, []string{"instance-1 12:00:00-12:00:45"}, describedConnections(connections))

	bye := at(12, 1, 0)
	require.NoError(t, f.svc.RetireInstance(ctx, created.ID, "instance-1", probe.RetiredAt(bye), bye))
	connections, err = f.svc.FindInstanceConnections(ctx, created.ID, longAgo)
	require.NoError(t, err)
	assert.Equal(t, []string{"instance-1 12:00:00-12:01:00 (bye 12:01:00)"}, describedConnections(connections))
}

func TestService_AHelloThatFailsAfterItsConnectionLeavesNoConnection(t *testing.T) {
	f := newFixtureWith(t, func(repo probe.Repository) probe.Repository {
		return failingRepo{Repository: repo, setTypesErr: assert.AnError}
	})
	ctx := context.Background()
	created, err := f.svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
	require.NoError(t, err)

	err = f.svc.RegisterInstance(ctx, created.ID, &probe.Instance{ID: "instance-1", LastSeenAt: at(12, 0, 0)})

	require.ErrorIs(t, err, assert.AnError)
	connections, err := f.svc.FindInstanceConnections(ctx, created.ID, longAgo)
	require.NoError(t, err)
	assert.Empty(t, connections, "the hello rolled back with its connection")
}
