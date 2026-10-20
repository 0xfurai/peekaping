package notification_channel

import (
	"context"
	"peekaping/internal/modules/monitor_notification"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error)
	Delete(ctx context.Context, id string) error
}

type ServiceImpl struct {
	repository                 Repository
	monitorNotificationService monitor_notification.Service
	logger                     *zap.SugaredLogger
}

func NewService(
	repository Repository,
	monitorNotificationService monitor_notification.Service,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository,
		monitorNotificationService,
		logger.Named("[notification-service]"),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, entity *CreateUpdateDto) (*Model, error) {
	config, secret, err := WriteSecrets(entity.Config, entity.Secret, "")
	if err != nil {
		return nil, err
	}

	createModel := &Model{
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    &config,
	}
	if secret != "" {
		createModel.Secret = &secret
	}

	return mr.repository.Create(ctx, createModel)
}

func (mr *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return mr.repository.FindByID(ctx, id)
}

func (mr *ServiceImpl) FindAll(
	ctx context.Context,
	page int,
	limit int,
	q string,
) ([]*Model, error) {
	entities, err := mr.repository.FindAll(ctx, page, limit, q)
	if err != nil {
		return nil, err
	}

	return entities, nil
}

func (mr *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *CreateUpdateDto) (*Model, error) {
	// Credentials the client leaves empty keep the stored ones, so editing a channel
	// does not need the passwords typed again (#257).
	stored, err := mr.storedSecret(ctx, id)
	if err != nil {
		return nil, err
	}
	config, secret, err := WriteSecrets(entity.Config, entity.Secret, stored)
	if err != nil {
		return nil, err
	}

	updateModel := &Model{
		ID:        id,
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    &config,
	}
	if secret != "" {
		updateModel.Secret = &secret
	}

	err = mr.repository.UpdateFull(ctx, id, updateModel)
	if err != nil {
		return nil, err
	}

	return updateModel, nil
}

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *PartialUpdateDto) (*Model, error) {
	updateModel := &UpdateModel{
		ID:        &id,
		Name:      entity.Name,
		Type:      entity.Type,
		Active:    entity.Active,
		IsDefault: entity.IsDefault,
		Config:    entity.Config,
	}

	// Same rule as a full update: whatever secret the client did not send is kept. The row
	// is read once, for both the stored secret and the config a partial update leaves alone.
	if entity.Config != nil || entity.Secret != nil {
		current, err := mr.repository.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		stored, configJSON := "", ""
		if current != nil {
			if current.Secret != nil {
				stored = *current.Secret
			}
			if current.Config != nil {
				configJSON = *current.Config
			}
		}
		if entity.Config != nil {
			configJSON = *entity.Config
		}
		explicit := ""
		if entity.Secret != nil {
			explicit = *entity.Secret
		}
		config, secret, err := WriteSecrets(configJSON, explicit, stored)
		if err != nil {
			return nil, err
		}
		updateModel.Config = &config
		if secret != "" {
			updateModel.Secret = &secret
		}
	}

	err := mr.repository.UpdatePartial(ctx, id, updateModel)
	if err != nil {
		return nil, err
	}

	updatedModel, err := mr.repository.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return updatedModel, nil
}

func (mr *ServiceImpl) Delete(ctx context.Context, id string) error {
	err := mr.repository.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Cascade delete monitor_notification relations. The channel is already gone, so a
	// failure here doesn't fail the delete: links left behind are skipped when sending.
	if err := mr.monitorNotificationService.DeleteByNotificationID(ctx, id); err != nil {
		mr.logger.Warnw("Failed to delete the deleted notification channel's monitor links", "id", id, "error", err)
	}

	return nil
}

// storedSecret returns the secret a channel already has, or "" when it has none.
func (mr *ServiceImpl) storedSecret(ctx context.Context, id string) (string, error) {
	current, err := mr.repository.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	if current == nil || current.Secret == nil {
		return "", nil
	}
	return *current.Secret, nil
}
