package probe_test

import (
	"context"
	"errors"
	"testing"

	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// deleteReadFailingRepo is the SQL repository whose reads that Delete makes fail: the probe, or the
// count of its monitors (when zeroFirst, the first count finds none and only the ones after it fail)
type deleteReadFailingRepo struct {
	probe.Repository
	findErr   error
	countErr  error
	zeroFirst bool
	counts    int
}

func (r *deleteReadFailingRepo) FindByID(ctx context.Context, id string) (*probe.Model, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return r.Repository.FindByID(ctx, id)
}

func (r *deleteReadFailingRepo) CountMonitors(ctx context.Context, id string) (int, error) {
	r.counts++
	if r.zeroFirst && r.counts == 1 {
		return 0, nil
	}
	if r.countErr != nil {
		return 0, r.countErr
	}
	return r.Repository.CountMonitors(ctx, id)
}

func TestService_DeleteFailsWhenTheProbeCannotBeRead(t *testing.T) {
	boom := errors.New("boom")
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		created, err := serviceOn(db, func(repo probe.Repository) probe.Repository { return repo }).Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
		require.NoError(t, err)
		svc := serviceOn(db, func(repo probe.Repository) probe.Repository {
			return &deleteReadFailingRepo{Repository: repo, findErr: boom}
		})

		err = svc.Delete(ctx, created.ID)

		assert.ErrorIs(t, err, boom)
		assert.NotErrorIs(t, err, probe.ErrNotFound)
		stays, err := serviceOn(db, func(repo probe.Repository) probe.Repository { return repo }).FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.NotNil(t, stays, "the probe is as it was")
	})
}

func TestService_DeleteFailsWhenTheMonitorsCannotBeCounted(t *testing.T) {
	boom := errors.New("boom")
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		created, err := serviceOn(db, func(repo probe.Repository) probe.Repository { return repo }).Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
		require.NoError(t, err)
		svc := serviceOn(db, func(repo probe.Repository) probe.Repository {
			return &deleteReadFailingRepo{Repository: repo, countErr: boom}
		})

		err = svc.Delete(ctx, created.ID)

		assert.ErrorIs(t, err, boom)
		stays, err := serviceOn(db, func(repo probe.Repository) probe.Repository { return repo }).FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.NotNil(t, stays, "the probe is as it was")
	})
}

func TestService_DeleteKeepsTheDatabasesRefusalWhenTheMonitorsCannotBeCountedAgain(t *testing.T) {
	boom := errors.New("boom")
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		svc := serviceOn(db, func(repo probe.Repository) probe.Repository {
			return &deleteReadFailingRepo{Repository: repo, countErr: boom, zeroFirst: true}
		})
		created, err := svc.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
		require.NoError(t, err)
		addMonitorOn(t, db, created.ID, true)

		err = svc.Delete(ctx, created.ID)

		require.ErrorIs(t, err, probe.ErrProbeHasMonitors, "the foreign key refuses the delete")
		var hasMonitors *probe.HasMonitorsError
		assert.False(t, errors.As(err, &hasMonitors), "the count for the message failed, so the refusal is the database's own")
		assert.NotErrorIs(t, err, boom)
	})
}
