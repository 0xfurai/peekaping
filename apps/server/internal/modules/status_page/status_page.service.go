package status_page

import (
	"context"
	"errors"
	"fmt"
	"peekaping/internal/dbtx"
	"peekaping/internal/limits"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"
	"strings"

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
	// FindPublishedBySlugAnyWorkspace serves a visitor, who is in no workspace. A published page
	// whose workspace is suspended is ErrUnavailable.
	FindPublishedBySlugAnyWorkspace(ctx context.Context, slug string) (*Model, error)
	// FindPublishedByDomainAnyWorkspace serves a visitor of a custom domain, and is ErrUnavailable
	// like FindPublishedBySlugAnyWorkspace. A domain that doesn't serve yet (see
	// IsServedAnyWorkspace) is nil, as an unknown one is.
	FindPublishedByDomainAnyWorkspace(ctx context.Context, domain string) (*Model, error)
	// IsServedAnyWorkspace reports whether domain is on the custom domain list and serves, whatever
	// page holds it and whether that page is published: with VERIFY_CUSTOM_DOMAINS on, a domain
	// serves once it is verified. It is the one decision about which hosts get a certificate, for
	// the TLS ask endpoint.
	IsServedAnyWorkspace(ctx context.Context, domain string) (bool, error)
	// DomainStatuses are the states of the page's custom domains and the records to verify them
	// with, or ErrNotFound when the page isn't in the workspace
	DomainStatuses(ctx context.Context, workspaceID workspace.ID, id string) (*DomainStatusesDTO, error)
	// CheckDomain looks for the records of one of the page's custom domains and returns its state
	// after. It is ErrNotFound when the page isn't in the workspace, and
	// domain_status_page.ErrDomainNotFound when the page doesn't list the domain.
	CheckDomain(ctx context.Context, workspaceID workspace.ID, id, domain string) (*domain_status_page.Status, error)
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
	// limits are the workspaces' limits (PP-160), and transactor the transaction their count and the
	// write after it share. Without them no workspace has limits, and a page and its domains are
	// written one by one.
	limits     limits.Source
	transactor dbtx.Transactor
}

// ServiceOption changes how the status page service behaves, for an install that has a reason to
type ServiceOption func(*ServiceImpl)

// WithLimits tells the service each workspace's limits, which it enforces: how many status pages
// the workspace may have, and how many custom domains its pages may have together. A page and its
// domains are then written in one transaction of the transactor, with the count.
func WithLimits(source limits.Source, transactor dbtx.Transactor) ServiceOption {
	return func(s *ServiceImpl) { s.limits, s.transactor = source, transactor }
}

