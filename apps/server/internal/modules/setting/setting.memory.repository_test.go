package setting_test

import (
	"context"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/workspace"
)

// memoryRepository is an in-memory setting.Repository.
type memoryRepository struct {
	settings map[string]setting.Model
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{settings: map[string]setting.Model{}}
}

func (r *memoryRepository) GetByKey(_ context.Context, key string) (*setting.Model, error) {
	stored, ok := r.settings[key]
	if !ok {
		return nil, nil
	}
	return &stored, nil
}

func (r *memoryRepository) SetByKey(_ context.Context, key string, entity *setting.Model) (*setting.Model, error) {
	stored := setting.Model{Key: key, Value: entity.Value, Type: entity.Type}
	r.settings[key] = stored
	return &stored, nil
}

func (r *memoryRepository) CreateIfAbsent(_ context.Context, key string, entity *setting.Model) (*setting.Model, error) {
	if stored, ok := r.settings[key]; ok {
		return &stored, nil
	}
	stored := setting.Model{Key: key, Value: entity.Value, Type: entity.Type}
	r.settings[key] = stored
	return &stored, nil
}

func (r *memoryRepository) DeleteByKey(_ context.Context, key string) error {
	delete(r.settings, key)
	return nil
}

// memoryWorkspaceRepository is an in-memory setting.WorkspaceRepository.
type memoryWorkspaceRepository struct {
	settings map[workspace.ID]map[string]setting.Model
}

func newMemoryWorkspaceRepository() *memoryWorkspaceRepository {
	return &memoryWorkspaceRepository{settings: map[workspace.ID]map[string]setting.Model{}}
}

func (r *memoryWorkspaceRepository) GetByKey(_ context.Context, id workspace.ID, key string) (*setting.Model, error) {
	stored, ok := r.settings[id][key]
	if !ok {
		return nil, nil
	}
	return &stored, nil
}

func (r *memoryWorkspaceRepository) SetByKey(_ context.Context, id workspace.ID, key string, entity *setting.Model) (*setting.Model, error) {
	if r.settings[id] == nil {
		r.settings[id] = map[string]setting.Model{}
	}
	stored := setting.Model{Key: key, Value: entity.Value, Type: entity.Type}
	r.settings[id][key] = stored
	return &stored, nil
}

func (r *memoryWorkspaceRepository) DeleteByKey(_ context.Context, id workspace.ID, key string) error {
	delete(r.settings[id], key)
	return nil
}
