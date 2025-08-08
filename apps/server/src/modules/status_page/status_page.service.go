package status_page

import (
	"context"
	"peekaping/src/modules/domain_status_page"
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

	GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*monitor_status_page.Model, error)
}

type ServiceImpl struct {
	repository               Repository
	eventBus                 *events.EventBus
	monitorStatusPageService monitor_status_page.Service
	domainStatusPageService  domain_status_page.Service
	logger                   *zap.SugaredLogger
}

func NewService(
	repository Repository,
	eventBus *events.EventBus,
	monitorStatusPageService monitor_status_page.Service,
	domainStatusPageService domain_status_page.Service,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repository:               repository,
		eventBus:                 eventBus,
		monitorStatusPageService: monitorStatusPageService,
		domainStatusPageService:  domainStatusPageService,
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
	}

	created, err := s.repository.Create(ctx, model)
	if err != nil {
		return nil, err
	}

	// Add monitors if provided
	s.logger.Debugw("Adding monitors to status page", "statusPageID", created.ID, "monitorIDs", dto.MonitorIDs)
	if len(dto.MonitorIDs) > 0 {
		for i, monitorID := range dto.MonitorIDs {
			_, err := s.monitorStatusPageService.AddMonitorToStatusPage(ctx, created.ID, monitorID, i, true)
			if err != nil {
				s.logger.Errorw("Failed to add monitor to status page", "error", err)
				continue
			}
		}
	}

	// Add domains if provided
	if len(dto.Domains) > 0 {
		s.logger.Debugw("Adding domains to status page", "statusPageID", created.ID, "domains", dto.Domains)
		for _, domain := range dto.Domains {
			_, err := s.domainStatusPageService.AddDomainToStatusPage(ctx, created.ID, domain)
			if err != nil {
				s.logger.Errorw("Failed to add domain to status page", "error", err)
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
	monitors, err := s.monitorStatusPageService.GetMonitorsForStatusPage(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to get monitors for status page", "error", err, "statusPageID", id)
		return nil, err
	}

	// Extract monitor IDs from the monitor_status_page models
	monitorIDs := make([]string, len(monitors))
	for i, monitor := range monitors {
		monitorIDs[i] = monitor.MonitorID
	}

	domains, err := s.domainStatusPageService.GetDomainsForStatusPage(ctx, id)
	if err != nil {
		s.logger.Errorw("Failed to get domains for status page", "error", err, "statusPageID", id)
		return nil, err
	}

	domainsDTO := make([]string, len(domains))
	for i, domain := range domains {
		domainsDTO[i] = domain.Domain
	}

	s.logger.Debugw("Successfully found status page with:",
		"id", id,
		"monitorCount", len(monitorIDs),
		"monitorIDs", monitorIDs,
		"domainCount", len(domainsDTO),
		"domains", domainsDTO)

	// Map the model to the DTO
	dto := s.mapModelToStatusPageWithMonitorsDTO(model, monitorIDs, domainsDTO)

	return dto, nil
}

func (s *ServiceImpl) FindBySlug(ctx context.Context, slug string) (*Model, error) {
	return s.repository.FindBySlug(ctx, slug)
}

func (s *ServiceImpl) FindByDomain(ctx context.Context, domain string) (*Model, error) {
	// Find the domain-status page relationship
	domainStatusPage, err := s.domainStatusPageService.FindByDomain(ctx, domain)
	if err != nil {
		return nil, err
	}
	if domainStatusPage == nil {
		return nil, nil
	}

	// Get the status page
	return s.repository.FindByID(ctx, domainStatusPage.StatusPageID)
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
	}

	err := s.repository.Update(ctx, id, updateModel)
	if err != nil {
		return nil, err
	}

	// Update monitors if provided
	if dto.MonitorIDs != nil {
		// Get current monitors
		currentMonitors, err := s.monitorStatusPageService.GetMonitorsForStatusPage(ctx, id)
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
				err := s.monitorStatusPageService.RemoveMonitorFromStatusPage(ctx, id, monitorID)
				if err != nil {
					// Log the error but don't fail the entire update
					continue
				}
			}
		}

		// Add new monitors
		for i, monitorID := range *dto.MonitorIDs {
			if !currentMonitorIDs[monitorID] {
				_, err := s.monitorStatusPageService.AddMonitorToStatusPage(ctx, id, monitorID, i, true)
				if err != nil {
					// Log the error but don't fail the entire update
					continue
				}
			}
		}
	}

	if dto.Domains != nil {
		if len(*dto.Domains) > 0 {
			for _, domain := range *dto.Domains {
				_, err := s.domainStatusPageService.AddDomainToStatusPage(ctx, id, domain)
				if err != nil {
					s.logger.Errorw("Failed to add domain to status page", "error", err)
					continue
				}
			}
		}

		// Get current monitors
		currentDomains, err := s.domainStatusPageService.GetDomainsForStatusPage(ctx, id)
		if err != nil {
			return nil, err
		}

		// Remove monitors that are no longer in the list
		currentDomainIDs := make(map[string]bool)
		for _, domain := range currentDomains {
			currentDomainIDs[domain.Domain] = true
		}

		newDomainIDs := make(map[string]bool)
		for _, domain := range *dto.Domains {
			newDomainIDs[domain] = true
		}

		// Remove monitors that are no longer in the list
		for domain := range currentDomainIDs {
			if !newDomainIDs[domain] {
				err := s.domainStatusPageService.RemoveDomainFromStatusPage(ctx, id, domain)
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

func (s *ServiceImpl) GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*monitor_status_page.Model, error) {
	return s.monitorStatusPageService.GetMonitorsForStatusPage(ctx, statusPageID)
}

// mapModelToStatusPageWithMonitorsDTO converts a Model to StatusPageWithMonitorsDTO
func (s *ServiceImpl) mapModelToStatusPageWithMonitorsDTO(model *Model, monitorIDs []string, domains []string) *StatusPageWithMonitorsResponseDTO {
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
		MonitorIDs:          monitorIDs,
		Domains:             domains,
	}
}