func NewService(
	repository Repository,
	eventBus events.EventBus,
	monitorStatusPageService monitor_status_page.Service,
	domainStatusPageService domain_status_page.Service,
	logger *zap.SugaredLogger,
	opts ...ServiceOption,
) Service {
	s := &ServiceImpl{
		repository:               repository,
		eventBus:                 eventBus,
		monitorStatusPageService: monitorStatusPageService,
		domainStatusPageService:  domainStatusPageService,
		logger:                   logger.Named("[status-page-service]"),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// limitsOf are the workspace's limits (PP-160), none without a source
func (s *ServiceImpl) limitsOf(ctx context.Context, workspaceID workspace.ID) (limits.Limits, error) {
	if s.limits == nil {
		return limits.Limits{}, nil
	}
	l, _, err := s.limits.For(ctx, workspaceID)
	if err != nil {
		return limits.Limits{}, fmt.Errorf("read the workspace's limits: %w", err)
	}
	return l, nil
}

// inTx runs fn in one transaction, or as it is without a transactor
func (s *ServiceImpl) inTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.transactor == nil {
		return fn(ctx)
	}
	return s.transactor.WithTx(ctx, fn)
}

// requireRoom refuses a write that adds pages, or adds domains, past the workspace's limits: pages
// new pages, and addedDomains new domains while removedDomains of its domains go. A write that adds
// none is never refused, so a workspace above a limit that was lowered can still remove. It holds
// the workspace first, in the caller's transaction, so that two such writes at once count one
// after the other (limits.LockWorkspace).
func (s *ServiceImpl) requireRoom(ctx context.Context, workspaceID workspace.ID, l limits.Limits, pages, addedDomains, removedDomains int) error {
	pagesLimited := pages > 0 && l.StatusPages != nil
	domainsLimited := addedDomains > 0 && l.CustomDomains != nil
	if !pagesLimited && !domainsLimited {
		return nil
	}
	if err := s.repository.LockWorkspace(ctx, workspaceID); err != nil {
		return fmt.Errorf("lock the workspace: %w", err)
	}
	if pagesLimited {
		have, err := s.repository.CountPages(ctx, workspaceID)
		if err != nil {
			return fmt.Errorf("count the status pages: %w", err)
		}
		if have+pages > *l.StatusPages {
			return limits.Reached(limits.StatusPages, *l.StatusPages)
		}
	}
	if domainsLimited {
		have, err := s.repository.CountDomains(ctx, workspaceID)
		if err != nil {
			return fmt.Errorf("count the custom domains: %w", err)
		}
		if have-removedDomains+addedDomains > *l.CustomDomains {
			return limits.Reached(limits.CustomDomains, *l.CustomDomains)
		}
	}
	return nil
}

// domainChange is how many of the domains the page doesn't have yet, and how many it has that
// aren't among them. Both lists are normalized.
func domainChange(current []*domain_status_page.Model, domains []string) (added, removed int) {
	have := make(map[string]bool, len(current))
	for _, d := range current {
		have[d.Domain] = true
	}
	keep := make(map[string]bool, len(domains))
	for _, domain := range domains {
		keep[domain] = true
		if !have[domain] {
			added++
		}
	}
	for domain := range have {
		if !keep[domain] {
			removed++
		}
	}
	return added, removed
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

	workspaceLimits, err := s.limitsOf(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	// The page and its domains are stored together or not at all, after the count against the
	// workspace's limits in the same transaction
	var created *Model
	err = s.inTx(ctx, func(ctx context.Context) error {
		if err := s.requireRoom(ctx, workspaceID, workspaceLimits, 1, len(domains), 0); err != nil {
			return err
		}
		var err error
		created, err = s.repository.Create(ctx, workspaceID, model)
		if err != nil {
			return slugWriteError(err, dto.Slug)
		}
		if len(domains) > 0 {
			s.logger.Debugw("Adding domains to status page", "statusPageID", created.ID, "domains", domains)
		}
		for _, domain := range domains {
			// Since we pre-validate, reaching here likely means a race. Surface error.
			if err := s.addDomain(ctx, created.ID, domain); err != nil {
				return err
			}
		}
		return nil
	})
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
// callers can't tell the two apart. A published page whose workspace is suspended, by either hold,
// is ErrUnavailable.
func (s *ServiceImpl) FindPublishedBySlugAnyWorkspace(ctx context.Context, slug string) (*Model, error) {
	page, err := s.repository.FindBySlugAnyWorkspace(ctx, slug)
	return s.available(ctx, page, err)
}

// FindPublishedByDomainAnyWorkspace returns the published status page served at a custom
// domain, or nil like FindPublishedBySlugAnyWorkspace.
func (s *ServiceImpl) FindPublishedByDomainAnyWorkspace(ctx context.Context, domain string) (*Model, error) {
	// Find the domain-status page relationship. A domain that is not verified yet is not served.
	domainStatusPage, err := s.domainStatusPageService.FindServedByDomain(ctx, domain)
	if err != nil {
		return nil, err
	}
	if domainStatusPage == nil {
		return nil, nil
	}

	page, err := s.repository.FindByIDAnyWorkspace(ctx, domainStatusPage.StatusPageID)
	return s.available(ctx, page, err)
}

// IsServedAnyWorkspace reports whether domain is a custom domain of any status page that serves,
// published or not, so a page's certificate is ready when it is published. With verification on
// (VERIFY_CUSTOM_DOMAINS) a domain serves once it is verified, so the ask answers no for a pending
// one. A host is the same in any case and with one trailing dot, which is how a resolver may write
// it. Only the answer leaves: the page, its workspace and its state do not.
func (s *ServiceImpl) IsServedAnyWorkspace(ctx context.Context, domain string) (bool, error) {
	served, err := s.domainStatusPageService.FindServedByDomain(ctx, strings.TrimSuffix(strings.TrimSpace(domain), "."))
	return served != nil, err
}

// available is the page when it is published and its workspace is not suspended: nil when it is not
// published, ErrUnavailable when it is and its workspace is suspended. Whether a page is published
// is checked first, so that an unpublished page of a suspended workspace stays as hidden as any.
func (s *ServiceImpl) available(ctx context.Context, page *Model, err error) (*Model, error) {
	if err != nil || page == nil || !page.Published {
		return nil, err
	}
	suspended, err := s.repository.WorkspaceSuspended(ctx, page.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if suspended {
		return nil, ErrUnavailable
	}
	return page, nil
}

// DomainStatuses loads the page in the workspace first, and then its domains by the page's ID
func (s *ServiceImpl) DomainStatuses(ctx context.Context, workspaceID workspace.ID, id string) (*DomainStatusesDTO, error) {
	if err := s.requirePage(ctx, workspaceID, id); err != nil {
		return nil, err
	}
	statuses, err := s.domainStatusPageService.Statuses(ctx, id)
	if err != nil {
		return nil, err
	}
	return &DomainStatusesDTO{VerificationRequired: s.domainStatusPageService.VerificationRequired(), Domains: statuses}, nil
}

// CheckDomain loads the page in the workspace first, and then checks its domain
func (s *ServiceImpl) CheckDomain(ctx context.Context, workspaceID workspace.ID, id, domain string) (*domain_status_page.Status, error) {
	if err := s.requirePage(ctx, workspaceID, id); err != nil {
		return nil, err
	}
	return s.domainStatusPageService.CheckNow(ctx, id, domain)
}

// requirePage is ErrNotFound unless the page is in the workspace
func (s *ServiceImpl) requirePage(ctx context.Context, workspaceID workspace.ID, id string) error {
	page, err := s.repository.FindByID(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if page == nil {
		return ErrNotFound
	}
	return nil
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

	// The domains are checked before anything is written, so that a refused list changes nothing:
	// none another page holds, and none past the workspace's limit
	var domains []string
	var workspaceLimits limits.Limits
	if dto.Domains != nil {
		// The list as it is stored, so that the page's own domain in another case is kept
		domains = normalizedDomains(*dto.Domains)
		if err := s.validateDomains(ctx, id, domains); err != nil {
			return nil, err
		}
		if workspaceLimits, err = s.limitsOf(ctx, workspaceID); err != nil {
			return nil, err
		}
		currentDomains, err := s.domainStatusPageService.GetDomainsForStatusPage(ctx, id)
		if err != nil {
			return nil, err
		}
		added, removed := domainChange(currentDomains, domains)
		if err := s.requireRoom(ctx, workspaceID, workspaceLimits, 0, added, removed); err != nil {
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

	// The page, its monitors and its domains are written in one transaction, after the count against
	// the workspace's limit in it, so that a refused update stores nothing of the request: the check
	// above saw no other write that may have come since
	err = s.inTx(ctx, func(ctx context.Context) error {
		var currentDomains []*domain_status_page.Model
		if dto.Domains != nil {
			var err error
			if currentDomains, err = s.domainStatusPageService.GetDomainsForStatusPage(ctx, id); err != nil {
				return err
			}
			added, removed := domainChange(currentDomains, domains)
			if err := s.requireRoom(ctx, workspaceID, workspaceLimits, 0, added, removed); err != nil {
				return err
			}
		}

		if err := s.repository.Update(ctx, workspaceID, id, updateModel); err != nil {
			if dto.Slug != nil {
				err = slugWriteError(err, *dto.Slug)
			}
			return err
		}

		// A failed write of a monitor or a domain fails the update: in a transaction it can't be
		// stepped over
		if dto.MonitorIDs != nil {
			currentMonitors, err := s.monitorStatusPageService.GetMonitorsForStatusPage(ctx, id)
			if err != nil {
				return err
			}
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
					if err := s.monitorStatusPageService.RemoveMonitorFromStatusPage(ctx, id, monitorID); err != nil {
						return err
					}
				}
			}

			// Add new monitors
			for i, monitorID := range *dto.MonitorIDs {
				if !currentMonitorIDs[monitorID] {
					if _, err := s.monitorStatusPageService.AddMonitorToStatusPage(ctx, id, monitorID, i, true); err != nil {
						return err
					}
				}
			}
		}

		if dto.Domains != nil {
			for _, domain := range domains {
				if err := s.addDomain(ctx, id, domain); err != nil {
					return err
				}
			}

			newDomainIDs := make(map[string]bool)
			for _, domain := range domains {
				newDomainIDs[domain] = true
			}

			// Remove domains that are no longer in the list
			for _, current := range currentDomains {
				if !newDomainIDs[current.Domain] {
					if err := s.domainStatusPageService.RemoveDomainFromStatusPage(ctx, id, current.Domain); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
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
