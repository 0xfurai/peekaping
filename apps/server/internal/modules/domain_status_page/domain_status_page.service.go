package domain_status_page

import (
	"context"
	"fmt"
	"strings"
	"time"

	"peekaping/internal/dbtx"

	"go.uber.org/zap"
)

// Normalize is the form a custom domain is stored in and looked up by: a host name is the same in
// any case, and with spaces around it. The service normalizes every domain it is given, so a
// caller that compares domains itself should too.
func Normalize(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

// DomainTakenError is a domain that another status page holds. Custom domains are unique across the
// install, and the database enforces it, so this is also what two pages that race for a domain get.
type DomainTakenError struct {
	Domain string
}

func (e *DomainTakenError) Error() string {
	return fmt.Sprintf("domain %q is used by another status page", e.Domain)
}

// refuseTaken is err, or a DomainTakenError for the domain when err is the database refusing a
// domain that is held already
func refuseTaken(err error, domain string) error {
	if dbtx.IsUniqueViolation(err) {
		return &DomainTakenError{Domain: domain}
	}
	return err
}

type Service interface {
	Create(ctx context.Context, entity *Model) (*Model, error)
	FindByID(ctx context.Context, id string) (*Model, error)
	FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error)
	UpdateFull(ctx context.Context, id string, entity *Model) (*Model, error)
	UpdatePartial(ctx context.Context, id string, entity *UpdateModel) (*Model, error)
	Delete(ctx context.Context, id string) error

	// Additional methods for managing relationships
	AddDomainToStatusPage(ctx context.Context, statusPageID, domain string) (*Model, error)
	RemoveDomainFromStatusPage(ctx context.Context, statusPageID, domain string) error
	GetDomainsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error)
	FindByStatusPageAndDomain(ctx context.Context, statusPageID, domain string) (*Model, error)
	DeleteAllDomainsForStatusPage(ctx context.Context, statusPageID string) error
	FindByDomain(ctx context.Context, domain string) (*Model, error)

	// FindServedByDomain is FindByDomain for the domains that serve their page: with verification
	// on (VERIFY_CUSTOM_DOMAINS), only the verified ones. The others answer nil, as unknown ones do.
	FindServedByDomain(ctx context.Context, domain string) (*Model, error)
	// VerificationRequired is whether a domain must be verified to serve
	VerificationRequired() bool
	// CNAMETarget is the host a domain may be a CNAME to now, or "" when there is none
	CNAMETarget(ctx context.Context) string
	// EnsureToken returns the row with a verification token, storing a new one if it had none, or
	// ErrDomainNotFound when the row is gone
	EnsureToken(ctx context.Context, m *Model) (*Model, error)
	// Statuses are the states of a status page's domains, with the records to add. The caller has
	// loaded the page in the request's workspace.
	Statuses(ctx context.Context, statusPageID string) ([]*Status, error)
	// CheckNow looks for the records of one of the page's domains and returns its state after, or
	// ErrDomainNotFound when the page doesn't list it. The caller has loaded the page in the
	// request's workspace.
	CheckNow(ctx context.Context, statusPageID, domain string) (*Status, error)
	// CheckDue checks the domains of every workspace that are due, when verification is on
	CheckDue(ctx context.Context) error
	// Run is CheckDue every five minutes until ctx is done, and returns at once when verification
	// is off
	Run(ctx context.Context)
}

type ServiceImpl struct {
	repository Repository
	logger     *zap.SugaredLogger

	// verify is VERIFY_CUSTOM_DOMAINS, and cnameTarget CUSTOM_DOMAIN_CNAME_TARGET as configured
	verify      bool
	cnameTarget string
	resolver    Resolver
	publicURL   PublicURLSource
	now         func() time.Time
}

// Option changes how the service behaves
type Option func(*ServiceImpl)

// WithVerification sets VERIFY_CUSTOM_DOMAINS and CUSTOM_DOMAIN_CNAME_TARGET. Without it no
// domain needs verifying.
func WithVerification(verify bool, cnameTarget string) Option {
	return func(s *ServiceImpl) { s.verify, s.cnameTarget = verify, cnameTarget }
}

