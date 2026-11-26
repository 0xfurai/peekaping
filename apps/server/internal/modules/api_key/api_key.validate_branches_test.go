package api_key

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// flakyRepo is a repository whose key lookup and use count fail when the test says so
type flakyRepo struct {
	Repository
	findErr      error
	consumeErr   error
	consumeCalls int
}

func (r *flakyRepo) FindByIDAnyWorkspace(ctx context.Context, id string) (*Model, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return r.Repository.FindByIDAnyWorkspace(ctx, id)
}

func (r *flakyRepo) ConsumeUsage(ctx context.Context, id string) (bool, error) {
	r.consumeCalls++
	if r.consumeErr != nil {
		return false, r.consumeErr
	}
	return r.Repository.ConsumeUsage(ctx, id)
}

func newFlakyService(t *testing.T) (Service, *flakyRepo, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	repo := &flakyRepo{Repository: NewSQLRepository(setupServiceTestDB(t))}
	return NewService(repo, zap.New(core).Sugar()), repo, logs
}

func TestValidateKey_ARepositoryThatCannotReadTheKeyFailsWithItsError(t *testing.T) {
	service, repo, logs := newFlakyService(t)
	created, err := service.Create(adminContext(), &CreateRequest{Name: "Key"})
	require.NoError(t, err)
	repo.findErr = errors.New("db down")

	key, err := service.ValidateKey(context.Background(), created.Token)

	assert.Nil(t, key)
	assert.Same(t, repo.findErr, err, "the error as the repository gave it")
	entries := logs.FilterMessage("Error finding API key by ID").All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	assert.Zero(t, repo.consumeCalls)
}

func TestValidateKey_ACountThatFailsOnlyFailsAKeyWithALimit(t *testing.T) {
	t.Run("a key without a limit is accepted", func(t *testing.T) {
		service, repo, logs := newFlakyService(t)
		created, err := service.Create(adminContext(), &CreateRequest{Name: "Key"})
		require.NoError(t, err)
		repo.consumeErr = errors.New("db down")

		key, err := service.ValidateKey(context.Background(), created.Token)

		require.NoError(t, err)
		assert.Equal(t, created.ID, key.ID)
		assert.Equal(t, 1, logs.FilterMessage("Error updating last used timestamp and usage count").Len())
		assert.Equal(t, 1, logs.FilterMessage("API key validation successful").Len())
	})
	t.Run("a key with a limit is refused with the error", func(t *testing.T) {
		service, repo, logs := newFlakyService(t)
		limit := int64(5)
		created, err := service.Create(adminContext(), &CreateRequest{Name: "Key", MaxUsageCount: &limit})
		require.NoError(t, err)
		repo.consumeErr = errors.New("db down")

		key, err := service.ValidateKey(context.Background(), created.Token)

		assert.Nil(t, key)
		assert.Same(t, repo.consumeErr, err)
		assert.Equal(t, 1, logs.FilterMessage("Error updating last used timestamp and usage count").Len())
		assert.Zero(t, logs.FilterMessage("API key validation successful").Len())
	})
}

// The checks run in a fixed order, so that a request learns of the first thing wrong with its key:
// the format, whether the key exists, whether it expired, whether the secret is right, whether the
// request names the key's workspace, and last the usage limit.
func TestValidateKey_RefusesForTheFirstOfItsReasons(t *testing.T) {
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	one := int64(1)
	named := WithRequestedWorkspace(context.Background(), "5b7c1d44-0a43-4e2c-9a8e-6a1c3d2f9b02")

	tests := []struct {
		name    string
		create  CreateRequest
		secret  string // replaces the secret in the token when set
		useOnce bool   // the key's one use is spent before the request
		ctx     context.Context
		wantErr string
		wantIs  error
	}{
		{name: "an expired key with a wrong secret is expired", create: CreateRequest{Name: "k", ExpiresAt: &past}, secret: "wrong", ctx: context.Background(), wantErr: "API key has expired"},
		{name: "an expired key for another workspace is expired", create: CreateRequest{Name: "k", ExpiresAt: &past}, ctx: named, wantErr: "API key has expired"},
		{name: "a wrong secret for another workspace is invalid", create: CreateRequest{Name: "k"}, secret: "wrong", ctx: named, wantErr: "invalid API key"},
		{name: "a wrong secret on a spent key is invalid", create: CreateRequest{Name: "k", MaxUsageCount: &one}, secret: "wrong", useOnce: true, ctx: context.Background(), wantErr: "invalid API key"},
		{name: "another workspace beats a spent key", create: CreateRequest{Name: "k", MaxUsageCount: &one}, useOnce: true, ctx: named, wantErr: ErrWorkspaceMismatch.Error(), wantIs: ErrWorkspaceMismatch},
		{name: "a spent key in its own workspace is over its limit", create: CreateRequest{Name: "k", MaxUsageCount: &one}, useOnce: true, ctx: context.Background(), wantErr: "API key usage limit exceeded"},
		{name: "a key that expires later is accepted", create: CreateRequest{Name: "k", ExpiresAt: &future}, ctx: context.Background()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(t)
			created, err := service.Create(adminContext(), &tt.create)
			require.NoError(t, err)
			token := created.Token
			if tt.secret != "" {
				token = tokenWith(t, decodeToken(t, created.Token)["id"], tt.secret)
			}
			if tt.useOnce {
				_, err = service.ValidateKey(context.Background(), created.Token)
				require.NoError(t, err)
			}

			key, err := service.ValidateKey(tt.ctx, token)

			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, created.ID, key.ID)
				return
			}
			assert.Nil(t, key)
			assert.EqualError(t, err, tt.wantErr)
			if tt.wantIs != nil {
				assert.ErrorIs(t, err, tt.wantIs)
			}
		})
	}
}

func TestValidateKey_ARequestForAnotherWorkspaceIsNotCountedAsAUse(t *testing.T) {
	service := newTestService(t)
	limit := int64(1)
	created, err := service.Create(adminContext(), &CreateRequest{Name: "Key", MaxUsageCount: &limit})
	require.NoError(t, err)

	_, err = service.ValidateKey(WithRequestedWorkspace(context.Background(), "5b7c1d44-0a43-4e2c-9a8e-6a1c3d2f9b02"), created.Token)
	assert.ErrorIs(t, err, ErrWorkspaceMismatch)

	stored, err := service.FindByID(adminContext(), testWorkspace, created.ID)
	require.NoError(t, err)
	assert.Zero(t, stored.UsageCount, "the refused request used nothing up")
	assert.Nil(t, stored.LastUsed)

	// The header of the key's own workspace passes, in any case, and spends the one use
	_, err = service.ValidateKey(WithRequestedWorkspace(context.Background(), strings.ToUpper(string(testWorkspace))), created.Token)
	assert.NoError(t, err)
	_, err = service.ValidateKey(WithRequestedWorkspace(context.Background(), string(testWorkspace)), created.Token)
	assert.EqualError(t, err, "API key usage limit exceeded")
}
