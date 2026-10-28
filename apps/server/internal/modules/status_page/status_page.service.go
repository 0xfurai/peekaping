package status_page

import (
	"context"
	"errors"
	"fmt"
	"peekaping/internal/dbtx"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"

	"go.uber.org/zap"
)

// Service works on status pages, which belong to a workspace (docs/adr/0005 section 3). The methods
// that serve a workspace take it right after the context, and answer nil or ErrNotFound for a page
// of another workspace. A page's slug and custom domains are unique across the install (section
// 6): one that another workspace's page holds is refused as taken, with the answer the same
// workspace's page gets. The AnyWorkspace methods serve the public pages, which a visitor finds by
// slug or custom domain, the badges, and the usage ping.
type Service interface {
	// Create makes the page in the workspace. Every monitor it names must be in the workspace, or
	// the error wraps monitor_status_page.ErrUnknownMonitor and nothing is written.
	Create(ctx context.Context, workspaceID workspace.ID, dto *CreateStatusPageDTO) (*Model, error)
	FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error)
	FindByIDWithMonitors(ctx context.Context, workspaceID workspace.ID, id string) (*StatusPageWithMonitorsResponseDTO, error)
	// FindPublishedBySlugAnyWorkspace serves a visitor, who is in no workspace
	FindPublishedBySlugAnyWorkspace(ctx context.Context, slug string) (*Model, error)
	// FindPublishedByDomainAnyWorkspace serves a visitor of a custom domain
	FindPublishedByDomainAnyWorkspace(ctx context.Context, domain string) (*Model, error)
	FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error)
	// FindAllAnyWorkspace lists the pages of every workspace: the usage ping describes the install
	FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error)
	// Update changes the page. Like Create, it refuses a monitor that isn't in the workspace before
	// it writes anything.
	Update(ctx context.Context, workspaceID workspace.ID, id string, dto *UpdateStatusPageDTO) (*Model, error)
	Delete(ctx context.Context, workspaceID workspace.ID, id string) error

	// GetMonitorsForStatusPage lists the links of a page the caller has loaded already: a page of
	// the request's workspace, or a published page
	GetMonitorsForStatusPage(ctx context.Context, statusPageID string) ([]*monitor_status_page.Model, error)
	// IsMonitorPublishedAnyWorkspace serves the badges, which a visitor asks for
	IsMonitorPublishedAnyWorkspace(ctx context.Context, monitorID string) (bool, error)
}

type ServiceImpl struct {
	repository               Repository
	eventBus                 events.EventBus
	monitorStatusPageService monitor_status_page.Service
	domainStatusPageService  domain_status_page.Service
	logger                   *zap.SugaredLogger
}

