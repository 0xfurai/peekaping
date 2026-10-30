package domain_status_page

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"peekaping/internal/config"

	"github.com/danielgtaylor/huma/v2"
)

const (
	// TXTPrefix is the label in front of a domain that holds its TXT record
	TXTPrefix = "_peekaping-verify."
	// TXTValuePrefix is what a TXT record's value starts with, before the domain's token
	TXTValuePrefix = "peekaping-verify="
	// CheckEvery is how often the background check runs
	CheckEvery = 5 * time.Minute

	lookupTimeout = 5 * time.Second
	// checkWorkers is how many domains a pass checks at once, so that slow lookups don't add up
	checkWorkers = 8
)

// ErrDomainNotFound is a domain the status page doesn't list, or that went while it was checked
var ErrDomainNotFound = errors.New("domain not found on the status page")

// Resolver looks up the DNS records a domain is verified by. *net.Resolver is one.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
}

// NewNetResolver is the resolver of the process, which the server verifies domains with
func NewNetResolver() Resolver { return net.DefaultResolver }

// PublicURLSource gives the address of the web app now: CLIENT_URL, or the address an instance
// admin saved. *installsettings.Resolver is one.
type PublicURLSource interface {
	PublicURL(ctx context.Context) string
}

// TXTRecord is the TXT record that proves a domain is its owner's
type TXTRecord struct {
	Name  string `json:"name" example:"_peekaping-verify.status.example.com"`
	Value string `json:"value" example:"peekaping-verify=0f3c..."`
}

// CNAMERecord is the CNAME record that points a domain at this install
type CNAMERecord struct {
	Name   string `json:"name" example:"status.example.com"`
	Target string `json:"target" example:"peekaping.example.com"`
}

// Records are the DNS records a domain is verified by. Either one is enough.
type Records struct {
	TXT TXTRecord `json:"txt"`
	// CNAME is null when the install has no host to point at: its public URL is localhost or an IP
	// address, and CUSTOM_DOMAIN_CNAME_TARGET is not set. Then only the TXT record verifies.
	CNAME *CNAMERecord `json:"cname"`
}

// TransformSchema documents the CNAME as a record or null. Huma has no nullable object field, so
// this does what utils.NullableApiResponse does for its data.
func (Records) TransformSchema(_ huma.Registry, schema *huma.Schema) *huma.Schema {
	if cname := schema.Properties["cname"]; cname != nil {
		schema.Properties["cname"] = &huma.Schema{AnyOf: []*huma.Schema{cname, {Type: "null"}}}
	}
	return schema
}

// Status is what a workspace sees of one of its status page's custom domains
type Status struct {
	Domain string `json:"domain" example:"status.example.com"`
	// State is verified when the domain's records were found, and pending otherwise. A pending
	// domain serves its page only while verification is off.
	State         string     `json:"state" enum:"verified,pending" example:"pending"`
	VerifiedAt    *time.Time `json:"verifiedAt" nullable:"true"`
	LastCheckedAt *time.Time `json:"lastCheckedAt" nullable:"true"`
	// FailingSince is when a check first missed a verified domain's records: the domain goes back
	// to pending when they have been missing for 24 hours. Null while they are there.
	FailingSince *time.Time `json:"failingSince" nullable:"true"`
	// RemoveAt is when a pending domain is removed from its page: 7 days of checks after the first
	// one with verification on found it unverified. A pause in the checks (verification off, the
	// server down) restarts the 7 days. Null for a verified domain, while verification is off, and
	// until that first check.
	RemoveAt *time.Time `json:"removeAt" nullable:"true"`
	Records  Records    `json:"records"`
}

// VerificationRequired is whether a domain must be verified to serve (VERIFY_CUSTOM_DOMAINS)
func (dr *ServiceImpl) VerificationRequired() bool { return dr.verify }

// CNAMETarget is the host a domain may be a CNAME to now, or "" when there is none
func (dr *ServiceImpl) CNAMETarget(ctx context.Context) string {
	var publicURL string
	if dr.publicURL != nil {
		publicURL = dr.publicURL.PublicURL(ctx)
	}
	return config.CustomDomainCNAMETargetFor(dr.cnameTarget, publicURL)
}

// FindServedByDomain is the domain's row when it serves its page: it is listed and, with
// verification on, verified. A domain that is listed and not served answers as an unknown one.
func (dr *ServiceImpl) FindServedByDomain(ctx context.Context, domain string) (*Model, error) {
	m, err := dr.repository.FindByDomain(ctx, Normalize(domain))
	if err != nil || m == nil || (dr.verify && !m.Verified()) {
		return nil, err
	}
	return m, nil
}

// EnsureToken is the row with a verification token: the one it has, or a new one stored first
func (dr *ServiceImpl) EnsureToken(ctx context.Context, m *Model) (*Model, error) {
	if m.VerificationToken != "" {
		return m, nil
	}
	token, err := NewToken()
	if err != nil {
		return nil, err
	}
	stored, err := dr.repository.SetTokenIfEmpty(ctx, m.ID, token)
	if err == nil && stored == nil {
		err = ErrDomainNotFound
	}
	return stored, err
}

// Statuses are the states of a status page's custom domains, each with the records to add
func (dr *ServiceImpl) Statuses(ctx context.Context, statusPageID string) ([]*Status, error) {
	models, err := dr.repository.GetDomainsForStatusPage(ctx, statusPageID)
	if err != nil {
		return nil, err
	}
	target := dr.CNAMETarget(ctx)
	statuses := make([]*Status, 0, len(models))
	for _, m := range models {
		m, err = dr.EnsureToken(ctx, m)
		if errors.Is(err, ErrDomainNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, dr.statusOf(m, target))
	}
	return statuses, nil
}

