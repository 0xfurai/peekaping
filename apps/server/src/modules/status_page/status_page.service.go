package status_page

import (
	"context"
	"peekaping/src/modules/events"
	"peekaping/src/modules/monitor_status_page"

	"go.uber.org/zap"
)

type Service interface {
	Create(ctx context.Context, dto *CreateStatusPageDTO) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindByIDWithMonitors(ctx context.Context, id string) (*StatusPageWithMonitorsResponseDTO, error)
	FindBySlug(ctx context.Context, slug string) (*Model, error)
	FindByDomain(ctx context.Context, domain string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	Update(ctx context.Context, id string, dto *UpdateStatusPageDTO) (*Model, error)
	Delete(ctx context.Context, id string) error

	// Monitor relationship methods
	AddMonitor(ctx context.Context, statusPageID, monitorID string, order int, active bool) (*monitor_status_page.Model, error)
	RemoveMonitor(ctx context.Context, statusPageID, monitorID string) error
	GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*monitor_status_page.Model, error)
	UpdateMonitorOrder(ctx context.Context, statusPageID, monitorID string, order int) (*monitor_status_page.Model, error)
	UpdateMonitorActiveStatus(ctx context.Context, statusPageID, monitorID string, active bool) (*monitor_status_page.Model, error)
}

type ServiceImpl struct {
	repository               Repository
	eventBus                 *events.EventBus
	monitorStatusPageService monitor_status_page.Service
	logger                   *zap.SugaredLogger
}

func NewService(
	repository Repository,
	eventBus *events.EventBus,
	monitorStatusPageService monitor_status_page.Service,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository:               repository,
		eventBus:                 eventBus,
		monitorStatusPageService: monitorStatusPageService,
		logger:                   logger.Named("[status-page-service]"),
	}
}

func (s *ServiceImpl) Create(ctx context.Context, dto *CreateStatusPageDTO) (*Model, error) {
	model := &Model{
		Slug:                dto.Slug,
		Title:               dto.Title,
		Description:         dto.Description,
		Icon:                dto.Icon,
		Theme:               dto.Theme,
		Published:           dto.Published,
		FooterText:          dto.FooterText,
		AutoRefreshInterval: dto.AutoRefreshInterval,
		Domain:              dto.Domain,
	}

	created, err := s.repository.Create(ctx, model)
	if err != nil {
		return nil, err
	}

	// Add monitors if provided
	s.logger.Debugw("Adding monitors to status page", "statusPageID", created.ID, "monitorIDs", dto.MonitorIDs)
	if len(dto.MonitorIDs) > 0 {
		for i, monitorID := range dto.MonitorIDs {
			_, err := s.AddMonitor(ctx, created.ID, monitorID, i, true)
			if err != nil {
				s.logger.Errorw("Failed to add monitor to status page", "error", err)
				continue
			}
		}
	}

	return created, nil
}

func (s *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *ServiceImpl) FindByIDWithMonitors(
	ctx context.Context, id string,
) (*StatusPageWithMonitorsResponseDTO, error) {
	s.logger.Debugw("Finding status page by ID with monitors", "id", id)

	// First, get the status page model
	model, err := s.repository.FindByID(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to find status page by ID", "error", err, "id", id)
		return nil, err
	}

	if model == nil {
		s.logger.Debugw("Status page not found", "id", id)
		return nil, nil
	}

	// Get the monitors for this status page
	monitors, err := s.GetMonitorsForStatusPage(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to get monitors for status page", "error", err, "statusPageID", id)
		return nil, err
	}

	// Extract monitor IDs from the monitor_status_page models
	monitorIDs := make([]string, len(monitors))
	for i, monitor := range monitors {
		monitorIDs[i] = monitor.MonitorID
	}

	s.logger.Debugw("Successfully found status page with monitors",
		"id", id,
		"monitorCount", len(monitorIDs),
		"monitorIDs", monitorIDs)

	// Map the model to the DTO
	dto := s.mapModelToStatusPageWithMonitorsDTO(model, monitorIDs)

	return dto, nil
}

func (s *ServiceImpl) FindBySlug(ctx context.Context, slug string) (*Model, error) {
	return s.repository.FindBySlug(ctx, slug)
}

func (s *ServiceImpl) FindByDomain(ctx context.Context, domain string) (*Model, error) {
	return s.repository.FindByDomain(ctx, domain)
}

func (s *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	return s.repository.FindAll(ctx, page, limit, q)
}

