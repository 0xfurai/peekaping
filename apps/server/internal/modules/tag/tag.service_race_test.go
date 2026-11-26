package tag_test

import (
	"context"
	"testing"

	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// racingRepository stores a tag of the same name just after the service has looked for one, as a
// second request does when two creates run at once
type racingRepository struct {
	tag.Repository
	rival func(ctx context.Context, workspaceID workspace.ID, name string)
}

func (r *racingRepository) FindByName(ctx context.Context, workspaceID workspace.ID, name string) (*tag.Model, error) {
	found, err := r.Repository.FindByName(ctx, workspaceID, name)
	if found == nil && err == nil && r.rival != nil {
		r.rival(ctx, workspaceID, name)
		r.rival = nil
	}
	return found, err
}

func TestTagService_ATagCreatedWithTheSameNameAfterTheCheckIsRefusedAsATakenName(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	repository := tag.NewSQLRepository(h.db)
	racing := &racingRepository{Repository: repository, rival: func(ctx context.Context, workspaceID workspace.ID, name string) {
		_, err := repository.Create(ctx, workspaceID, &tag.Model{Name: name, Color: "#000000"})
		require.NoError(t, err)
	}}
	svc := tag.NewService(racing, h.monitorTags, zap.NewNop().Sugar())

	_, err := svc.Create(ctx, h.ws, &tag.CreateUpdateDto{Name: "production", Color: "#EF4444"})

	assert.ErrorIs(t, err, tag.ErrNameTaken, "as the check itself answers")
	all, err := h.svc.FindAll(ctx, h.ws, 0, 10, "")
	require.NoError(t, err)
	assert.Len(t, all, 1, "the rival's tag is the one there is")
}

func TestTagService_ARenameToANameTakenAfterTheCheckIsRefusedAsATakenName(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	staging := h.create(t, "staging")
	repository := tag.NewSQLRepository(h.db)
	rival := func(ctx context.Context, workspaceID workspace.ID, name string) {
		_, err := repository.Create(ctx, workspaceID, &tag.Model{Name: name, Color: "#000000"})
		require.NoError(t, err)
	}
	svc := tag.NewService(&racingRepository{Repository: repository, rival: rival}, h.monitorTags, zap.NewNop().Sugar())
	_, err := svc.UpdateFull(ctx, h.ws, staging.ID, &tag.CreateUpdateDto{Name: "production", Color: "#EF4444"})
	assert.ErrorIs(t, err, tag.ErrNameTaken)

	svc = tag.NewService(&racingRepository{Repository: repository, rival: func(ctx context.Context, workspaceID workspace.ID, name string) {
		_, err := repository.Create(ctx, workspaceID, &tag.Model{Name: name, Color: "#000000"})
		require.NoError(t, err)
	}}, h.monitorTags, zap.NewNop().Sugar())
	_, err = svc.UpdatePartial(ctx, h.ws, staging.ID, &tag.PartialUpdateDto{Name: ptr("qa")})
	assert.ErrorIs(t, err, tag.ErrNameTaken)
	assert.Equal(t, "staging", h.find(t, staging.ID).Name)
}