func NewService(
	repository Repository,
	eventBus events.EventBus,
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

// DomainAlreadyUsedError represents a validation error when a domain is already
// attached to a different status page.
type DomainAlreadyUsedError struct {
	Code   string `json:"code"`
	Domain string `json:"domain"`
}

func (e *DomainAlreadyUsedError) Error() string {
	return fmt.Sprintf(`{"code":"%s", "domain":"%s"}`, e.Code, e.Domain)
}

func domainAlreadyUsedError(domain string) *DomainAlreadyUsedError {
	return &DomainAlreadyUsedError{
		Code:   "DOMAIN_EXISTS",
		Domain: domain,
	}
}

// SlugAlreadyUsedError represents a validation error when a slug is already held by another status
// page. Slugs are unique across the install, so the page may be in any workspace, and the error
// says nothing about it.
type SlugAlreadyUsedError struct {
	Code string `json:"code"`
	Slug string `json:"slug"`
}

func (e *SlugAlreadyUsedError) Error() string {
	return fmt.Sprintf(`{"code":"%s", "slug":"%s"}`, e.Code, e.Slug)
}

func slugAlreadyUsedError(slug string) *SlugAlreadyUsedError {
	return &SlugAlreadyUsedError{
		Code: "SLUG_EXISTS",
		Slug: slug,
	}
}

// validateSlug ensures that no other status page, in any workspace, holds the slug. The page
// itself, when it has an ID, may keep its own.
func (s *ServiceImpl) validateSlug(ctx context.Context, statusPageID, slug string) error {
	existing, err := s.repository.FindBySlugAnyWorkspace(ctx, slug)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != statusPageID {
		return slugAlreadyUsedError(slug)
	}
	return nil
}

// slugWriteError is the error of a write of a slug: a slug that another page took since the check
// is reported as the check reports it.
func slugWriteError(err error, slug string) error {
	if dbtx.IsUniqueViolation(err) {
		return slugAlreadyUsedError(slug)
	}
	return err
}

// normalizedDomains is the domains as they are stored: lower-case, trimmed, and each once.
// A domain that is empty once trimmed is no domain.
func normalizedDomains(domains []string) []string {
	normalized := make([]string, 0, len(domains))
	seen := map[string]bool{}
	for _, domain := range domains {
		domain = domain_status_page.Normalize(domain)
		if domain != "" && !seen[domain] {
			seen[domain] = true
			normalized = append(normalized, domain)
		}
	}
	return normalized
}

// addDomain gives the status page a domain. A domain that another page took since the check, or
// that the database refuses for being held, is reported as the check reports it.
func (s *ServiceImpl) addDomain(ctx context.Context, statusPageID, domain string) error {
	_, err := s.domainStatusPageService.AddDomainToStatusPage(ctx, statusPageID, domain)
	var taken *domain_status_page.DomainTakenError
	if errors.As(err, &taken) {
		return domainAlreadyUsedError(taken.Domain)
	}
	return err
}

// validateDomains ensures that provided domains, in any case, are not already used
// by any existing status page.
func (s *ServiceImpl) validateDomains(ctx context.Context, statusPageID string, domains []string) error {
	for _, domain := range normalizedDomains(domains) {
		existing, err := s.domainStatusPageService.FindByDomain(ctx, domain)
		if err != nil {
			return err
		}
		if existing != nil && existing.StatusPageID != statusPageID {
			return domainAlreadyUsedError(domain)
		}
	}
	return nil
}

func (s *ServiceImpl) Create(ctx context.Context, workspaceID workspace.ID, dto *CreateStatusPageDTO) (*Model, error) {
	dto.MonitorIDs = utils.LowerUUIDs(dto.MonitorIDs)
	// Every monitor must be in the workspace, and is checked before anything is written
	if err := s.monitorStatusPageService.RequireMonitors(ctx, workspaceID, dto.MonitorIDs); err != nil {
		return nil, err
	}

	// The slug is checked before the page is written, and again by the database if another page
	// takes it in between
	if err := s.validateSlug(ctx, "", dto.Slug); err != nil {
		return nil, err
	}

	// Pre-validate domain uniqueness before creating the status page to avoid
	// partial creation without domains.
	domains := normalizedDomains(dto.Domains)
	if len(domains) > 0 {
		if err := s.validateDomains(ctx, "", domains); err != nil {
			return nil, err
		}
	}

	model := &Model{
		Slug:                dto.Slug,
		Title:               dto.Title,
		Description:         dto.Description,
		Icon:                dto.Icon,
		Theme:               dto.Theme,
		Published:           dto.Published,
		ShowLocations:       dto.ShowLocations,
		FooterText:          dto.FooterText,
		AutoRefreshInterval: dto.AutoRefreshInterval,
	}

	created, err := s.repository.Create(ctx, workspaceID, model)
	if err != nil {
		return nil, slugWriteError(err, dto.Slug)
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
	if len(domains) > 0 {
		s.logger.Debugw("Adding domains to status page", "statusPageID", created.ID, "domains", domains)
		for _, domain := range domains {
			err := s.addDomain(ctx, created.ID, domain)
			if err != nil {
				// Since we pre-validate, reaching here likely means a race. Surface error.
				return nil, err
			}
		}
	}

	return created, nil
}

func (s *ServiceImpl) FindByID(ctx context.Context, workspaceID workspace.ID, id string) (*Model, error) {
	return s.repository.FindByID(ctx, workspaceID, id)
}

func (s *ServiceImpl) FindByIDWithMonitors(
	ctx context.Context, workspaceID workspace.ID, id string,
) (*StatusPageWithMonitorsResponseDTO, error) {
	s.logger.Debugw("Finding status page by ID with monitors", "id", id)

	// First, get the status page model
	model, err := s.repository.FindByID(ctx, workspaceID, id)
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

// FindPublishedBySlugAnyWorkspace returns the published status page at slug, whatever workspace it
// is in: a visitor is in none. A page that doesn't exist or isn't published is nil, so public
// callers can't tell the two apart.
func (s *ServiceImpl) FindPublishedBySlugAnyWorkspace(ctx context.Context, slug string) (*Model, error) {
	return publishedOnly(s.repository.FindBySlugAnyWorkspace(ctx, slug))
}

// FindPublishedByDomainAnyWorkspace returns the published status page served at a custom
// domain, or nil like FindPublishedBySlugAnyWorkspace.
func (s *ServiceImpl) FindPublishedByDomainAnyWorkspace(ctx context.Context, domain string) (*Model, error) {
	// Find the domain-status page relationship
	domainStatusPage, err := s.domainStatusPageService.FindByDomain(ctx, domain)
	if err != nil {
		return nil, err
	}
	if domainStatusPage == nil {
		return nil, nil
	}

	return publishedOnly(s.repository.FindByIDAnyWorkspace(ctx, domainStatusPage.StatusPageID))
}

func publishedOnly(page *Model, err error) (*Model, error) {
	if err != nil || page == nil || !page.Published {
		return nil, err
	}
	return page, nil
}

func (s *ServiceImpl) FindAll(ctx context.Context, workspaceID workspace.ID, page int, limit int, q string) ([]*Model, error) {
	return s.repository.FindAll(ctx, workspaceID, page, limit, q)
}

func (s *ServiceImpl) FindAllAnyWorkspace(ctx context.Context, page int, limit int) ([]*Model, error) {
	return s.repository.FindAllAnyWorkspace(ctx, page, limit)
}

func (s *ServiceImpl) Update(ctx context.Context, workspaceID workspace.ID, id string, dto *UpdateStatusPageDTO) (*Model, error) {
	// The page must be in the workspace, and so must every monitor it is given: they are checked
	// before anything is written
	found, err := s.repository.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, ErrNotFound
	}
	if dto.MonitorIDs != nil {
		lowered := utils.LowerUUIDs(*dto.MonitorIDs)
		dto.MonitorIDs = &lowered
		if err := s.monitorStatusPageService.RequireMonitors(ctx, workspaceID, *dto.MonitorIDs); err != nil {
			return nil, err
		}
	}

	if dto.Slug != nil {
		if err := s.validateSlug(ctx, id, *dto.Slug); err != nil {
			return nil, err
		}
	}

	updateModel := &UpdateModel{
		Slug:                dto.Slug,
		Title:               dto.Title,
		Description:         dto.Description,
		Icon:                dto.Icon,
		Theme:               dto.Theme,
		Published:           dto.Published,
		ShowLocations:       dto.ShowLocations,
		FooterText:          dto.FooterText,
		AutoRefreshInterval: dto.AutoRefreshInterval,
	}

	err = s.repository.Update(ctx, workspaceID, id, updateModel)
	if err != nil {
		if dto.Slug != nil {
			err = slugWriteError(err, *dto.Slug)
		}
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
		// The list as it is stored, so that the page's own domain in another case is kept
		domains := normalizedDomains(*dto.Domains)

		// Pre-validate the new domain list for uniqueness against other pages
		if err := s.validateDomains(ctx, id, domains); err != nil {
			return nil, err
		}

		for _, domain := range domains {
			if err := s.addDomain(ctx, id, domain); err != nil {
				return nil, err
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
		for _, domain := range domains {
			newDomainIDs[domain] = true
		}

		// Remove domains that are no longer in the list
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

	return s.repository.FindByID(ctx, workspaceID, id)
}

func (s *ServiceImpl) Delete(ctx context.Context, workspaceID workspace.ID, id string) error {
	err := s.repository.Delete(ctx, workspaceID, id)
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

// IsMonitorPublishedAnyWorkspace reports whether the monitor is on at least one published
// status page. The badge of a monitor is public, so the page is found in whatever workspace it is.
func (s *ServiceImpl) IsMonitorPublishedAnyWorkspace(ctx context.Context, monitorID string) (bool, error) {
	links, err := s.monitorStatusPageService.GetStatusPagesForMonitor(ctx, monitorID)
	if err != nil {
		return false, err
	}
	for _, link := range links {
		page, err := s.repository.FindByIDAnyWorkspace(ctx, link.StatusPageID)
		if err != nil {
			return false, err
		}
		if page != nil && page.Published {
			return true, nil
		}
	}
	return false, nil
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
		ShowLocations:       model.ShowLocations,
		CreatedAt:           model.CreatedAt,
		UpdatedAt:           model.UpdatedAt,
		FooterText:          model.FooterText,
		AutoRefreshInterval: model.AutoRefreshInterval,
		MonitorIDs:          monitorIDs,
		Domains:             domains,
	}
}