func (dr *ServiceImpl) statusOf(m *Model, target string) *Status {
	s := &Status{
		Domain:        m.Domain,
		State:         "pending",
		VerifiedAt:    m.VerifiedAt,
		LastCheckedAt: m.LastCheckedAt,
		FailingSince:  m.FailingSince,
		Records:       recordsOf(m, target),
	}
	if m.Verified() {
		s.State = "verified"
	} else if dr.verify && m.PendingSince != nil {
		removeAt := m.PendingSince.Add(PendingLife)
		s.RemoveAt = &removeAt
	}
	return s
}

func recordsOf(m *Model, target string) Records {
	r := Records{TXT: TXTRecord{Name: TXTPrefix + m.Domain, Value: TXTValuePrefix + m.VerificationToken}}
	if target != "" {
		r.CNAME = &CNAMERecord{Name: m.Domain, Target: target}
	}
	return r
}

// CheckNow looks for the records of one of the status page's domains, whether verification is on
// or not, and returns the domain's state after. A pending domain that is past its seven days is
// left to the scheduled check to remove, so that asking for a check never deletes a domain.
func (dr *ServiceImpl) CheckNow(ctx context.Context, statusPageID, domain string) (*Status, error) {
	m, err := dr.repository.FindByStatusPageAndDomain(ctx, statusPageID, Normalize(domain))
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrDomainNotFound
	}
	next, _, err := dr.evaluate(ctx, m)
	if err != nil {
		return nil, err
	}
	if err := dr.repository.SaveVerification(ctx, next); err != nil {
		return nil, err
	}
	return dr.statusOf(next, dr.CNAMETarget(ctx)), nil
}

// CheckDue checks the domains of every workspace that are due, when verification is on: each
// pending one, and each verified one not checked for an hour. A domain that stays pending for
// seven days of checks is removed from its page. A domain whose check fails is logged and left for
// the next pass; one that went from its page meanwhile is left alone.
func (dr *ServiceImpl) CheckDue(ctx context.Context) error {
	if !dr.verify {
		return nil
	}
	due, err := dr.repository.FindDueAnyWorkspace(ctx, dr.now().Add(-RecheckEvery))
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, checkWorkers)
	for _, m := range due {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-slots; wg.Done() }()
			err := dr.checkDue(ctx, m)
			if errors.Is(err, ErrDomainNotFound) {
				dr.logger.Debugw("A custom domain went while it was checked", "domain", m.Domain, "statusPageId", m.StatusPageID)
			} else if err != nil && ctx.Err() == nil {
				dr.logger.Errorw("Failed to check a custom domain", "domain", m.Domain, "statusPageId", m.StatusPageID, "error", err)
			}
		}()
	}
	wg.Wait()
	return nil
}

// checkDue checks one due domain and stores the result, or removes the domain when its time is up
func (dr *ServiceImpl) checkDue(ctx context.Context, m *Model) error {
	next, remove, err := dr.evaluate(ctx, m)
	if err != nil {
		return err
	}
	if !remove {
		return dr.repository.SaveVerification(ctx, next)
	}
	dr.logger.Infow("Removing a custom domain that stayed unverified for 7 days", "domain", m.Domain, "statusPageId", m.StatusPageID)
	return dr.repository.Delete(ctx, m.ID)
}

// evaluate looks for m's records and returns the row after the check, and whether the domain has run
// out of time. The row gets its token first, since the records name it.
func (dr *ServiceImpl) evaluate(ctx context.Context, m *Model) (*Model, bool, error) {
	m, err := dr.EnsureToken(ctx, m)
	if err != nil {
		return nil, false, err
	}
	found := dr.recordsFound(ctx, m)
	next, remove := Decide(m, found, dr.now().UTC(), dr.verify)
	return &next, remove, nil
}

// recordsFound is whether the domain's TXT record or its CNAME is there. A lookup that fails, for
// a name that doesn't exist or for any other reason, finds nothing; the reason goes to the log.
func (dr *ServiceImpl) recordsFound(ctx context.Context, m *Model) bool {
	return dr.txtFound(ctx, m) || dr.cnameFound(ctx, m)
}

func (dr *ServiceImpl) txtFound(ctx context.Context, m *Model) bool {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	want := TXTValuePrefix + m.VerificationToken
	values, err := dr.resolver.LookupTXT(ctx, TXTPrefix+m.Domain)
	if err != nil {
		dr.logger.Debugw("TXT lookup of a custom domain failed", "domain", m.Domain, "error", err)
	}
	for _, value := range values {
		if strings.EqualFold(strings.Trim(strings.TrimSpace(value), `"`), want) {
			return true
		}
	}
	return false
}

func (dr *ServiceImpl) cnameFound(ctx context.Context, m *Model) bool {
	target := dr.CNAMETarget(ctx)
	if target == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	canonical, err := dr.resolver.LookupCNAME(ctx, m.Domain)
	if err != nil {
		dr.logger.Debugw("CNAME lookup of a custom domain failed", "domain", m.Domain, "error", err)
		return false
	}
	canonical = config.NormalizeHost(canonical)
	// A host with no CNAME is its own canonical name; a domain that is the target is no proof
	return canonical == target && canonical != config.NormalizeHost(m.Domain)
}

// Run checks the due domains every CheckEvery until ctx is done, starting with one pass at once. It
// returns at once when verification is off.
func (dr *ServiceImpl) Run(ctx context.Context) {
	if !dr.verify {
		return
	}
	ticker := time.NewTicker(CheckEvery)
	defer ticker.Stop()
	for {
		if err := dr.CheckDue(ctx); err != nil && ctx.Err() == nil {
			dr.logger.Errorw("Failed to check the custom domains", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
