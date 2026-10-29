package proxy

import (
	"context"
	"peekaping/internal/dbtx"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/netguard"

	"go.uber.org/dig"
	"go.uber.org/zap"
)

// Service works on proxies, which belong to a workspace (docs/adr/0005 section 3). Every method
// takes the workspace right after the context, and answers ErrNotFound (or nil, for a find) for a
// proxy of another workspace.
type Service interface {
	// Create makes the proxy in the workspace
	Create(ctx context.Context, workspaceID workspace.ID, entity *CreateUpdateDto) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *CreateUpdateDto) (*Model, error)
	UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *PartialUpdateDto) (*Model, error)
	// Delete removes the proxy from the workspace's monitors, which are told they changed so that
	// they stop using it, and then deletes it
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error
}

type ServiceImpl struct {
	repository     Repository
	monitorService monitor.Service
	transactor     dbtx.Transactor
	guard          *netguard.Guard
	logger         *zap.SugaredLogger
}

type NewServiceParams struct {
	dig.In
	Repository     Repository
	MonitorService monitor.Service
	// Transactor makes the deletes of a proxy and of the references to it one transaction
	Transactor dbtx.Transactor
	// Guard refuses a proxy at an address the install blocks (BLOCK_PRIVATE_TARGETS, PP-156). A
	// container that has none blocks nothing.
	Guard  *netguard.Guard `optional:"true"`
	Logger *zap.SugaredLogger
}

func NewService(params NewServiceParams) Service {
	return &ServiceImpl{
		repository:     params.Repository,
		monitorService: params.MonitorService,
		transactor:     params.Transactor,
		guard:          params.Guard,
		logger:         params.Logger.Named("[proxy-service]"),
	}
}

func (mr *ServiceImpl) Create(ctx context.Context, workspaceID workspace.ID, entity *CreateUpdateDto) (*Model, error) {
	if err := mr.guard.CheckLiteral(entity.Host); err != nil {
		return nil, err
	}
	model := &Model{
		Protocol: entity.Protocol,
		Host:     entity.Host,
		Port:     entity.Port,
		Auth:     entity.Auth,
		Username: entity.Username,
		Password: entity.Password,
	}
	return mr.repository.Create(ctx, workspaceID, model)
}

func (mr *ServiceImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	return mr.repository.FindByID(ctx, workspaceID, id)
}

func (mr *ServiceImpl) FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error) {
	return mr.repository.FindAll(ctx, workspaceID, page, limit, q)
}

func (mr *ServiceImpl) UpdateFull(ctx context.Context, workspaceID workspace.ID, id string, entity *CreateUpdateDto) (*Model, error) {
	if err := mr.guard.CheckLiteral(entity.Host); err != nil {
		return nil, err
	}
	model := &Model{
		Protocol: entity.Protocol,
		Host:     entity.Host,
		Port:     entity.Port,
		Auth:     entity.Auth,
		Username: entity.Username,
		Password: entity.Password,
	}
	updated, err := mr.repository.UpdateFull(ctx, workspaceID, id, model)
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func (mr *ServiceImpl) UpdatePartial(ctx context.Context, workspaceID workspace.ID, id string, entity *PartialUpdateDto) (*Model, error) {
	// An update that leaves the host alone is not held to the setting: a proxy stored before it was
	// turned on can still be renamed or have its port changed
	if entity.Host != nil {
		if err := mr.guard.CheckLiteral(*entity.Host); err != nil {
			return nil, err
		}
	}
	updateModel := &UpdateModel{
		Protocol: entity.Protocol,
		Host:     entity.Host,
		Port:     entity.Port,
		Auth:     entity.Auth,
		Username: entity.Username,
		Password: entity.Password,
	}
	updated, err := mr.repository.UpdatePartial(ctx, workspaceID, id, updateModel)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (mr *ServiceImpl) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	// A proxy of another workspace is left alone, and so are the monitors that name it
	found, err := mr.repository.FindByID(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if found == nil {
		return ErrNotFound
	}

	// The workspace's monitors stop using the proxy, and are told they changed. The database
	// would clear the reference when the proxy goes, but silently: nothing would tell the
	// scheduler to load the monitors' config again. Clearing them and deleting the proxy are one
	// transaction, so a delete that fails leaves the monitors their proxy, and the events go out
	// only after it has committed.
	var changed []*monitor.Model
	err = mr.transactor.WithTx(ctx, func(ctx context.Context) error {
		var err error
		if changed, err = mr.monitorService.RemoveProxyReference(ctx, workspaceID, id); err != nil {
			return err
		}
		return mr.repository.Delete(ctx, workspaceID, id)
	})
	if err != nil {
		return err
	}
	mr.monitorService.PublishUpdated(ctx, changed)
	return nil
}