func (s *ServiceImpl) Update(ctx context.Context, id string, dto *UpdateStatusPageDTO) (*Model, error) {
	updateModel := &UpdateModel{
		Slug:                dto.Slug,
		Title:               dto.Title,
		Description:         dto.Description,
		Icon:                dto.Icon,
		Theme:               dto.Theme,
		Published:           dto.Published,
		FooterText:          dto.FooterText,
		AutoRefreshInterval: dto.AutoRefreshInterval,
		Domain:              dto.Domain,
	}

	err := s.repository.Update(ctx, id, updateModel)
	if err != nil {
		return nil, err
	}

	// Update monitors if provided
	if dto.MonitorIDs != nil {
		// Get current monitors
		currentMonitors, err := s.GetMonitorsForStatusPage(ctx, id)
		if err != nil {
			return nil, err
		}

		// Remove monitors that are no longer in the list
		currentMonitorIDs := make(map[string]bool)
		for _, monitor := range currentMonitors {
			currentMonitorIDs[monitor.MonitorID] = true
		}

		newMonitorIDs := make(map[string]bool)
		for _, monitorID := range *dto.MonitorIDs {
			newMonitorIDs[monitorID] = true
		}

		// Remove monitors that are no longer in the list
		for monitorID := range currentMonitorIDs {
			if !newMonitorIDs[monitorID] {
				err := s.RemoveMonitor(ctx, id, monitorID)
				if err != nil {
					// Log the error but don't fail the entire update
					continue
				}
			}
		}

		// Add new monitors
		for i, monitorID := range *dto.MonitorIDs {
			if !currentMonitorIDs[monitorID] {
				_, err := s.AddMonitor(ctx, id, monitorID, i, true)
				if err != nil {
					// Log the error but don't fail the entire update
					continue
				}
			}
		}
	}

	return s.repository.FindByID(ctx, id)
}

func (s *ServiceImpl) Delete(ctx context.Context, id string) error {
	err := s.repository.Delete(ctx, id)
	if err != nil {
		return err
	}

	err = s.monitorStatusPageService.DeleteAllMonitorsForStatusPage(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to delete all monitors for status page", "error", err, "statusPageID", id)
		return err
	}

	return nil
}

func (s *ServiceImpl) AddMonitor(ctx context.Context, statusPageID, monitorID string, order int, active bool) (*monitor_status_page.Model, error) {
	s.logger.Debugw("Adding monitor to status page", "statusPageID", statusPageID, "monitorID", monitorID, "order", order, "active", active)
	return s.monitorStatusPageService.AddMonitorToStatusPage(ctx, statusPageID, monitorID, order, active)
}

func (s *ServiceImpl) RemoveMonitor(ctx context.Context, statusPageID, monitorID string) error {
	return s.monitorStatusPageService.RemoveMonitorFromStatusPage(ctx, statusPageID, monitorID)
}

func (s *ServiceImpl) GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*monitor_status_page.Model, error) {
	return s.monitorStatusPageService.GetMonitorsForStatusPage(ctx, statusPageID)
}

func (s *ServiceImpl) UpdateMonitorOrder(ctx context.Context, statusPageID, monitorID string, order int) (*monitor_status_page.Model, error) {
	return s.monitorStatusPageService.UpdateMonitorOrder(ctx, statusPageID, monitorID, order)
}

func (s *ServiceImpl) UpdateMonitorActiveStatus(ctx context.Context, statusPageID, monitorID string, active bool) (*monitor_status_page.Model, error) {
	return s.monitorStatusPageService.UpdateMonitorActiveStatus(ctx, statusPageID, monitorID, active)
}

// mapModelToStatusPageWithMonitorsDTO converts a Model to StatusPageWithMonitorsDTO
func (s *ServiceImpl) mapModelToStatusPageWithMonitorsDTO(model *Model, monitorIDs []string) *StatusPageWithMonitorsResponseDTO {
	return &StatusPageWithMonitorsResponseDTO{
		ID:                  model.ID,
		Slug:                model.Slug,
		Title:               model.Title,
		Description:         model.Description,
		Icon:                model.Icon,
		Theme:               model.Theme,
		Published:           model.Published,
		CreatedAt:           model.CreatedAt,
		UpdatedAt:           model.UpdatedAt,
		FooterText:          model.FooterText,
		AutoRefreshInterval: model.AutoRefreshInterval,
		Domain:              model.Domain,
		MonitorIDs:          monitorIDs,
	}
}