// WithResolver sets the resolver the records are looked up with, instead of the process's
func WithResolver(resolver Resolver) Option {
	return func(s *ServiceImpl) { s.resolver = resolver }
}

// WithPublicURL sets where the address of the web app comes from, for the host a CNAME may point
// at when none is configured. Without it there is none.
func WithPublicURL(source PublicURLSource) Option {
	return func(s *ServiceImpl) { s.publicURL = source }
}

// WithClock sets the service's clock, for a test
func WithClock(now func() time.Time) Option {
	return func(s *ServiceImpl) { s.now = now }
}

func NewService(
	repository Repository,
	logger *zap.SugaredLogger,
	opts ...Option,
) Service {
	s := &ServiceImpl{
		repository: repository,
		logger:     logger.Named("[domain-status-page-service]"),
		resolver:   NewNetResolver(),
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (dr *ServiceImpl) Create(ctx context.Context, entity *Model) (*Model, error) {
	normalized := *entity
	normalized.Domain = Normalize(entity.Domain)
	created, err := dr.repository.Create(ctx, &normalized)
	return created, refuseTaken(err, normalized.Domain)
}

func (dr *ServiceImpl) FindByID(ctx context.Context, id string) (*Model, error) {
	return dr.repository.FindByID(ctx, id)
}

func (dr *ServiceImpl) FindAll(ctx context.Context, page int, limit int, q string) ([]*Model, error) {
	return dr.repository.FindAll(ctx, page, limit, q)
}

func (dr *ServiceImpl) UpdateFull(ctx context.Context, id string, entity *Model) (*Model, error) {
	normalized := *entity
	normalized.Domain = Normalize(entity.Domain)
	updated, err := dr.repository.UpdateFull(ctx, id, &normalized)
	return updated, refuseTaken(err, normalized.Domain)
}

func (dr *ServiceImpl) UpdatePartial(ctx context.Context, id string, entity *UpdateModel) (*Model, error) {
	normalized := *entity
	var domain string
	if entity.Domain != nil {
		domain = Normalize(*entity.Domain)
		normalized.Domain = &domain
	}
	updated, err := dr.repository.UpdatePartial(ctx, id, &normalized)
	return updated, refuseTaken(err, domain)
}

func (dr *ServiceImpl) Delete(ctx context.Context, id string) error {
	return dr.repository.Delete(ctx, id)
}

func (dr *ServiceImpl) AddDomainToStatusPage(ctx context.Context, statusPageID, domain string) (*Model, error) {
	domain = Normalize(domain)
	dr.logger.Debugw("Adding domain to status page", "statusPageID", statusPageID, "domain", domain)
	added, err := dr.repository.AddDomainToStatusPage(ctx, statusPageID, domain)
	return added, refuseTaken(err, domain)
}

func (dr *ServiceImpl) RemoveDomainFromStatusPage(ctx context.Context, statusPageID, domain string) error {
	return dr.repository.RemoveDomainFromStatusPage(ctx, statusPageID, Normalize(domain))
}

func (dr *ServiceImpl) GetDomainsForStatusPage(ctx context.Context, statusPageID string) ([]*Model, error) {
	return dr.repository.GetDomainsForStatusPage(ctx, statusPageID)
}

func (dr *ServiceImpl) FindByStatusPageAndDomain(ctx context.Context, statusPageID, domain string) (*Model, error) {
	return dr.repository.FindByStatusPageAndDomain(ctx, statusPageID, Normalize(domain))
}

func (dr *ServiceImpl) DeleteAllDomainsForStatusPage(ctx context.Context, statusPageID string) error {
	return dr.repository.DeleteAllDomainsForStatusPage(ctx, statusPageID)
}

func (dr *ServiceImpl) FindByDomain(ctx context.Context, domain string) (*Model, error) {
	return dr.repository.FindByDomain(ctx, Normalize(domain))
}
